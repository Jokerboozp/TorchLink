package httpapi

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ratelimit"
	"net/http"
	"time"
)

func (s *Server) SetDeviceOperations(publish func(context.Context, string, []byte, byte, bool) error, revoke func(context.Context, string) error) {
	s.onboarding.PublishCommand = publish
	s.onboarding.RevokeUsername = revoke
}

// SetRateLimiter shares device, open API and login budgets across replicas.
func (s *Server) SetRateLimiter(l ratelimit.Limiter) {
	s.onboarding.Limiter = l
	s.logins = newLoginLimiter(l)
}

// RetryCredentialRevocationsOnce retries pending broker revocations once.
func (s *Server) RetryCredentialRevocationsOnce(ctx context.Context) error {
	return s.onboarding.RetryRevocationsOnce(ctx)
}
func (s *Server) deviceOperationsRoutes() {
	s.router.GET("/api/v1/device-registry/:id/history", s.authorize("viewer"), s.endpoint(s.deviceHistory, "id"))
	s.router.GET("/api/v1/device-registry/:id/commands", s.authorize("viewer"), s.endpoint(s.listDeviceCommands, "id"))
	s.router.GET("/api/v1/device-registry/:id/signals", s.authorize("viewer"), s.endpoint(s.deviceSignals, "id"))
	s.router.POST("/api/v1/device-registry/:id/commands", s.authorize("operator"), s.endpoint(s.sendDeviceCommand, "id"))
}
func (s *Server) operationDevice(w http.ResponseWriter, r *http.Request) bool {
	_, e := s.engine.Repo.GetManagedDevice(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if e != nil {
		problem(w, 404, "device not found")
		return false
	}
	return true
}
func (s *Server) deviceHistory(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	t, d := claims(r).TenantID, r.PathValue("id")
	page := parseListPagination(r)
	limit, offset := page.PageSize, page.Offset
	var items any
	var total int
	var e error
	switch r.URL.Query().Get("kind") {
	case "connection", "":
		items, total, e = s.engine.Repo.ListDeviceStateEvents(r.Context(), t, d, limit, offset)
	case "property":
		items, total, e = s.engine.Repo.ListDeviceMessages(r.Context(), t, d, model.PropertyReport, limit, offset)
	case "event":
		items, total, e = s.engine.Repo.ListDeviceMessages(r.Context(), t, d, model.EventReport, limit, offset)
	default:
		problem(w, 422, "kind must be connection, event or property")
		return
	}
	if e != nil {
		s.fail(w, r, e, "")
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) listDeviceCommands(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	page := parseListPagination(r)
	limit, offset := page.PageSize, page.Offset
	items, total, e := s.engine.Repo.ListDeviceCommands(r.Context(), claims(r).TenantID, r.PathValue("id"), limit, offset)
	if e != nil {
		s.fail(w, r, e, "")
		return
	}
	for i := range items {
		items[i] = items[i].ObservedOutcome(time.Now().UnixMilli()).Public()
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) sendDeviceCommand(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	var q model.DeviceCommand
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.onboarding.SendCommand(r.Context(), claims(r).TenantID, r.PathValue("id"), q)
	if e != nil {
		problem(w, 422, e.Error())
		return
	}
	s.audit(r, "device.command", "device", v.DeviceID, map[string]any{"commandId": v.ID, "status": v.Status})
	write(w, 202, v.Public())
}

// deviceSignals lists a device's current health signals.
func (s *Server) deviceSignals(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	if s.engine.DeviceSignals == nil {
		write(w, 200, map[string]any{"items": []any{}, "available": false})
		return
	}
	items, err := s.engine.DeviceSignals.ListDeviceSignals(r.Context(), claims(r).TenantID, []string{r.PathValue("id")}, 50)
	if err != nil {
		s.fail(w, r, err, "读取设备健康信号失败")
		return
	}
	write(w, 200, map[string]any{"items": items, "available": true})
}
