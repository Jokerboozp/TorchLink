package httpapi

import (
	"encoding/json"
	"iot-platform/internal/model"
	"net/http"
)

// Authenticated polling re-evaluates roles and device scope on every request.
// Managed accounts never receive a reusable tenant-wide broker credential.
func (s *Server) userEvents(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	alarms := []model.Alarm{}
	states := []model.DeviceState{}
	p := map[string]bool{"*": true}
	var e error
	if c.TokenUse == "user" {
		_, p, e = s.managedIdentity(r, c)
		if e != nil {
			problem(w, 401, "会话已失效")
			return
		}
	} else if c.TokenUse != "" || c.Role != "admin" {
		problem(w, 403, "此接口用于已登录用户消息")
		return
	}
	wantAlarms := p["*"] || p["menu:alarms"] || p["menu:dashboard"]
	wantStates := p["*"] || p["menu:devices"] || p["menu:raw"]
	if wantAlarms || wantStates {
		allAlarms, allStates, err := s.events.get(r.Context(), s.unscopedRepo(), c.TenantID)
		if err != nil {
			problem(w, 503, "读取消息失败")
			return
		}
		if wantAlarms {
			alarms = scopedEventAlarms(r.Context(), c.TenantID, allAlarms)
		}
		if wantStates {
			states = scopedEventStates(r.Context(), c.TenantID, allStates)
		}
	}
	body, err := json.Marshal(map[string]any{"alarms": alarms, "devices": states, "permissions": permissionList(p), "accessVersion": requestAccessVersion(r.Context(), c)})
	if err != nil {
		problem(w, 500, "读取消息失败")
		return
	}
	// Pages poll every few seconds; an unchanged view answers 304 without a body.
	tag := eventETag(body)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
}
