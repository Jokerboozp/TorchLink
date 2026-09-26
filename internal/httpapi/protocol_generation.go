package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
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
		preview, err = core.PreviewProtocolAssistant(draft, tenant, text)
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
		if err := core.NormalizeGeneratedModbusConfig(release.Config); err != nil {
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

func (s *Server) previewGeneratedRelease(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Payload      json.RawMessage `json:"payload"`
		StartAddress *int            `json:"startAddress"`
	}
	if decode(w, r, &input) != nil {
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
	if !generatedMapping(release.ParserType) {
		problem(w, 422, "此入口仅用于报文与点表生成的协议")
		return
	}
	config, ok := jsonValue(release.Config).(map[string]any)
	if !ok {
		problem(w, 422, "protocol mapping is missing")
		return
	}
	if input.StartAddress != nil {
		config["startAddress"] = *input.StartAddress
	}
	payload, err := assistantPayloadText(input.Payload, release.PayloadFormat)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	draft := model.ProtocolAssistantDraft{Protocol: release.ProtocolID, ParserType: release.ParserType, Transport: release.Transport, PayloadFormat: release.PayloadFormat, Config: config}
	message, err := core.PreviewProtocolAssistant(draft, claims(r).TenantID, payload)
	if err != nil {
		problem(w, 422, "样本解析失败："+err.Error())
		return
	}
	if release.Status == "DRAFT" {
		if err = s.engine.Repo.UpdateProtocolReleaseStatus(r.Context(), release.TenantID, release.ProtocolID, release.Version, "VALIDATED", 0); err != nil {
			problem(w, 500, err.Error())
			return
		}
		s.engine.ProtocolsChanged(release.TenantID)
		release.Status = "VALIDATED"
	}
	s.audit(r, "protocol.generated.preview", "protocolRelease", release.ProtocolID+"@"+release.Version, map[string]any{"status": release.Status})
	write(w, 200, map[string]any{"release": release, "standardMessage": message})
}
