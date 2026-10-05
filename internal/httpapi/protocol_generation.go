package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/aiworkflow"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
)

func generatedMapping(kind string) bool {
	return kind == "configurable_json_parser" || kind == "configurable_hex_parser" || kind == parser.ModbusTCPParserName || kind == parser.ModbusRTUParserName
}

func (s *Server) saveGeneratedProtocol(w http.ResponseWriter, r *http.Request, id, version string, draft model.ProtocolAssistantDraft, payload json.RawMessage, format string) {
	id, version = strings.TrimSpace(id), strings.TrimSpace(version)
	if !protocolSegmentV2.MatchString(id) || !protocolSegmentV2.MatchString(version) || strings.TrimSpace(draft.Name) == "" {
		problem(w, 422, "请填写协议名称、标识和版本")
		return
	}
	if format != "" {
		draft.PayloadFormat = format
	}
	now := time.Now().UnixMilli()
	tenant := claims(r).TenantID
	release := model.ProtocolRelease{TenantID: tenant, ProtocolID: id, Version: version, ParserType: draft.ParserType, Transport: draft.Transport, PayloadFormat: draft.PayloadFormat, Config: draft.Config, Status: "DRAFT", CreatedAt: now, Artifact: map[string]any{"generatedMapping": true}}
	if err := validateGeneratedMapping(release); err != nil {
		problem(w, 422, err.Error())
		return
	}
	if _, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version); err == nil {
		problem(w, 409, "该版本已存在，请使用新的版本号")
		return
	} else if !errors.Is(err, model.ErrNotFound) {
		problem(w, 500, err.Error())
		return
	}
	var preview *model.StandardMessage
	if len(payload) > 0 && string(payload) != "null" {
		text, err := assistantPayloadText(payload, draft.PayloadFormat)
		if err != nil {
			problem(w, 422, err.Error())
			return
		}
		preview, err = aiworkflow.PreviewProtocolAssistant(draft, tenant, text)
		if err != nil {
			problem(w, 422, "样本解析失败："+err.Error())
			return
		}
		s.engine.ProtocolsChanged(release.TenantID)
		release.Status = "VALIDATED"
	}
	definition, err := s.engine.Repo.GetProtocolDefinition(r.Context(), tenant, id)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		problem(w, 500, err.Error())
		return
	}
	if errors.Is(err, model.ErrNotFound) {
		definition = model.ProtocolDefinition{TenantID: tenant, ID: id, Name: draft.Name, Description: draft.Description, CreatedAt: now, UpdatedAt: now}
		if err = s.engine.Repo.SaveProtocolDefinition(r.Context(), definition); err != nil {
			problem(w, 500, err.Error())
			return
		}
	}
	if err = s.engine.Repo.CreateProtocolRelease(r.Context(), release); err != nil {
		problem(w, 409, err.Error())
		return
	}
	s.audit(r, "protocol.generated.save", "protocolRelease", id+"@"+version, map[string]any{"status": release.Status})
	write(w, 201, map[string]any{"release": release, "definition": definition, "standardMessage": preview})
}

func validateGeneratedMapping(release model.ProtocolRelease) error {
	if !generatedMapping(release.ParserType) {
		return errors.New("该协议需要 Go 源码")
	}
	if release.ParserType == parser.ModbusTCPParserName || release.ParserType == parser.ModbusRTUParserName {
		want := "MODBUS_TCP"
		if release.ParserType == parser.ModbusRTUParserName {
			want = "MODBUS_RTU"
		}
		if release.Transport != want || release.PayloadFormat != "hex" {
			return errors.New("点表协议的传输方式与格式不一致")
		}
		if err := aiworkflow.NormalizeGeneratedModbusConfig(release.Config); err != nil {
			return err
		}
	} else {
		if release.Transport != "MQTT" && release.Transport != "HTTP" {
			return errors.New("报文映射支持 MQTT / HTTP；TCP / UDP 接入请使用含拆帧能力的 Go 源码")
		}
		want := "json"
		if release.ParserType == "configurable_hex_parser" {
			want = "hex"
		}
		if release.PayloadFormat != want {
			return errors.New("报文格式与解析器不一致")
		}
	}
	return validateProtocolReleaseV2(release)
}

type protocolPreviewInput struct {
	RawMessageID string          `json:"rawMessageId"`
	MessageKind  string          `json:"messageKind"`
	Payload      json.RawMessage `json:"payload"`
	StartAddress *int            `json:"startAddress"`
	ReadOnly     bool            `json:"readOnly"`
	DeviceID     string          `json:"deviceId"`
	State        map[string]any  `json:"state"`
	Operation    string          `json:"operation"`
	Chunks       []string        `json:"chunks"`
	Datagram     bool            `json:"datagram"`
	Command      map[string]any  `json:"command"`
	Expected     map[string]any  `json:"expected"`
	Now          int64           `json:"now"`
}

func (s *Server) previewGeneratedRelease(w http.ResponseWriter, r *http.Request) {
	var input protocolPreviewInput
	if decode(w, r, &input) != nil {
		return
	}
	if input.Operation == "" {
		input.Operation = "decode"
	}
	if input.Operation != "decode" && input.Operation != "ingress" && input.Operation != "encode" {
		problem(w, 422, "请选择 ingress、decode 或 encode 操作")
		return
	}
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), claims(r).TenantID, r.PathValue("id"), r.PathValue("version"))
	if err != nil {
		problem(w, 404, "protocol release not found")
		return
	}
	if release.Status == "REVOKED" {
		problem(w, 409, "已撤销的版本不能校验")
		return
	}
	standard := release.ParserType == parser.StandardParserName
	if !generatedMapping(release.ParserType) && release.ParserType != parser.GoProtocolParserName && !standard {
		problem(w, 422, "此版本不支持解析预览")
		return
	}
	var snapshot *model.RawMessage
	if input.RawMessageID != "" {
		if !requestAllows(r, "GET", "/api/v1/raw-messages/:id") {
			problem(w, 403, "没有读取原文的权限")
			return
		}
		// The scoped repository checks both tenant and the current user's device
		// scope. Never accept a browser-supplied raw snapshot or archive location.
		index, loadErr := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, input.RawMessageID)
		if loadErr != nil {
			problem(w, 404, "raw message not found")
			return
		}
		raw, loadErr := s.engine.GetRaw(r.Context(), index)
		if loadErr != nil {
			problem(w, 500, "raw archive could not be read")
			return
		}
		if raw.ProtocolID != release.ProtocolID || raw.ProtocolVersion != release.Version {
			problem(w, 409, "原文归档的协议版本与所选版本不一致")
			return
		}
		if input.DeviceID != "" && input.DeviceID != raw.DeviceID {
			problem(w, 422, "原文设备与模拟设备标识不一致")
			return
		}
		snapshot = &raw
		input.ReadOnly = true
		input.DeviceID = raw.DeviceID
		input.Now = raw.ReceivedAt
		if len(input.Payload) == 0 {
			input.Payload = append(json.RawMessage(nil), raw.Payload...)
		}
		if input.State == nil {
			input.State, _ = jsonValue(raw.Metadata["protocolState"]).(map[string]any)
		}
	}
	config, ok := jsonValue(release.Config).(map[string]any)
	if !ok && !standard {
		problem(w, 422, "protocol mapping is missing")
		return
	}
	if config == nil {
		config = map[string]any{}
	}
	if input.StartAddress != nil {
		config["startAddress"] = *input.StartAddress
	}
	var message *model.StandardMessage
	var actual map[string]any
	state, err := json.Marshal(input.State)
	if err != nil || len(state) > protocolworker.MaxStateBytes {
		problem(w, 422, "帧前状态不能超过 64 KiB")
		return
	}
	if input.Now == 0 {
		input.Now = time.Now().UnixMilli()
	}
	if input.Now < 0 {
		problem(w, 422, "样本时间必须为有效毫秒时间戳")
		return
	}
	expected, _ := json.Marshal(input.Expected)
	if len(expected) > 64<<10 {
		problem(w, 422, "预期结果不能超过 64 KiB")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if standard {
		input.ReadOnly = true
		if input.Operation != "decode" {
			problem(w, 422, "平台标准协议仅支持 decode 解析")
			return
		}
		var payload string
		payload, err = assistantPayloadText(input.Payload, "json")
		if err == nil {
			var raw model.RawMessage
			raw, err = previewRaw(release, firstNonBlank(input.DeviceID, "protocol_preview"), input.Now, payload, state, snapshot)
			if err == nil {
				if snapshot == nil {
					raw.Headers = map[string]string{"messageKind": input.MessageKind}
				} else if input.MessageKind != "" && input.MessageKind != raw.Headers["messageKind"] {
					problem(w, 422, "消息类型与原文接入类型不一致，请解除原文关联后试跑自填样本")
					return
				}
				message, err = (parser.StandardParser{}).Parse(raw)
			}
		}
		actual = map[string]any{"standardMessage": message}
	} else if release.ParserType == parser.GoProtocolParserName {
		// Preview uses standalone invocations. Do not use a live resident
		// Worker, ingest a RawMessage, or trust browser-provided artifact paths.
		if artifact, ok := config["artifact"].(map[string]any); ok {
			delete(artifact, "workerMode")
		}
		deviceID := strings.TrimSpace(input.DeviceID)
		if len(deviceID) > 128 {
			problem(w, 422, "模拟设备标识不能超过 128 字节")
			return
		}
		if deviceID == "" {
			deviceID = "protocol_preview"
		}
		execution := release
		execution.Config = config
		switch input.Operation {
		case "decode":
			var payload string
			payload, err = assistantPayloadText(input.Payload, release.PayloadFormat)
			if err == nil {
				message, err = s.previewDecode(ctx, execution, deviceID, input.Now, payload, state, snapshot)
			}
			actual = map[string]any{"standardMessage": message}
		case "encode":
			if len(input.Command) == 0 {
				problem(w, 422, "请填写命令 JSON")
				return
			}
			var response protocolworker.Response
			response, err = protocolworker.Call(ctx, s.cfg.DataDir, execution, protocolworker.Request{Operation: "encode", DeviceID: deviceID, State: state, Command: input.Command, Now: input.Now})
			actual, _ = jsonValue(response).(map[string]any)
		case "ingress":
			actual, err = s.previewIngress(ctx, execution, input, deviceID, state, snapshot)
		}
	} else {
		if input.Operation != "decode" {
			problem(w, 422, "映射协议仅支持 decode 解析")
			return
		}
		payload, payloadErr := assistantPayloadText(input.Payload, release.PayloadFormat)
		if payloadErr != nil {
			problem(w, 422, payloadErr.Error())
			return
		}
		draft := model.ProtocolAssistantDraft{Protocol: release.ProtocolID, ParserType: release.ParserType, Transport: release.Transport, PayloadFormat: release.PayloadFormat, Config: config}
		message, err = aiworkflow.PreviewProtocolAssistant(draft, claims(r).TenantID, payload)
		actual = map[string]any{"standardMessage": message}
	}
	if err != nil {
		problem(w, 422, "样本解析失败："+err.Error())
		return
	}
	actual, _ = jsonValue(actual).(map[string]any)
	var comparison map[string]any
	matched := true
	if input.Expected != nil {
		differences := previewDifferences(actual, input.Expected, "$")
		matched = len(differences) == 0
		comparison = map[string]any{"matched": matched, "expected": input.Expected, "actual": actual, "differences": differences}
	}
	if release.Status == "DRAFT" && !input.ReadOnly && input.Operation == "decode" && matched {
		if err = s.engine.Repo.UpdateProtocolReleaseStatus(r.Context(), release.TenantID, release.ProtocolID, release.Version, "VALIDATED", 0); err != nil {
			problem(w, 500, err.Error())
			return
		}
		s.engine.ProtocolsChanged(release.TenantID)
		release.Status = "VALIDATED"
	}
	result := map[string]any{"release": release, "standardMessage": message, "operation": input.Operation, "operationResult": actual}
	if comparison != nil {
		result["comparison"] = comparison
	}
	s.audit(r, "protocol.generated.preview", "protocolRelease", release.ProtocolID+"@"+release.Version, map[string]any{"status": release.Status, "operation": input.Operation, "readOnly": input.ReadOnly})
	write(w, 200, result)
}

func (s *Server) previewDecode(ctx context.Context, release model.ProtocolRelease, device string, now int64, payload string, state json.RawMessage, snapshot *model.RawMessage) (*model.StandardMessage, error) {
	raw, err := previewRaw(release, device, now, payload, state, snapshot)
	if err != nil {
		return nil, err
	}
	return (parser.ExternalParser{Root: s.cfg.DataDir}).ParseWithContext(ctx, raw, release.Config)
}

func previewRaw(release model.ProtocolRelease, device string, now int64, payload string, state json.RawMessage, snapshot *model.RawMessage) (model.RawMessage, error) {
	value, err := aiworkflow.ProtocolAssistantPayload(release.PayloadFormat, payload)
	if err != nil {
		return model.RawMessage{}, err
	}
	raw := model.RawMessage{MessageID: "raw_protocol_preview", Source: "protocol-preview", TenantID: release.TenantID, ProductID: "protocol_preview", DeviceID: device, Protocol: release.ProtocolID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, Transport: release.Transport, PayloadFormat: release.PayloadFormat, Payload: value, ReceivedAt: now, Metadata: map[string]any{"protocolState": state}}
	if snapshot != nil {
		// Keep all archived context, including product, gateway, headers and
		// vendor metadata. Only the sample bytes and frame state are editable.
		raw = *snapshot
		raw.Payload = value
		raw.Metadata, _ = jsonValue(snapshot.Metadata).(map[string]any)
		if raw.Metadata == nil {
			raw.Metadata = map[string]any{}
		}
		raw.Metadata["protocolState"] = state
	}
	return raw, nil
}

// Simulate byte buffers only: no listener, socket, registration, archive or
// message ingestion is involved. State advances only after complete frames,
// exactly as the live ingress contract does; children remain observations.
func (s *Server) previewIngress(ctx context.Context, release model.ProtocolRelease, input protocolPreviewInput, device string, state json.RawMessage, snapshot *model.RawMessage) (map[string]any, error) {
	chunks := input.Chunks
	if len(chunks) == 0 {
		value, err := assistantPayloadText(input.Payload, "hex")
		if err != nil {
			return nil, err
		}
		chunks = []string{value}
	}
	if len(chunks) > 32 {
		return nil, errors.New("单次最多预览 32 个报文片段")
	}
	buffer := []byte{}
	frames, attempts := []map[string]any{}, []map[string]any{}
	for chunkIndex, chunk := range chunks {
		data, err := hex.DecodeString(strings.Join(strings.Fields(chunk), ""))
		if err != nil || len(data) == 0 || len(buffer)+len(data) > protocolworker.MaxFrameBytes {
			return nil, errors.New("每次接入缓冲须为 1 字节至 64 KiB 的 HEX 数据")
		}
		buffer = append(buffer, data...)
		for len(buffer) > 0 {
			if len(attempts) >= 64 {
				return nil, errors.New("单次最多执行 64 次拆帧，请缩小样本")
			}
			response, err := protocolworker.Call(ctx, s.cfg.DataDir, release, protocolworker.Request{Operation: "ingress", Data: hex.EncodeToString(buffer), DeviceID: device, State: state, Now: input.Now})
			if err != nil {
				return nil, err
			}
			if input.Datagram && (response.NeedMore || response.Consumed != len(buffer)) {
				return nil, errors.New("UDP 样本必须由 ingress 一次消费完整数据报")
			}
			attempt := map[string]any{"chunk": chunkIndex + 1, "bufferHex": strings.ToUpper(hex.EncodeToString(buffer)), "consumed": response.Consumed, "needMore": response.NeedMore, "reply": response.Reply, "correlationId": response.CorrelationID, "deviceId": response.DeviceID, "stateBefore": state, "stateAfter": response.State, "children": response.Children}
			attempts = append(attempts, attempt)
			if response.NeedMore {
				break
			}
			if len(frames) > 0 && response.DeviceID != device {
				return nil, errors.New("同一模拟连接识别出了不同设备，请拆开样本")
			}
			frameHex := strings.ToUpper(hex.EncodeToString(buffer[:response.Consumed]))
			attempt["frameHex"] = frameHex
			message, decodeErr := s.previewDecode(ctx, release, response.DeviceID, input.Now, frameHex, state, snapshot)
			if decodeErr != nil {
				attempt["decodeError"] = decodeErr.Error()
			} else {
				attempt["standardMessage"] = message
			}
			frames = append(frames, attempt)
			buffer = buffer[response.Consumed:]
			state = append(json.RawMessage(nil), response.State...)
			device = response.DeviceID
		}
	}
	return map[string]any{"frames": frames, "attempts": attempts, "remainingHex": strings.ToUpper(hex.EncodeToString(buffer)), "needMore": len(buffer) > 0, "state": state, "deviceId": device}, nil
}

// Expected objects match a subset; arrays match each position and their length.
func previewDifferences(actual, expected any, path string) []map[string]any {
	result := []map[string]any{}
	if wanted, ok := expected.(map[string]any); ok {
		if values, ok := actual.(map[string]any); ok {
			keys := make([]string, 0, len(wanted))
			for key := range wanted {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value, exists := values[key]
				if !exists {
					result = append(result, map[string]any{"path": path + "." + key, "expected": wanted[key], "actual": nil, "missing": true})
				} else {
					result = append(result, previewDifferences(value, wanted[key], path+"."+key)...)
				}
			}
			return result
		}
	} else if wanted, ok := expected.([]any); ok {
		if values, ok := actual.([]any); ok && len(values) == len(wanted) {
			for i, value := range wanted {
				result = append(result, previewDifferences(values[i], value, fmt.Sprintf("%s[%d]", path, i))...)
			}
			return result
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		result = append(result, map[string]any{"path": path, "expected": expected, "actual": actual})
	}
	return result
}
