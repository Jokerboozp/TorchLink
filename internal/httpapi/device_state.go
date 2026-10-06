package httpapi

import (
	"net/http"
	"strings"

	"iot-platform/internal/model"
)

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	tenantID := claims(r).TenantID
	unregisteredOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("unregistered")), "true")
	var items []model.DeviceState
	var total int
	var err error
	if unregisteredOnly {
		items, total, err = s.engine.Repo.ListUnregisteredDeviceStatesPage(r.Context(), tenantID, pagination.PageSize, pagination.Offset)
	} else {
		items, total, err = s.engine.Repo.ListDeviceStatesPage(r.Context(), tenantID, pagination.PageSize, pagination.Offset)
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	_, online, err := s.engine.Repo.CountDeviceStates(r.Context(), tenantID, unregisteredOnly)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeList(w, 200, items, total, pagination, map[string]any{"online": online, "offline": total - online, "unregistered": unregisteredOnly})
}

func (s *Server) deviceLatest(w http.ResponseWriter, r *http.Request) {
	tenant, device := claims(r).TenantID, r.PathValue("deviceId")
	v, err := s.engine.Repo.GetDeviceState(r.Context(), tenant, device)
	if err != nil {
		problem(w, 404, "device state not found")
		return
	}
	out := map[string]any{"state": v}
	if latest, latestErr := s.engine.Repo.GetLatestMessage(r.Context(), tenant, device); latestErr == nil {
		out["latestMessage"] = latest
		out["properties"] = latest.Properties
		out["timestamp"] = latest.Timestamp
	}
	write(w, 200, out)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	property := q.Get("property")
	if property == "" {
		problem(w, 400, "property is required")
		return
	}
	if !validPropertyName(property) {
		problem(w, 422, "property name is invalid")
		return
	}
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.PropertyHistoryPage(r.Context(), claims(r).TenantID, r.PathValue("deviceId"), property, i64(q.Get("start")), i64(q.Get("end")), pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeList(w, 200, items, total, pagination, nil)
}

// validPropertyName bounds the free-form property name used by history
// queries: non-identifier names reach the storage layer as string literals.
func validPropertyName(name string) bool {
	if len(name) > 256 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (s *Server) stateEvent(w http.ResponseWriter, r *http.Request) {
	var v model.DeviceState
	if decode(w, r, &v) != nil {
		return
	}
	v.TenantID = tenant(claims(r), v.TenantID)
	if err := s.engine.UpdateDeviceState(r.Context(), v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 202, v)
}
