package httpapi

import (
	"context"
	"iot-platform/internal/model"
	"net/http"
	"strconv"
)

func (s *Server) SetDeviceOperations(publish func(context.Context, string, []byte, byte, bool) error, revoke func(context.Context, string) error) {
	s.onboarding.PublishCommand = publish
	s.onboarding.RevokeUsername = revoke
}
func (s *Server) RunCredentialRevocations(ctx context.Context) { s.onboarding.RetryRevocations(ctx) }
func (s *Server) deviceOperationsRoutes() {
	s.router.GET("/api/v1/edge-nodes", s.authorize("viewer"), s.endpoint(s.listEdgeNodes))
	s.router.POST("/api/v1/edge-nodes", s.authorize("admin"), s.endpoint(s.saveEdgeNode))
	s.router.PUT("/api/v1/edge-nodes/:id", s.authorize("admin"), s.endpoint(s.saveEdgeNode, "id"))
	s.router.GET("/api/v1/device-registry/:id/history", s.authorize("viewer"), s.endpoint(s.deviceHistory, "id"))
	s.router.GET("/api/v1/device-registry/:id/commands", s.authorize("viewer"), s.endpoint(s.listDeviceCommands, "id"))
	s.router.POST("/api/v1/device-registry/:id/commands", s.authorize("operator"), s.endpoint(s.sendDeviceCommand, "id"))
}
func (s *Server) listEdgeNodes(w http.ResponseWriter, r *http.Request) {
	v, e := s.engine.Repo.ListEdgeNodes(r.Context(), claims(r).TenantID)
	if e != nil {
		problem(w, 500, e.Error())
		return
	}
	write(w, 200, map[string]any{"items": v})
}
func (s *Server) saveEdgeNode(w http.ResponseWriter, r *http.Request) {
	var v model.EdgeNode
	if decode(w, r, &v) != nil {
		return
	}
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	v, e := s.onboarding.SaveEdge(r.Context(), claims(r).TenantID, v)
	if e != nil {
		problem(w, 422, e.Error())
		return
	}
	s.audit(r, "edge.save", "edge", v.ID, nil)
	write(w, 200, v)
}
func operationPage(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if page < 1 || page > 100000 {
		page = 1
	}
	return limit, (page - 1) * limit
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
	limit, offset := operationPage(r)
	var items any
	var total int
	var e error
	switch r.URL.Query().Get("kind") {
	case "connection", "":
		items, total, e = s.engine.Repo.ListDeviceStateEvents(r.Context(), t, d, limit, offset)
	case "event":
		items, total, e = s.engine.Repo.ListDeviceMessages(r.Context(), t, d, model.EventReport, limit, offset)
	default:
		problem(w, 422, "kind must be connection or event")
		return
	}
	if e != nil {
		problem(w, 500, e.Error())
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) listDeviceCommands(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	limit, offset := operationPage(r)
	items, total, e := s.engine.Repo.ListDeviceCommands(r.Context(), claims(r).TenantID, r.PathValue("id"), limit, offset)
	if e != nil {
		problem(w, 500, e.Error())
		return
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
	write(w, 202, v)
}
