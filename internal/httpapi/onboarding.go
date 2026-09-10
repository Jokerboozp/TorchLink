package httpapi

import (
	"context"
	"errors"
	"io"
	"iot-platform/internal/connector"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) SetMQTTHealth(health func(context.Context) error) { s.onboarding.MQTTHealth = health }

func (s *Server) onboardingTest(w http.ResponseWriter, r *http.Request) {
	var q onboarding.Request
	if decode(w, r, &q) != nil {
		return
	}
	result, err := s.onboarding.Test(r.Context(), claims(r).TenantID, q)
	if err != nil {
		write(w, 422, result)
		return
	}
	write(w, 200, result)
}
func (s *Server) onboardingCreate(w http.ResponseWriter, r *http.Request) {
	var q onboarding.Request
	if decode(w, r, &q) != nil {
		return
	}
	result, err := s.onboarding.Create(r.Context(), claims(r).TenantID, q)
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	result.AccessInfo = s.deviceAccessInfo(result.Device)
	w.Header().Set("Cache-Control", "no-store")
	if result.Reused {
		write(w, 200, result)
		return
	}
	s.audit(r, "device.onboarding", "device", result.Device.ID, map[string]any{"connector": q.Type})
	write(w, 201, result)
}
func (s *Server) standardDeviceIngest(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, code, message string) {
		write(w, status, map[string]string{"error": message, "errorCode": code})
	}
	d, err := s.onboarding.Authenticate(r.Context(), r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret"))
	if err != nil || d.TenantID != r.PathValue("tenantId") || d.ProductID != r.PathValue("productId") || d.ID != r.PathValue("deviceId") {
		fail(401, "AUTH_FAILED", "invalid or disabled device credential")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		fail(413, "BODY_TOO_LARGE", "body exceeds 64 KiB")
		return
	}
	raw, err := s.onboarding.PrepareStandard(r.Context(), d.TenantID, d.ProductID, d.ID, r.PathValue("kind"), "HTTP", data)
	if err != nil {
		status, code := 422, "PROTOCOL_ERROR"
		if errors.Is(err, onboarding.ErrRate) {
			status, code = 429, "RATE_LIMITED"
			w.Header().Set("Retry-After", "1")
		}
		if errors.Is(err, onboarding.ErrAuth) {
			status, code = 401, "AUTH_FAILED"
		}
		fail(status, code, err.Error())
		return
	}
	raw.RemoteAddress = r.RemoteAddr
	idx, created, err := s.engine.IngestRaw(r.Context(), raw)
	if err != nil {
		if errors.Is(err, model.ErrRawConflict) {
			fail(409, "MESSAGE_CONFLICT", err.Error())
			return
		}
		fail(503, "INGEST_FAILED", err.Error())
		return
	}
	write(w, 202, map[string]any{"messageId": idx.MessageID, "created": created, "status": "ACCEPTED"})
}
func (s *Server) disableDeviceCredential(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	_, v, e := s.onboarding.ChangeCredential(r.Context(), claims(r).TenantID, r.PathValue("id"), false)
	if e != nil {
		status := http.StatusInternalServerError
		if errors.Is(e, onboarding.ErrCredentialUnsupported) {
			status = http.StatusUnprocessableEntity
		}
		problem(w, status, e.Error())
		return
	}
	s.audit(r, "device.credential.disable", "device", r.PathValue("id"), nil)
	write(w, 200, map[string]any{"disabled": true, "revocation": v})
}

func (s *Server) connectorTypes(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"items": connector.Types()})
}

func publicEndpoint(value string) string {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	switch u.Scheme {
	case "http", "https", "mqtt", "mqtts", "tcp", "ssl", "ws", "wss":
		return strings.TrimRight(u.String(), "/")
	}
	return ""
}
func (s *Server) deviceAccessInfo(d model.ManagedDevice) map[string]any {
	if !d.UsesPlatformCredentials(model.Product{}) || (d.Tags["connector"] != "HTTP" && d.Tags["connector"] != "MQTT") {
		return nil
	}
	identity := d.TenantID + "/" + d.ProductID + "/" + d.ID
	return map[string]any{
		"httpUrl":    publicEndpoint(s.cfg.DeviceHTTPPublicURL) + "/api/v1/device-ingest/standard/" + identity + "/property",
		"mqttBroker": publicEndpoint(s.cfg.MQTTPublicURL), "mqttWebSocket": publicEndpoint(s.cfg.MQTTWebSocketURL),
		"clientId": "device-" + d.AccessKey, "username": d.AccessKey, "tokenEndpoint": "/api/v1/device-mqtt/token",
		"upTopic": "/iot/up/" + identity + "/property", "downTopic": "/iot/down/" + identity + "/command",
		"sample": map[string]any{"version": "1.0", "id": "replace-with-unique-message-id", "timestamp": time.Now().UnixMilli(), "data": map[string]any{"temperature": 26.5}},
	}
}
