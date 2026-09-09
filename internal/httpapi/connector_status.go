package httpapi

import (
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
	"net/http"
	"sort"
	"strings"
)

type listenerSnapshot interface {
	Status(string, string) (string, string, int64)
	Sessions(string, string) []map[string]any
}

// Match only explicit configuration or an identified session, never product alone.
func deviceUsesProfile(d model.ManagedDevice, p model.DeviceAccessProfile, sessions []map[string]any) bool {
	if d.TenantID != p.TenantID || d.ProductID != p.ProductID {
		return false
	}
	if d.Tags["connectorProfileId"] == p.ID || p.DeviceID == d.ID {
		return true
	}
	for _, session := range sessions {
		if session["deviceId"] == d.ID {
			return true
		}
	}
	return false
}

func (s *Server) profileSnapshot(tenant string, p model.DeviceAccessProfile) (model.DeviceAccessProfile, []map[string]any) {
	sessions := []map[string]any{}
	if p.Mode == "listener" {
		if runtime, ok := s.protocolListeners.(listenerSnapshot); ok {
			p.RuntimeStatus, p.LastError, p.LastSuccessAt = runtime.Status(tenant, p.ID)
			sessions = runtime.Sessions(tenant, p.ID)
		}
	}
	if !p.Enabled {
		p.RuntimeStatus = "DISABLED"
	}
	return p, sessions
}

func (s *Server) connectorStatus(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	items := []map[string]any{}
	devices, err := s.engine.Repo.ListManagedDevices(r.Context(), tenant)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].CreatedAt == devices[j].CreatedAt {
			return devices[i].ID < devices[j].ID
		}
		return devices[i].CreatedAt > devices[j].CreatedAt
	})
	for _, p := range profiles {
		p, sessions := s.profileSnapshot(tenant, p)
		kind := "MODBUS_TCP"
		if p.Mode == "listener" {
			kind = strings.ToUpper(p.Network)
		}
		recent := []map[string]any{}
		for _, d := range devices {
			if deviceUsesProfile(d, p, sessions) && len(recent) < 20 {
				recent = append(recent, map[string]any{"deviceId": d.ID, "name": d.Name, "createdAt": d.CreatedAt})
			}
		}
		items = append(items, map[string]any{"type": kind, "profile": p, "sessions": sessions, "recentDevices": recent})
	}
	for _, d := range devices {
		if d.Tags["connector"] == "MQTT" || d.Tags["connector"] == "HTTP" {
			items = append(items, map[string]any{"type": d.Tags["connector"], "deviceId": d.ID, "profile": nil, "sessions": []any{}})
		}
	}
	write(w, 200, map[string]any{"items": items})
}
func (s *Server) deviceConnection(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	d, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "device not found")
		return
	}
	p, _ := s.engine.Repo.GetProduct(r.Context(), tenant, d.ProductID)
	state, _ := s.engine.Repo.GetDeviceState(r.Context(), tenant, d.ID)
	latest, _ := s.engine.Repo.GetLatestMessage(r.Context(), tenant, d.ID)
	properties, _, err := s.engine.Repo.ListDeviceMessages(r.Context(), tenant, d.ID, model.PropertyReport, 1, 0)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	var profile *model.DeviceAccessProfile
	all, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	candidates := []model.DeviceAccessProfile{}
	byProfile := map[string][]map[string]any{}
	for _, candidate := range all {
		candidate, live := s.profileSnapshot(tenant, candidate)
		if !deviceUsesProfile(d, candidate, live) {
			continue
		}
		candidates = append(candidates, candidate)
		for _, session := range live {
			if session["deviceId"] != d.ID {
				continue
			}
			copy := map[string]any{"profileId": candidate.ID}
			for key, value := range session {
				copy[key] = value
			}
			byProfile[candidate.ID] = append(byProfile[candidate.ID], copy)
		}
	}
	selected := r.URL.Query().Get("profileId")
	if selected == "" {
		for _, candidate := range candidates {
			if candidate.ID == d.Tags["connectorProfileId"] {
				selected = candidate.ID
				break
			}
		}
	}
	for _, candidate := range candidates {
		if candidate.ID == selected || (selected == "" && len(candidates) == 1) {
			v := candidate
			profile = &v
			break
		}
	}
	if r.URL.Query().Get("profileId") != "" && profile == nil {
		problem(w, 422, "接入实例不属于该设备")
		return
	}
	sessions := []map[string]any{}
	for _, candidate := range candidates {
		if profile == nil || profile.ID == candidate.ID {
			sessions = append(sessions, byProfile[candidate.ID]...)
		}
	}
	kind := d.Tags["connector"]
	if kind == "" && profile != nil {
		kind = "MODBUS_TCP"
		if profile.Mode == "listener" {
			kind = strings.ToUpper(profile.Network)
		}
	}
	protocolID, version := "", ""
	if d.Tags["connector"] == "HTTP" || d.Tags["connector"] == "MQTT" {
		protocolID, version = parser.StandardProtocolID, "1.0.0"
	} else if binding, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, d.ProductID); e == nil {
		protocolID, version = binding.ProtocolID, binding.Version
	} else if profile != nil {
		protocolID, version = profile.ProtocolID, profile.ProtocolVersion
	}
	alarms, err := s.engine.Repo.ListAlarms(r.Context(), ports.AlarmFilter{TenantID: tenant, DeviceID: d.ID, Limit: 5})
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	revocations, err := s.engine.Repo.ListCredentialRevocations(r.Context(), tenant, d.ID, false)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	var edge *model.EdgeNode
	if profile != nil && profile.EdgeNodeID != "" {
		if v, e := s.engine.Repo.GetEdgeNode(r.Context(), tenant, profile.EdgeNodeID); e == nil {
			edge = &v
		}
	}
	release, _ := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, protocolID, version)
	write(w, 200, map[string]any{"recentAlarms": alarms, "revocations": revocations, "edgeNode": edge, "mqttCommandAvailable": d.Tags["connector"] == "MQTT" && s.onboarding.PublishCommand != nil, "device": d, "product": p, "connector": kind, "protocolId": protocolID, "protocolVersion": version, "canCommand": protocolworker.HasCapability(release, "encode"), "profile": profile, "profiles": candidates, "connection": state, "sessions": sessions, "latest": latest, "latestProperties": properties, "credentialEnabled": d.SecretHash != ""})
}
