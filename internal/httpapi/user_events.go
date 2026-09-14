package httpapi

import (
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
)

// Authenticated polling re-evaluates roles and device scope on every request.
// Managed accounts never receive a reusable tenant-wide broker credential.
func (s *Server) userEvents(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	alarms := []model.Alarm{}
	states := []model.DeviceState{}
	if c.TokenUse != "user" {
		problem(w, 403, "此接口用于受限用户消息")
		return
	}
	_, p, e := s.managedIdentity(r, c)
	if e != nil {
		problem(w, 401, "会话已失效")
		return
	}
	if p["menu:alarms"] || p["menu:dashboard"] {
		alarms, e = s.engine.Repo.(*deviceScopeRepository).scopedAlarms(r.Context(), ports.AlarmFilter{TenantID: c.TenantID, Status: "ACTIVE"})
		if e != nil {
			problem(w, 503, "读取消息失败")
			return
		}
	}
	if p["menu:devices"] || p["menu:raw"] {
		states, e = s.engine.Repo.ListDeviceStates(r.Context(), c.TenantID)
		if e != nil {
			problem(w, 503, "读取消息失败")
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]any{"alarms": alarms, "devices": states, "permissions": permissionList(p)})
}
