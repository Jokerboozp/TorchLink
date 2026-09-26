package httpapi

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type listenerSnapshot interface {
	Status(string, string) (string, string, int64)
	Sessions(string, string) []map[string]any
}

// Match only explicit configuration or an identified session, never product alone.
func deviceUsesProfile(d model.ManagedDevice, p model.DeviceAccessProfile, sessions []map[string]any) bool {
	if d.TenantID == p.TenantID && d.GatewayID != "" && d.ConnectorProfileID == p.ID {
		return true
	}
	if d.TenantID != p.TenantID || d.ProductID != p.ProductID {
		return false
	}
	if d.ConnectorProfileID == p.ID || p.DeviceID == d.ID {
		return true
	}
	for _, session := range sessions {
		if session["deviceId"] == d.ID {
			return true
		}
	}
	return false
}

func (s *Server) profileSnapshot(ctx context.Context, tenant string, p model.DeviceAccessProfile) (model.DeviceAccessProfile, []map[string]any) {
	sessions := []map[string]any{}
	if p.Mode == "listener" && p.EdgeNodeID == "" {
		if runtime, ok := s.protocolListeners.(listenerSnapshot); ok {
			status, message, last := runtime.Status(tenant, p.ID)
			p.RuntimeStatus, p.LastError = status, message
			if status == "LISTENING" || status == "CONNECTED" {
				p.LastSuccessAt = max(p.LastSuccessAt, last)
			}
			sessions = runtime.Sessions(tenant, p.ID)
		}
	}
	if p.EdgeNodeID != "" {
		p.RuntimeStatus, p.LastError = "UNSUPPORTED", "边缘节点功能已移除，旧现场任务不会在中心执行"
		sessions = []map[string]any{}
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
	if limited(r.Context()) {
		profiles = nil
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
		p, sessions := s.profileSnapshot(r.Context(), tenant, p)
		kind := profileTransport(p)
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
		if d.Connector == "MQTT" || d.Connector == "HTTP" {
			items = append(items, map[string]any{"type": d.Connector, "deviceId": d.ID, "profile": nil, "sessions": []any{}})
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
	p, productErr := s.engine.Repo.GetProduct(r.Context(), tenant, d.ProductID)
	isChild := d.GatewayID != "" || d.DeviceRole == "CHILD"
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
	if limited(r.Context()) {
		all = nil
	}
	candidates := []model.DeviceAccessProfile{}
	byProfile := map[string][]map[string]any{}
	for _, candidate := range all {
		candidate, live := s.profileSnapshot(r.Context(), tenant, candidate)
		if !deviceUsesProfile(d, candidate, live) {
			continue
		}
		candidates = append(candidates, candidate)
		for _, session := range live {
			if session["deviceId"] != d.ID && (d.GatewayID == "" || session["deviceId"] != d.GatewayID) {
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
			if candidate.ID == d.ConnectorProfileID {
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
	kind := d.Connector
	if kind == "" && profile != nil {
		kind = profileTransport(*profile)
		if profile.Mode == "listener" {
			kind = strings.ToUpper(profile.Network)
		}
	}
	protocolID, version := "", ""
	bindingRevision := int64(0)
	if d.Connector == "HTTP" || d.Connector == "MQTT" {
		protocolID, version = parser.StandardProtocolID, "1.0.0"
	} else if binding, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, d.ProductID); e == nil {
		protocolID, version = binding.ProtocolID, binding.Version
		bindingRevision = binding.UpdatedAt
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
	// A check starts at a caller-selected observation time, but never predates the
	// saved device and selected connection revisions. The ordinary detail view
	// continues to show the latest historical result when since is omitted.
	since := int64(0)
	if value := r.URL.Query().Get("since"); value != "" {
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || parsed < 1 || parsed > time.Now().Add(time.Minute).UnixMilli() {
			problem(w, 422, "检测开始时间无效")
			return
		}
		since = max(parsed, d.UpdatedAt, p.UpdatedAt, bindingRevision)
		if profile != nil {
			since = max(since, profile.UpdatedAt)
		}
	}
	indexes, err := s.engine.Repo.ListRawIndexes(r.Context(), ports.RawFilter{TenantID: tenant, DeviceID: d.ID, Start: since, Limit: 30})
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	ingest := map[string]any{"configurationSaved": true, "rawReceived": false, "parsed": false, "stage": "WAITING_FOR_DATA", "since": since, "recentCount": 0, "simulationCount": 0, "continuouslyUpdating": false}
	var selectedRaw *model.RawArchiveIndex
	validCount := 0
	parsedCount := 0
	simulationCount := 0
	for i := range indexes {
		idx := &indexes[i]
		raw, rawErr := s.engine.GetRaw(r.Context(), *idx)
		if rawErr != nil || raw.TenantID != tenant || raw.ProductID != d.ProductID || raw.DeviceID != d.ID {
			continue
		}
		// Debug, sample, replay and arbitrary management ingress are not field evidence.
		if raw.Source == "managed-device" || raw.Source == "gateway" {
			simulationCount++
			continue
		}
		if !fieldSource(raw.Source) {
			continue
		}
		if profile != nil && raw.Metadata["profileId"] != profile.ID {
			continue
		}
		if protocolID != "" && raw.ProtocolID != "" && raw.ProtocolID != protocolID {
			continue
		}
		if version != "" && raw.ProtocolVersion != "" && raw.ProtocolVersion != version {
			continue
		}
		validCount++
		if _, parseErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), tenant, idx.MessageID); parseErr == nil {
			parsedCount++
		}
		if selectedRaw == nil {
			selectedRaw = idx
		}
	}
	ingest["recentCount"] = validCount
	ingest["simulationCount"] = simulationCount
	if selectedRaw != nil {
		idx := *selectedRaw
		ingest["rawReceived"] = true
		ingest["rawMessageId"] = idx.MessageID
		ingest["receivedAt"] = idx.ReceivedAt
		ingest["stage"] = "RAW_RECEIVED"
		ingest["parseError"] = idx.ParseError
		ingest["parseAttemptedAt"] = idx.ParseAttemptedAt
		if idx.ParseError != "" {
			ingest["stage"] = "PARSE_FAILED"
		}
		if standard, e := s.engine.Repo.GetStandardMessageByRaw(r.Context(), tenant, idx.MessageID); e == nil {
			ingest["parsed"] = true
			ingest["stage"] = "PARSED"
			ingest["standardMessage"] = standard
		}
		if idx.ReceivedAt < time.Now().Add(-15*time.Minute).UnixMilli() {
			ingest["stale"] = true
		} else if ingest["parsed"] == true && parsedCount >= 2 {
			ingest["continuouslyUpdating"] = true
		}
	}
	if since > 0 && ingest["parsed"] != true {
		older, olderErr := s.engine.Repo.ListRawIndexes(r.Context(), ports.RawFilter{TenantID: tenant, DeviceID: d.ID, End: since - 1, Limit: 30})
		if olderErr == nil {
			for _, idx := range older {
				raw, rawErr := s.engine.GetRaw(r.Context(), idx)
				if rawErr != nil || raw.TenantID != tenant || raw.ProductID != d.ProductID || raw.DeviceID != d.ID || !fieldSource(raw.Source) {
					continue
				}
				if _, parsedErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), tenant, idx.MessageID); parsedErr == nil {
					ingest["previousParsedAt"] = idx.ReceivedAt
					break
				}
			}
		}
	}
	var parent any
	if !deviceAllowed(r.Context(), tenant, d.GatewayID) {
		d.GatewayID = ""
	}
	if d.GatewayID != "" {
		if gateway, e := s.engine.Repo.GetManagedDevice(r.Context(), tenant, d.GatewayID); e == nil {
			parent = map[string]any{"id": gateway.ID, "name": gateway.Name}
		}
	}
	release, _ := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, protocolID, version)
	canCommand := protocolworker.HasCapability(release, "encode") && (profile == nil || profile.EdgeNodeID == "")
	if d.GatewayID != "" {
		canCommand = canCommand && profile != nil
		if profile != nil {
			binding, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, profile.ProductID)
			if e != nil {
				canCommand = false
			} else {
				outer, e := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, binding.ProtocolID, binding.Version)
				canCommand = canCommand && e == nil && protocolworker.HasCapability(outer, "encode")
			}
		}
	}

	diagnosis := s.connectionDiagnosis(r.Context(), tenant, d, p, productErr == nil, isChild, parent != nil, profile, ingest, selectedRaw)
	write(w, 200, map[string]any{"diagnosis": diagnosis, "parent": parent, "accessInfo": s.deviceAccessInfo(d, p), "ingest": ingest, "recentAlarms": alarms, "revocations": revocations, "mqttCommandAvailable": d.Connector == "MQTT" && s.onboarding.PublishCommand != nil, "device": d.Public(p), "product": p, "connector": kind, "protocolId": protocolID, "protocolVersion": version, "canCommand": canCommand, "profile": profile, "profiles": candidates, "connection": state, "sessions": sessions, "latest": latest, "latestProperties": properties, "credentialSupported": d.UsesPlatformCredentials(p), "credentialEnabled": d.UsesPlatformCredentials(p) && d.SecretHash != ""})
}

func profileTransport(p model.DeviceAccessProfile) string {
	if p.WireFormat == "rtu_over_tcp" {
		return "MODBUS_RTU_TCP"
	}
	if p.Mode == "listener" {
		return strings.ToUpper(p.Network)
	}
	switch p.Network {
	case "serial":
		return "MODBUS_RTU"
	case "opc_ua":
		return "OPC_UA"
	case "snmp":
		return "SNMP"
	case "bacnet":
		return "BACNET"
	}
	return "MODBUS_TCP"
}

// fieldSource tells whether a raw message came from a real device connection.
// Debug, sample, replay and management ingress are not evidence of onboarding.
func fieldSource(source string) bool {
	switch source {
	case "standard-http", "standard-mqtt", "device-http", "modbus-tcp-collector":
		return true
	}
	return strings.HasPrefix(source, "go-protocol-")
}

func (s *Server) connectionDiagnosis(ctx context.Context, tenant string, d model.ManagedDevice, p model.Product, productFound, isChild, parentVisible bool, profile *model.DeviceAccessProfile, ingest map[string]any, selected *model.RawArchiveIndex) onboarding.Diagnosis {
	plan, planErr := s.onboarding.Plan(ctx, tenant, p)
	in := onboarding.DiagnosisInput{
		ProductEnabled:  productFound && p.Status == "ENABLED",
		ProtocolValid:   planErr == nil && plan.Protocol.Published,
		DeviceEnabled:   d.Status == "ENABLED",
		UsesCredentials: d.UsesPlatformCredentials(p),
		IsChild:         isChild,
		ParentVisible:   parentVisible,
		Profile:         profile,
	}
	address := s.cfg.DeviceHTTPPublicURL
	if d.Connector == "MQTT" {
		address = s.cfg.MQTTPublicURL
	}
	if isChild && profile == nil && d.GatewayID != "" {
		// Children of a credential gateway report through the gateway's HTTP ingress.
		if gateway, err := s.engine.Repo.GetManagedDevice(ctx, tenant, d.GatewayID); err == nil {
			if gatewayProduct, err := s.engine.Repo.GetProduct(ctx, tenant, gateway.ProductID); err == nil && gateway.UsesPlatformCredentials(gatewayProduct) {
				in.UsesCredentials, address = true, s.cfg.DeviceHTTPPublicURL
			}
		}
	}
	in.AddressReady = publicEndpoint(address) != ""
	in.Ingest = onboarding.IngestSummary{ConfigurationSaved: true, Parsed: ingest["parsed"] == true, Stale: ingest["stale"] == true, ContinuouslyUpdating: ingest["continuouslyUpdating"] == true}
	if selected != nil {
		in.Ingest.RawReceived, in.Ingest.ReceivedAt = true, selected.ReceivedAt
		if !in.Ingest.Parsed {
			in.Ingest.ParseError = selected.ParseError
		}
	}
	if at, ok := ingest["previousParsedAt"].(int64); ok {
		in.Ingest.PreviousParsedAt = at
	}
	return onboarding.Diagnose(in)
}
