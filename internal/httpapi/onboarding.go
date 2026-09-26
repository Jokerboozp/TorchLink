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

func (s *Server) publicAddresses() onboarding.PublicAddresses {
	return onboarding.PublicAddresses{HTTP: publicEndpoint(s.cfg.DeviceHTTPPublicURL) != "", MQTT: publicEndpoint(s.cfg.MQTTPublicURL) != ""}
}

func enrollProblem(w http.ResponseWriter, err error) {
	var e *onboarding.EnrollError
	if errors.As(err, &e) {
		problem(w, e.Status, e.Message)
		return
	}
	problem(w, 500, err.Error())
}

// onboardingPreflight evaluates a saved template, or a template draft described
// by protocolPackageId and transport, without writing anything.
func (s *Server) onboardingPreflight(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	productID := strings.TrimSpace(q.Get("productId"))
	var draft *onboarding.NewProduct
	if productID == "" {
		draft = &onboarding.NewProduct{Category: q.Get("category"), ProtocolPackageID: q.Get("protocolPackageId"), Transport: q.Get("transport")}
		if strings.TrimSpace(draft.ProtocolPackageID) == "" {
			problem(w, 422, "请选择设备模板，或为新模板选择通信协议")
			return
		}
	}
	result, err := s.onboarding.Preflight(r.Context(), claims(r).TenantID, productID, draft, s.publicAddresses())
	if err != nil {
		enrollProblem(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, result)
}

// onboardingEnroll adds one device, and optionally its template and platform
// connection, in one transaction. Creating a template or a shared listener
// also requires the caller's permission for those resources.
func (s *Server) onboardingEnroll(w http.ResponseWriter, r *http.Request) {
	var q onboarding.EnrollRequest
	if decode(w, r, &q) != nil {
		return
	}
	if q.NewProduct != nil && !requestAllows(r, "POST", "/api/v1/products") {
		problem(w, 403, "当前账号不能新建设备模板，请选择已有模板")
		return
	}
	if q.Connection.Listener != nil && !requestAllows(r, "POST", "/api/v2/device-access-profiles") {
		problem(w, 403, "当前账号不能新建平台接入点，请选择已有接入点")
		return
	}
	result, err := s.onboarding.Enroll(r.Context(), claims(r).TenantID, q)
	if err != nil {
		enrollProblem(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response := struct {
		onboarding.EnrollResult
		AccessInfo map[string]any `json:"accessInfo,omitempty"`
	}{result, s.deviceAccessInfo(result.Device, result.Product)}
	if result.Reused {
		write(w, 200, response)
		return
	}
	details := map[string]any{"productId": result.Product.ID, "mode": result.Mode, "newProduct": q.NewProduct != nil}
	if result.Profile != nil {
		details["profileId"] = result.Profile.ID
	}
	s.audit(r, "device.onboarding", "device", result.Device.ID, details)
	write(w, 201, response)
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

// deviceAccessInfo returns what a credential device needs to report data. It
// never includes the device secret.
func (s *Server) deviceAccessInfo(d model.ManagedDevice, product model.Product) map[string]any {
	if !d.UsesPlatformCredentials(product) {
		return nil
	}
	endpoint := publicEndpoint(s.cfg.DeviceHTTPPublicURL)
	if d.Connector != "HTTP" && d.Connector != "MQTT" {
		// Managed devices post raw payloads that the template protocol parses.
		httpURL := ""
		if endpoint != "" {
			httpURL = endpoint + "/api/v1/device-ingest/" + url.PathEscape(d.ID)
		}
		return map[string]any{"kind": "managed", "httpUrl": httpURL, "username": d.AccessKey, "sample": map[string]any{"payload": map[string]any{"temperature": 26.5}}}
	}
	identity := d.TenantID + "/" + d.ProductID + "/" + d.ID
	httpURL := ""
	if endpoint != "" {
		httpURL = endpoint + "/api/v1/device-ingest/standard/" + identity + "/property"
	}
	return map[string]any{
		"kind": "standard", "httpUrl": httpURL,
		"mqttBroker": publicEndpoint(s.cfg.MQTTPublicURL), "mqttWebSocket": publicEndpoint(s.cfg.MQTTWebSocketURL),
		"clientId": "device-" + d.AccessKey, "username": d.AccessKey, "tokenEndpoint": "/api/v1/device-mqtt/token",
		"upTopic": "/iot/up/" + identity + "/property", "downTopic": "/iot/down/" + identity + "/command",
		"sample": map[string]any{"version": "1.0", "id": "replace-with-unique-message-id", "timestamp": time.Now().UnixMilli(), "data": map[string]any{"temperature": 26.5}},
	}
}
