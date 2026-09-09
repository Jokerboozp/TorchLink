package httpapi

import (
	"errors"
	"iot-platform/internal/model"
	"net/http"
)

func (s *Server) shadowRoutes() {
	s.router.GET("/api/v1/device-registry/:id/shadow", s.authorize("viewer"), s.endpoint(s.deviceShadow, "id"))
	s.router.PATCH("/api/v1/device-registry/:id/shadow", s.authorize("operator"), s.endpoint(s.deviceShadow, "id"))
	s.router.GET("/api/v1/device-registry/:id/shadow/history", s.authorize("viewer"), s.endpoint(s.shadowHistory, "id"))
	s.router.GET("/api/v1/device-shadow", s.endpoint(s.authenticatedDeviceShadow))
}
func (s *Server) deviceShadow(w http.ResponseWriter, r *http.Request) {
	tenant, id := claims(r).TenantID, r.PathValue("id")
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, id); err != nil {
		problem(w, 404, "device not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		shadow, err := s.engine.Repo.GetDeviceShadow(r.Context(), tenant, id)
		if err != nil {
			problem(w, 503, "load device shadow")
			return
		}
		write(w, 200, shadow)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var request struct {
		Version   int64          `json:"expectedDesiredVersion"`
		Desired   map[string]any `json:"desired"`
		Confirmed bool           `json:"confirmed"`
	}
	if decode(w, r, &request) != nil {
		return
	}
	if !request.Confirmed {
		problem(w, 422, "修改期望状态需要人工确认")
		return
	}
	shadow, err := s.engine.SetShadowDesired(r.Context(), tenant, id, claims(r).Username, request.Version, request.Desired)
	if err != nil {
		status := 422
		if errors.Is(err, model.ErrShadowConflict) {
			status = 409
		}
		problem(w, status, err.Error())
		return
	}
	s.audit(r, "device.shadow.desired", "device", id, map[string]any{"desiredVersion": shadow.DesiredVersion})
	write(w, 200, shadow)
}
func (s *Server) shadowHistory(w http.ResponseWriter, r *http.Request) {
	tenant, id := claims(r).TenantID, r.PathValue("id")
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, id); err != nil {
		problem(w, 404, "device not found")
		return
	}
	pagination := parseListPagination(r)
	items, err := s.engine.Repo.ListShadowChanges(r.Context(), tenant, id, pagination.PageSize, pagination.Offset)
	if err != nil {
		problem(w, 503, "load shadow history")
		return
	}
	write(w, 200, map[string]any{"items": items, "page": pagination.Page, "pageSize": pagination.PageSize})
}
func (s *Server) authenticatedDeviceShadow(w http.ResponseWriter, r *http.Request) {
	d, err := s.onboarding.Authenticate(r.Context(), r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret"))
	if err != nil {
		problem(w, 401, "invalid or disabled device credential")
		return
	}
	shadow, err := s.engine.Repo.GetDeviceShadow(r.Context(), d.TenantID, d.ID)
	if err != nil {
		problem(w, 503, "load device shadow")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, shadow)
}
