package core

import (
	"context"
	"io"
	"log/slog"
	"testing"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type protocolAssistantAI struct{}

func (protocolAssistantAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{}, nil
}
func (protocolAssistantAI) Chat(context.Context, string, string) (string, error) {
	return "巡检建议", nil
}
func (protocolAssistantAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, nil
}
func (protocolAssistantAI) Health(context.Context) error { return nil }
func (protocolAssistantAI) GenerateJSON(context.Context, string, string, string) (string, error) {
	return `{"name":"测试 Go 协议","protocol":"test-modbus","transport":"MODBUS_TCP","payloadFormat":"hex","parserType":"modbus_coil_parser","messageType":"PROPERTY_REPORT","config":{"frame":"tcp","startAddress":0,"functionCode":1,"fields":[{"name":"smoke","coilAddress":0}]},"fields":[{"name":"smoke","label":"烟雾","type":"boolean","coilAddress":0,"dataType":"BOOL"}]}`, nil
}

func TestProtocolAssistantBuildAndPreview(t *testing.T) {
	draft := model.ProtocolAssistantDraft{ParserType: parser.ModbusCoilParserName, Protocol: "modbus", Transport: "MODBUS_TCP", PayloadFormat: "hex", MessageType: model.PropertyReport, Config: map[string]any{"frame": "tcp", "startAddress": 0, "functionCode": 1, "fields": []any{map[string]any{"name": "smoke", "coilAddress": 0}}}}
	message, err := PreviewProtocolAssistant(draft, "tenant-test", "00 01 00 00 00 04 01 01 01 01")
	if err != nil {
		t.Fatal(err)
	}
	if message.Properties["smoke"] != true || message.Parser != parser.ModbusCoilParserName {
		t.Fatalf("unexpected protocol preview %#v", message)
	}
}

func TestProtocolAssistantRejectsInvalidGoMapping(t *testing.T) {
	err := parser.ValidateModbusCoilConfig(map[string]any{"fields": []any{map[string]any{"name": "x", "coilAddress": -1}}})
	if err == nil {
		t.Fatal("invalid coil address was accepted")
	}
}

func TestGenerateProtocolAssistant(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) {
		return protocolAssistantAI{}.GenerateJSON(context.Background(), "", "", "")
	}}
	engine.HarnessTokens = aitest.Tokens()
	draft, err := engine.GenerateProtocolAssistant(aitest.Context(context.Background()), "tenant-test", ProtocolAssistantInput{PointTable: "温度：第 2 字节，单位 0.1 度", SamplePayload: "01 2A"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Source != "" || draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 1 || draft.Preview != nil {
		t.Fatalf("unexpected generated draft %#v", draft)
	}
}

func TestInspectDeviceHealth(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = aiadapter.NoopAI{}
	ctx := context.Background()
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "device-health", TenantID: "tenant-test", ProductID: "sensor", Name: "测试设备"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-test", ProductID: "sensor", DeviceID: "device-health", BusinessStatus: "OFFLINE", DataStatus: "SILENT", LastSeenAt: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-health", TenantID: "tenant-test", DeviceID: "device-health", Status: "ACTIVE", AlarmLevel: "HIGH"}); err != nil {
		t.Fatal(err)
	}
	report, err := engine.InspectDeviceHealth(ctx, "tenant-test")
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts["total"] != 1 || report.Counts["offline"] != 1 || report.Items[0].ActiveAlarmCount != 1 {
		t.Fatalf("unexpected health report %#v", report)
	}
}
