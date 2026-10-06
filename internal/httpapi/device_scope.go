package httpapi

import (
	"github.com/gin-gonic/gin"
	"iot-platform/internal/devicescope"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/sites"
	"net/http"
	"strings"
)

// scopeFor builds the scope of a resolved user (see resolveUserDeviceScope)
// with the tenant's site index cached alongside the access state.
func (s *Server) scopeFor(u model.PlatformUser, p map[string]bool, tenant string) devicescope.Scope {
	v := devicescope.Scope{Tenant: tenant, IDs: map[string]bool{}}
	if !p["menu:devices"] {
		return v
	}
	v.All = u.DeviceScope == "all"
	if u.DeviceScope == "selected" {
		for _, id := range u.DeviceIDs {
			v.IDs[id] = true
		}
		if len(u.UnitIDs) > 0 {
			v.Units = make(map[string]bool, len(u.UnitIDs))
			for _, id := range u.UnitIDs {
				v.Units[id] = true
			}
			v.Sites = s.cachedSiteIndex(tenant)
		}
	}
	return v
}

func (s *Server) unscopedRepo() ports.Repository { return devicescope.Unscoped(s.engine.Repo) }

func (s *Server) accessDeviceOptions(w http.ResponseWriter, r *http.Request) {
	rows, e := s.unscopedRepo().ListManagedDevices(r.Context(), claims(r).TenantID)
	if e != nil {
		s.failure(w, r, e, "读取设备选项失败")
		return
	}
	out := []map[string]string{}
	for _, v := range rows {
		out = append(out, map[string]string{"id": v.ID, "name": v.Name, "deviceRole": v.DeviceRole})
	}
	// Units can be granted as a whole: every device placed in them.
	units := []map[string]any{}
	if state, err := s.sites.Snapshot(r.Context(), claims(r).TenantID); err == nil {
		count := map[string]int{}
		for _, unit := range sites.DeviceUnits(state) {
			count[unit]++
		}
		for _, u := range state.Units {
			units = append(units, map[string]any{"id": u.ID, "name": u.Name, "devices": count[u.ID]})
		}
	}
	write(w, 200, map[string]any{"items": out, "units": units, "tenantId": claims(r).TenantID})
}

func (s *Server) allowScopedRequest(c *gin.Context, v devicescope.Scope) bool {
	path := c.FullPath()
	// Each draft handler resolves its owner and checks the stored draft kind's
	// exact permissions and raw account scope before exposing any body.
	if strings.HasPrefix(path, "/api/v1/onboarding/drafts") {
		return true
	}
	ctx := c.Request.Context()
	t := v.Tenant
	if strings.HasPrefix(path, "/api/v1/device-registry/:id") || strings.HasPrefix(path, "/api/v1/discovered-devices/:id") {
		if !devicescope.Allowed(ctx, t, c.Param("id")) {
			return false
		}
	}
	if id := c.Param("deviceId"); id != "" && !devicescope.Allowed(ctx, t, id) {
		return false
	}
	alarmID := c.Param("alarmId")
	if strings.HasPrefix(path, "/api/v1/alarms/:id") {
		alarmID = c.Param("id")
	}
	if alarmID != "" {
		if _, e := s.engine.Repo.GetAlarm(ctx, t, alarmID); e != nil {
			return false
		}
	}
	if !v.All {
		if strings.Contains(path, "/products/:id/preparation") || path == "/api/v1/products/:id/verification" {
			return false
		}
		// Tenant-wide jobs and configuration can expose other devices. Their menus
		// are also removed from the effective permission list.
		// Adding devices is limited to users who can see every device.
		if strings.HasPrefix(path, "/api/v1/onboarding") {
			return false
		}
		if strings.HasPrefix(path, "/api/v1/replays") || strings.HasSuffix(path, "/replay") {
			return false
		}
		if c.Request.Method != "GET" && (path == "/api/v1/device-registry" || path == "/api/v1/device-states" || path == "/api/v1/raw-messages") {
			return false
		}
	}
	return true
}
