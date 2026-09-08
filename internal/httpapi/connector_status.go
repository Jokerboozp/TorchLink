package httpapi

import (
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
	"net/http"
	"strings"
)

type listenerSnapshot interface {
	Status(string, string) (string, string, int64)
	Sessions(string, string) []map[string]any
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
	for _, p := range profiles {
		kind := "MODBUS_TCP"
		sessions := []map[string]any{}
		if p.Mode == "listener" {
			kind = strings.ToUpper(p.Network)
			if runtime, ok := s.protocolListeners.(listenerSnapshot); ok {
				p.RuntimeStatus, p.LastError, p.LastSuccessAt = runtime.Status(tenant, p.ID)
				sessions = runtime.Sessions(tenant, p.ID)
			}
		}
		if !p.Enabled {
			p.RuntimeStatus = "DISABLED"
		}
		recent := []map[string]any{}
		for _, d := range devices {
			if d.Tags["connectorProfileId"] == p.ID && len(recent) < 20 {
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
	if id := d.Tags["connectorProfileId"]; id != "" {
		if v, e := s.engine.Repo.GetDeviceAccessProfile(r.Context(), tenant, id); e == nil {
			profile = &v
		}
	}
	sessions := []map[string]any{}
	if profile != nil && profile.Mode == "listener" {
		if runtime, ok := s.protocolListeners.(listenerSnapshot); ok {
			profile.RuntimeStatus, profile.LastError, profile.LastSuccessAt = runtime.Status(tenant, profile.ID)
			for _, session := range runtime.Sessions(tenant, profile.ID) {
				if session["deviceId"] == d.ID {
					sessions = append(sessions, session)
				}
			}
		}
	}
	protocolID, version := "", ""
	if profile != nil {
		protocolID, version = profile.ProtocolID, profile.ProtocolVersion
	} else if d.Tags["connector"] == "HTTP" || d.Tags["connector"] == "MQTT" {
		protocolID, version = parser.StandardProtocolID, "1.0.0"
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
	write(w, 200, map[string]any{"revocations": revocations, "edgeNode": edge, "mqttCommandAvailable": d.Tags["connector"] == "MQTT" && s.onboarding.PublishCommand != nil, "device": d, "product": p, "connector": d.Tags["connector"], "protocolId": protocolID, "protocolVersion": version, "canCommand": protocolworker.HasCapability(release, "encode"), "profile": profile, "connection": state, "sessions": sessions, "latest": latest, "latestProperties": properties, "credentialEnabled": d.SecretHash != ""})
}
