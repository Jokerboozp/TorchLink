package httpapi

import (
	"context"
	"errors"
	"io"
	"iot-platform/internal/onboarding"
	"net/http"
)

func (s *Server) SetMQTTHealth(health func(context.Context) error) { s.onboarding.MQTTHealth = health }

func (s *Server) onboardingTest(w http.ResponseWriter, r *http.Request) {
	var q onboarding.Request
	if decode(w, r, &q) != nil {
		return
	}
	result, err := s.onboarding.Test(r.Context(), claims(r).TenantID, q)
	if err != nil {
		problem(w, 422, err.Error())
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
		fail(503, "INGEST_FAILED", err.Error())
		return
	}
	write(w, 202, map[string]any{"messageId": idx.MessageID, "created": created, "status": "ACCEPTED"})
}
func (s *Server) disableDeviceCredential(w http.ResponseWriter, r *http.Request) {
	d, err := s.engine.Repo.GetManagedDevice(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "device not found")
		return
	}
	d.SecretHash = ""
	d.SecretHint = ""
	if err = s.engine.Repo.SaveManagedDevice(r.Context(), d); err != nil {
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "device.credential.disable", "device", d.ID, nil)
	write(w, 200, map[string]bool{"disabled": true})
}
