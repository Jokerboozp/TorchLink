package aiworkflow

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/aiprompt"
	"path/filepath"
	"strings"

	"iot-platform/internal/aioutput"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

var ErrProtocolInput = errors.New("invalid protocol input")

// ProtocolAssistantInput is the human-provided context for protocol
// generation. DocumentText is extracted from a PDF/Office/text point table by
// the HTTP layer, keeping the AI workflow independent from multipart parsing.
type ProtocolAssistantInput struct {
	InputKind        string
	Name             string
	Protocol         string
	Transport        string
	PayloadFormat    string
	DocumentText     string
	PointTable       string
	SamplePayload    string
	DocumentFilename string
	DocumentData     []byte
}

func (e *Service) GenerateProtocolAssistant(ctx context.Context, tenant string, in ProtocolAssistantInput) (model.ProtocolAssistantDraft, error) {
	if draft, handled, err := buildUploadedProtocol(in); handled {
		if err != nil {
			return draft, fmt.Errorf("%w: %v", ErrProtocolInput, err)
		}
		return draft, nil
	}
	if in.InputKind == "sample" && strings.TrimSpace(in.SamplePayload) == "" {
		in.SamplePayload = strings.TrimSpace(in.DocumentText)
	}
	if len(in.DocumentData) > 0 && strings.EqualFold(filepath.Ext(in.DocumentFilename), ".xlsx") {
		return BuildProtocolAssistantSpreadsheetDraft(in)
	}
	if !e.AIWorkflowsReady() {
		return model.ProtocolAssistantDraft{}, ErrAIWorkflowsUnavailable
	}
	if strings.TrimSpace(in.DocumentText) == "" && strings.TrimSpace(in.PointTable) == "" && strings.TrimSpace(in.SamplePayload) == "" {
		return model.ProtocolAssistantDraft{}, errors.New("protocol document or point table is required")
	}
	prompt := aiprompt.ProtocolAssistant(buildProtocolAssistantPrompt(in))
	query := retrievalQuery("协议接入 点表 报文解析", []string{in.Name, in.Protocol, in.Transport, in.PayloadFormat})
	result, err := e.runBusinessWorkflow(ctx, tenant, WorkflowProtocolAssist, aiprompt.ProtocolAssistVersion, prompt, query, []string{"query_knowledge_base"}, 8192)
	if err != nil {
		return model.ProtocolAssistantDraft{}, fmt.Errorf("generate protocol draft: %w", err)
	}
	content := result.Answer
	draft, err := decodeProtocolAssistant(content)
	if err != nil {
		return model.ProtocolAssistantDraft{}, err
	}
	if draft.Name == "" {
		draft.Name = strings.TrimSpace(in.Name)
	}
	if draft.Name == "" {
		draft.Name = "AI 协议解析草稿"
	}
	if draft.Protocol == "" {
		draft.Protocol = strings.TrimSpace(in.Protocol)
	}
	if draft.Protocol == "" {
		draft.Protocol = "custom-go-worker"
	}
	if draft.Transport == "" {
		draft.Transport = strings.ToUpper(strings.TrimSpace(in.Transport))
	}
	if draft.Transport == "" {
		draft.Transport = "MQTT"
	}
	if draft.PayloadFormat == "" {
		draft.PayloadFormat = strings.ToLower(strings.TrimSpace(in.PayloadFormat))
	}
	if draft.PayloadFormat != "hex" {
		draft.PayloadFormat = "json"
	}
	if draft.MessageType == "" {
		draft.MessageType = model.PropertyReport
	}
	if draft.ParserType == "" {
		draft.ParserType = parser.GoProtocolParserName
	}
	if draft.ParserType != parser.GoProtocolParserName && draft.ParserType != parser.ModbusCoilParserName && draft.ParserType != "configurable_json_parser" && draft.ParserType != "configurable_hex_parser" {
		return model.ProtocolAssistantDraft{}, fmt.Errorf("unsupported protocol assistant parserType %q", draft.ParserType)
	}
	draft.Source = ""
	draft.Setup = ""
	if draft.Config == nil {
		draft.Config = map[string]any{}
	}
	if draft.SamplePayload == nil && strings.TrimSpace(in.SamplePayload) != "" {
		draft.SamplePayload = assistantSampleValue(draft.PayloadFormat, in.SamplePayload)
	}
	if len(draft.Fields) == 0 && draft.ParserType != parser.GoProtocolParserName {
		return model.ProtocolAssistantDraft{}, errors.New("AI did not return any protocol fields")
	}
	if draft.ParserType == parser.GoProtocolParserName {
		draft.Warnings = append(draft.Warnings, "请到 Go 源码接入上传 .go 或项目 ZIP；平台编译并验证样例后发布。")
	} else if strings.TrimSpace(in.SamplePayload) != "" {
		if preview, previewErr := PreviewProtocolAssistant(draft, tenant, in.SamplePayload); previewErr != nil {
			draft.Warnings = append(draft.Warnings, "样本解析失败："+previewErr.Error())
		} else {
			draft.Preview = preview
		}
	} else {
		// Without a sample the config is only checked for shape; publishing
		// still needs a real sample to pass the preview.
		draft.Warnings = append(draft.Warnings, protocolAssistantConfigWarnings(draft)...)
	}
	return draft, nil
}

func buildProtocolAssistantPrompt(in ProtocolAssistantInput) string {
	var b strings.Builder
	b.WriteString("用户补充的协议名称：")
	b.WriteString(limitAssistantText(in.Name, 256))
	b.WriteString("\n协议标识：")
	b.WriteString(limitAssistantText(in.Protocol, 128))
	b.WriteString("\n传输方式：")
	b.WriteString(limitAssistantText(in.Transport, 64))
	b.WriteString("\n载荷格式：")
	b.WriteString(limitAssistantText(in.PayloadFormat, 32))
	b.WriteString("\n样本报文：\n")
	b.WriteString(limitAssistantText(in.SamplePayload, 12000))
	b.WriteString("\n点表文本：\n")
	b.WriteString(limitAssistantText(in.PointTable, 24000))
	b.WriteString("\n协议文档提取文本：\n")
	b.WriteString(limitAssistantText(in.DocumentText, 48000))
	b.WriteString("\n请严格按指定 JSON 结构返回，并把无法确认的内容放入 warnings。")
	return b.String()
}

func decodeProtocolAssistant(content string) (model.ProtocolAssistantDraft, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(aioutput.ExtractJSON(content)), &raw); err != nil {
		return model.ProtocolAssistantDraft{}, fmt.Errorf("decode protocol draft JSON: %w", err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return model.ProtocolAssistantDraft{}, err
	}
	var draft model.ProtocolAssistantDraft
	if err = json.Unmarshal(b, &draft); err != nil {
		return model.ProtocolAssistantDraft{}, fmt.Errorf("decode protocol draft fields: %w", err)
	}
	draft.MessageType = model.MessageType(strings.ToUpper(strings.TrimSpace(string(draft.MessageType))))
	if draft.MessageType != "" && !validProtocolAssistantMessageType(draft.MessageType) {
		return model.ProtocolAssistantDraft{}, fmt.Errorf("unsupported messageType %q", draft.MessageType)
	}
	if len(draft.Fields) == 0 {
		if properties, ok := raw["properties"].(map[string]any); ok {
			for name, value := range properties {
				expression := strings.TrimSpace(fmt.Sprint(value))
				if expression == "" {
					encoded, _ := json.Marshal(value)
					expression = string(encoded)
				}
				draft.Fields = append(draft.Fields, model.ProtocolAssistantField{Name: name, Label: name, Type: "value", Expression: expression})
			}
		}
	}
	if draft.TagExpressions == nil {
		if tags, ok := raw["tags"].(map[string]any); ok {
			draft.TagExpressions = map[string]string{}
			for name, value := range tags {
				if text, ok := value.(string); ok {
					draft.TagExpressions[name] = text
				} else {
					encoded, _ := json.Marshal(value)
					draft.TagExpressions[name] = string(encoded)
				}
			}
		}
	}
	return draft, nil
}

func PreviewProtocolAssistant(draft model.ProtocolAssistantDraft, tenant, payload string) (*model.StandardMessage, error) {
	payloadValue, err := ProtocolAssistantPayload(draft.PayloadFormat, payload)
	if err != nil {
		return nil, err
	}
	raw := model.RawMessage{MessageID: "raw_protocol_assistant", TenantID: tenant, ProductID: "protocol_assistant", DeviceID: "device_assistant", Protocol: draft.Protocol, Transport: draft.Transport, PayloadFormat: strings.ToLower(draft.PayloadFormat), Payload: payloadValue}
	switch draft.ParserType {
	case parser.ModbusCoilParserName:
		msg, parseErr := (parser.ModbusCoilParser{}).ParseWithConfig(raw, draft.Config)
		if parseErr != nil {
			return nil, parseErr
		}
		msg.Parser = parser.ModbusCoilParserName
		msg.ParserVersion = parser.ModbusCoilParserVersion
		return msg, nil
	case "configurable_json_parser":
		return (parser.ConfigurableJSONParser{}).ParseWithConfig(raw, draft.Config)
	case "configurable_hex_parser":
		return (parser.ConfigurableHexParser{}).ParseWithConfig(raw, draft.Config)
	case parser.ModbusTCPParserName:
		return (parser.ModbusTCPParser{}).ParseWithConfig(raw, draft.Config)
	case parser.ModbusRTUParserName:
		return (parser.ModbusRTUParser{}).ParseWithConfig(raw, draft.Config)
	case parser.GoProtocolParserName:
		return nil, errors.New("Go 协议 Worker 尚未上传，无法在助手内预览")
	default:
		return nil, fmt.Errorf("unsupported protocol assistant parserType %q", draft.ParserType)
	}
}

func ProtocolAssistantPayload(format, payload string) (json.RawMessage, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, errors.New("sample payload is required")
	}
	if strings.EqualFold(format, "hex") {
		cleaned := strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(payload)
		if len(cleaned)%2 != 0 {
			return nil, errors.New("hex sample payload must contain complete bytes")
		}
		if _, err := hex.DecodeString(cleaned); err != nil {
			return nil, fmt.Errorf("invalid hex sample payload: %w", err)
		}
		return json.Marshal(payload)
	}
	if !json.Valid([]byte(payload)) {
		return nil, errors.New("JSON sample payload is invalid")
	}
	return json.RawMessage(payload), nil
}

func assistantSampleValue(format, payload string) any {
	b, err := ProtocolAssistantPayload(format, payload)
	if err != nil {
		return payload
	}
	if strings.EqualFold(format, "hex") {
		return strings.TrimSpace(payload)
	}
	var value any
	if json.Unmarshal(b, &value) == nil {
		return value
	}
	return payload
}

func protocolAssistantConfigWarnings(draft model.ProtocolAssistantDraft) []string {
	warnings := []string{"未提供样本报文，解析配置尚未经过实际报文验证，发布前请用样本预览。"}
	switch draft.ParserType {
	case parser.ModbusCoilParserName:
		if err := parser.ValidateModbusCoilConfig(draft.Config); err != nil {
			warnings = append(warnings, "线圈点表配置无效："+err.Error())
		}
	case "configurable_json_parser", "configurable_hex_parser":
		if len(draft.Config) == 0 {
			warnings = append(warnings, "模型没有生成解析配置，请补充样本后重新生成。")
		}
	}
	return warnings
}

func validProtocolAssistantMessageType(value model.MessageType) bool {
	switch value {
	case model.PropertyReport, model.EventReport, model.StateChange, model.AlarmReport, model.CommandReply, model.LogReport:
		return true
	default:
		return false
	}
}

func limitAssistantText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum]) + "\n[内容已截断]"
}
