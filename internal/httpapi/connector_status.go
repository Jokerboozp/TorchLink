package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
	"net/http"                             /* 执行当前语句并推进处理流程。 */
	"sort"                                 /* 执行当前语句并推进处理流程。 */
	"strconv"
	"strings" /* 执行当前语句并推进处理流程。 */
	"time"
) /* 结束当前表达式或代码块。 */

type listenerSnapshot interface { /* 定义 listenerSnapshot 类型。 */
	Status(string, string) (string, string, int64) /* 执行当前语句并推进处理流程。 */
	Sessions(string, string) []map[string]any      /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// Match only explicit configuration or an identified session, never product alone.
func deviceUsesProfile(d model.ManagedDevice, p model.DeviceAccessProfile, sessions []map[string]any) bool { /* 定义 deviceUsesProfile 函数。 */
	if d.TenantID == p.TenantID && d.GatewayID != "" && d.ConnectorProfileID == p.ID { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if d.TenantID != p.TenantID || d.ProductID != p.ProductID { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if d.ConnectorProfileID == p.ID || p.DeviceID == d.ID { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, session := range sessions { /* 循环处理当前数据。 */
		if session["deviceId"] == d.ID { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) profileSnapshot(ctx context.Context, tenant string, p model.DeviceAccessProfile) (model.DeviceAccessProfile, []map[string]any) { /* 定义 profileSnapshot 函数。 */
	sessions := []map[string]any{}                  /* 更新 sessions 的值。 */
	if p.Mode == "listener" && p.EdgeNodeID == "" { /* 判断条件并选择处理分支。 */
		if runtime, ok := s.protocolListeners.(listenerSnapshot); ok { /* 判断条件并选择处理分支。 */
			status, message, last := runtime.Status(tenant, p.ID) /* 更新 last 的值。 */
			p.RuntimeStatus, p.LastError = status, message        /* 更新 p.LastError 的值。 */
			if status == "LISTENING" || status == "CONNECTED" {   /* 判断条件并选择处理分支。 */
				p.LastSuccessAt = max(p.LastSuccessAt, last) /* 更新 p.LastSuccessAt 的值。 */
			} /* 结束当前表达式或代码块。 */
			sessions = runtime.Sessions(tenant, p.ID) /* 更新 sessions 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if p.EdgeNodeID != "" { /* 判断条件并选择处理分支。 */
		p.RuntimeStatus, p.LastError = "UNSUPPORTED", "边缘节点功能已移除，旧现场任务不会在中心执行" /* 更新 p.LastError 的值。 */
		sessions = []map[string]any{}                                          /* 更新 sessions 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !p.Enabled { /* 判断条件并选择处理分支。 */
		p.RuntimeStatus = "DISABLED" /* 更新 p.RuntimeStatus 的值。 */
	} /* 结束当前表达式或代码块。 */
	return p, sessions /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) connectorStatus(w http.ResponseWriter, r *http.Request) { /* 定义 connectorStatus 函数。 */
	tenant := claims(r).TenantID                                                 /* 更新 tenant 的值。 */
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant) /* 更新 err 的值。 */
	if err != nil {                                                              /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if limited(r.Context()) { /* 判断条件并选择处理分支。 */
		profiles = nil /* 更新 profiles 的值。 */
	} /* 结束当前表达式或代码块。 */
	items := []map[string]any{}                                           /* 更新 items 的值。 */
	devices, err := s.engine.Repo.ListManagedDevices(r.Context(), tenant) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	sort.SliceStable(devices, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if devices[i].CreatedAt == devices[j].CreatedAt { /* 判断条件并选择处理分支。 */
			return devices[i].ID < devices[j].ID /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return devices[i].CreatedAt > devices[j].CreatedAt /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	for _, p := range profiles { /* 循环处理当前数据。 */
		p, sessions := s.profileSnapshot(r.Context(), tenant, p) /* 更新 sessions 的值。 */
		kind := profileTransport(p)                              /* 更新 kind 的值。 */
		if p.Mode == "listener" {                                /* 判断条件并选择处理分支。 */
			kind = strings.ToUpper(p.Network) /* 更新 kind 的值。 */
		} /* 结束当前表达式或代码块。 */
		recent := []map[string]any{} /* 更新 recent 的值。 */
		for _, d := range devices {  /* 循环处理当前数据。 */
			if deviceUsesProfile(d, p, sessions) && len(recent) < 20 { /* 判断条件并选择处理分支。 */
				recent = append(recent, map[string]any{"deviceId": d.ID, "name": d.Name, "createdAt": d.CreatedAt}) /* 更新 recent 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, map[string]any{"type": kind, "profile": p, "sessions": sessions, "recentDevices": recent}) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, d := range devices { /* 循环处理当前数据。 */
		if d.Connector == "MQTT" || d.Connector == "HTTP" { /* 判断条件并选择处理分支。 */
			items = append(items, map[string]any{"type": d.Connector, "deviceId": d.ID, "profile": nil, "sessions": []any{}}) /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": items}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) deviceConnection(w http.ResponseWriter, r *http.Request) { /* 定义 deviceConnection 函数。 */
	tenant := claims(r).TenantID                                                     /* 更新 tenant 的值。 */
	d, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		problem(w, 404, "device not found") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p, productErr := s.engine.Repo.GetProduct(r.Context(), tenant, d.ProductID)
	isChild := d.GatewayID != "" || d.DeviceRole == "CHILD"
	state, _ := s.engine.Repo.GetDeviceState(r.Context(), tenant, d.ID)                                           /* 更新 _ 的值。 */
	latest, _ := s.engine.Repo.GetLatestMessage(r.Context(), tenant, d.ID)                                        /* 更新 _ 的值。 */
	properties, _, err := s.engine.Repo.ListDeviceMessages(r.Context(), tenant, d.ID, model.PropertyReport, 1, 0) /* 更新 err 的值。 */
	if err != nil {                                                                                               /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var profile *model.DeviceAccessProfile                                  /* 声明 profile。 */
	all, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant) /* 更新 err 的值。 */
	if err != nil {                                                         /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if limited(r.Context()) { /* 判断条件并选择处理分支。 */
		all = nil /* 更新 all 的值。 */
	} /* 结束当前表达式或代码块。 */
	candidates := []model.DeviceAccessProfile{} /* 更新 candidates 的值。 */
	byProfile := map[string][]map[string]any{}  /* 更新 byProfile 的值。 */
	for _, candidate := range all {             /* 循环处理当前数据。 */
		candidate, live := s.profileSnapshot(r.Context(), tenant, candidate) /* 更新 live 的值。 */
		if !deviceUsesProfile(d, candidate, live) {                          /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		candidates = append(candidates, candidate) /* 更新 candidates 的值。 */
		for _, session := range live {             /* 循环处理当前数据。 */
			if session["deviceId"] != d.ID && (d.GatewayID == "" || session["deviceId"] != d.GatewayID) { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			copy := map[string]any{"profileId": candidate.ID} /* 更新 copy 的值。 */
			for key, value := range session {                 /* 循环处理当前数据。 */
				copy[key] = value /* 更新 copy[key] 的值。 */
			} /* 结束当前表达式或代码块。 */
			byProfile[candidate.ID] = append(byProfile[candidate.ID], copy) /* 更新 byProfile[candidate.ID] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	selected := r.URL.Query().Get("profileId") /* 更新 selected 的值。 */
	if selected == "" {                        /* 判断条件并选择处理分支。 */
		for _, candidate := range candidates { /* 循环处理当前数据。 */
			if candidate.ID == d.ConnectorProfileID { /* 判断条件并选择处理分支。 */
				selected = candidate.ID /* 更新 selected 的值。 */
				break                   /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, candidate := range candidates { /* 循环处理当前数据。 */
		if candidate.ID == selected || (selected == "" && len(candidates) == 1) { /* 判断条件并选择处理分支。 */
			v := candidate /* 更新 v 的值。 */
			profile = &v   /* 更新 profile 的值。 */
			break          /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if r.URL.Query().Get("profileId") != "" && profile == nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, "接入实例不属于该设备") /* 执行当前语句并推进处理流程。 */
		return                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	sessions := []map[string]any{}         /* 更新 sessions 的值。 */
	for _, candidate := range candidates { /* 循环处理当前数据。 */
		if profile == nil || profile.ID == candidate.ID { /* 判断条件并选择处理分支。 */
			sessions = append(sessions, byProfile[candidate.ID]...) /* 更新 sessions 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	kind := d.Connector               /* 更新 kind 的值。 */
	if kind == "" && profile != nil { /* 判断条件并选择处理分支。 */
		kind = profileTransport(*profile) /* 更新 kind 的值。 */
		if profile.Mode == "listener" {   /* 判断条件并选择处理分支。 */
			kind = strings.ToUpper(profile.Network) /* 更新 kind 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	protocolID, version := "", "" /* 更新 version 的值。 */
	bindingRevision := int64(0)
	if d.Connector == "HTTP" || d.Connector == "MQTT" { /* 判断条件并选择处理分支。 */
		protocolID, version = parser.StandardProtocolID, "1.0.0" /* 更新 version 的值。 */
	} else if binding, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, d.ProductID); e == nil { /* 结束当前表达式或代码块。 */
		protocolID, version = binding.ProtocolID, binding.Version /* 更新 version 的值。 */
		bindingRevision = binding.UpdatedAt
	} else if profile != nil { /* 结束当前表达式或代码块。 */
		protocolID, version = profile.ProtocolID, profile.ProtocolVersion /* 更新 version 的值。 */
	} /* 结束当前表达式或代码块。 */
	alarms, err := s.engine.Repo.ListAlarms(r.Context(), ports.AlarmFilter{TenantID: tenant, DeviceID: d.ID, Limit: 5}) /* 更新 err 的值。 */
	if err != nil {                                                                                                     /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	revocations, err := s.engine.Repo.ListCredentialRevocations(r.Context(), tenant, d.ID, false) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
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
	indexes, err := s.engine.Repo.ListRawIndexes(r.Context(), ports.RawFilter{TenantID: tenant, DeviceID: d.ID, Start: since, Limit: 30}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                       /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
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
		ingest["rawReceived"] = true                      /* 执行当前语句并推进处理流程。 */
		ingest["rawMessageId"] = idx.MessageID            /* 执行当前语句并推进处理流程。 */
		ingest["receivedAt"] = idx.ReceivedAt             /* 执行当前语句并推进处理流程。 */
		ingest["stage"] = "RAW_RECEIVED"                  /* 执行当前语句并推进处理流程。 */
		ingest["parseError"] = idx.ParseError             /* 执行当前语句并推进处理流程。 */
		ingest["parseAttemptedAt"] = idx.ParseAttemptedAt /* 执行当前语句并推进处理流程。 */
		if idx.ParseError != "" {                         /* 判断条件并选择处理分支。 */
			ingest["stage"] = "PARSE_FAILED" /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if standard, e := s.engine.Repo.GetStandardMessageByRaw(r.Context(), tenant, idx.MessageID); e == nil { /* 判断条件并选择处理分支。 */
			ingest["parsed"] = true              /* 执行当前语句并推进处理流程。 */
			ingest["stage"] = "PARSED"           /* 执行当前语句并推进处理流程。 */
			ingest["standardMessage"] = standard /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if idx.ReceivedAt < time.Now().Add(-15*time.Minute).UnixMilli() {
			ingest["stale"] = true
		} else if ingest["parsed"] == true && parsedCount >= 2 {
			ingest["continuouslyUpdating"] = true
		}
	} /* 结束当前表达式或代码块。 */
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
	var parent any                                        /* 声明 parent。 */
	if !deviceAllowed(r.Context(), tenant, d.GatewayID) { /* 判断条件并选择处理分支。 */
		d.GatewayID = "" /* 更新 d.GatewayID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if d.GatewayID != "" { /* 判断条件并选择处理分支。 */
		if gateway, e := s.engine.Repo.GetManagedDevice(r.Context(), tenant, d.GatewayID); e == nil { /* 判断条件并选择处理分支。 */
			parent = map[string]any{"id": gateway.ID, "name": gateway.Name} /* 更新 parent 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	release, _ := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, protocolID, version)                      /* 更新 _ 的值。 */
	canCommand := protocolworker.HasCapability(release, "encode") && (profile == nil || profile.EdgeNodeID == "") /* 更新 canCommand 的值。 */
	if d.GatewayID != "" {                                                                                        /* 判断条件并选择处理分支。 */
		canCommand = canCommand && profile != nil /* 更新 canCommand 的值。 */
		if profile != nil {                       /* 判断条件并选择处理分支。 */
			binding, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, profile.ProductID) /* 更新 e 的值。 */
			if e != nil {                                                                                 /* 判断条件并选择处理分支。 */
				canCommand = false /* 更新 canCommand 的值。 */
			} else { /* 结束当前表达式或代码块。 */
				outer, e := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, binding.ProtocolID, binding.Version) /* 更新 e 的值。 */
				canCommand = canCommand && e == nil && protocolworker.HasCapability(outer, "encode")                   /* 更新 canCommand 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	diagnosis := s.connectionDiagnosis(r.Context(), tenant, d, p, productErr == nil, isChild, parent != nil, profile, ingest, selectedRaw)
	write(w, 200, map[string]any{"diagnosis": diagnosis, "parent": parent, "accessInfo": s.deviceAccessInfo(d, p), "ingest": ingest, "recentAlarms": alarms, "revocations": revocations, "mqttCommandAvailable": d.Connector == "MQTT" && s.onboarding.PublishCommand != nil, "device": d.Public(p), "product": p, "connector": kind, "protocolId": protocolID, "protocolVersion": version, "canCommand": canCommand, "profile": profile, "profiles": candidates, "connection": state, "sessions": sessions, "latest": latest, "latestProperties": properties, "credentialSupported": d.UsesPlatformCredentials(p), "credentialEnabled": d.UsesPlatformCredentials(p) && d.SecretHash != ""}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func profileTransport(p model.DeviceAccessProfile) string { /* 定义 profileTransport 函数。 */
	if p.WireFormat == "rtu_over_tcp" { /* 判断条件并选择处理分支。 */
		return "MODBUS_RTU_TCP" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.Mode == "listener" { /* 判断条件并选择处理分支。 */
		return strings.ToUpper(p.Network) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch p.Network { /* 根据条件选择处理路径。 */
	case "serial": /* 处理当前分支。 */
		return "MODBUS_RTU" /* 返回当前处理结果。 */
	case "opc_ua": /* 处理当前分支。 */
		return "OPC_UA" /* 返回当前处理结果。 */
	case "snmp": /* 处理当前分支。 */
		return "SNMP" /* 返回当前处理结果。 */
	case "bacnet": /* 处理当前分支。 */
		return "BACNET" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "MODBUS_TCP" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

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
