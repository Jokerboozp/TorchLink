package aiworkflow

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"iot-platform/internal/core"
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

func TestGenerateProtocolAssistant(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := wrap(core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))))
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
	engine := wrap(core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))))
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

func TestProtocolAssistantSpreadsheetDraftUsesGoMapping(t *testing.T) {
	draft, err := BuildProtocolAssistantSpreadsheetDraft(ProtocolAssistantInput{
		Name:             "库卡火花探测器",
		Transport:        "MODBUS_TCP",
		PayloadFormat:    "hex",
		DocumentFilename: "变量地址表.xlsx",
		DocumentData:     protocolAssistantSpreadsheetFixture(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.ParserType != parser.ModbusCoilParserName || draft.MessageType != "PROPERTY_REPORT" {
		t.Fatalf("unexpected parser/message type: %#v", draft)
	}
	if len(draft.Fields) != 2 || draft.Fields[0].CoilAddress != 100 || draft.Fields[1].CoilAddress != 3001 {
		t.Fatalf("unexpected fields: %#v", draft.Fields)
	}
	if draft.Source != "" {
		t.Fatalf("spreadsheet draft must not generate source code: %#v", draft)
	}
	if len(draft.Warnings) == 0 {
		t.Fatal("expected missing sample/serial context warning")
	}
}

func TestGenerateMessageWithoutAI(t *testing.T) {
	engine := wrap(&core.Engine{})
	draft, err := engine.GenerateProtocolAssistant(aitest.Context(context.Background()), "tenant", ProtocolAssistantInput{InputKind: "sample", DocumentText: `{"messageType":"ALARM_REPORT","data":{"smoke":true},"event":{"alarmType":"FIRE"}}`, Transport: "HTTP", PayloadFormat: "json"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Preview.MessageType != model.AlarmReport || draft.Preview.Event["alarmType"] != "FIRE" {
		t.Fatal(draft.Preview)
	}
	next, err := PreviewProtocolAssistant(draft, "tenant", `{"messageType":"PROPERTY_REPORT","data":{"smoke":false}}`)
	if err != nil || next.Properties["smoke"] != false || next.MessageType != model.PropertyReport {
		t.Fatalf("mapping hardcodes the sample: %v %v", next, err)
	}
	_, err = engine.GenerateProtocolAssistant(context.Background(), "tenant", ProtocolAssistantInput{InputKind: "sample", SamplePayload: `{invalid`, PayloadFormat: "json"})
	if !errors.Is(err, ErrProtocolInput) {
		t.Fatal(err)
	}
}
func TestGeneratedModbusPreservesExplicitZeroBasedAddress(t *testing.T) {
	in := ProtocolAssistantInput{InputKind: "point-table", Transport: "MODBUS_TCP", DocumentFilename: "points.csv", DocumentData: []byte("identifier,name,functionCode,address,addressNotation,dataType\nx,X,3,40001,zero_based,uint16\n")}
	draft, _, err := buildUploadedProtocol(in)
	if err != nil {
		t.Fatal(err)
	}
	points := draft.Config["points"].([]model.ModbusPoint)
	if points[0].Address != 40001 {
		t.Fatal(points)
	}
	if err = NormalizeGeneratedModbusConfig(draft.Config); err != nil {
		t.Fatal(err)
	}
	if draft.Config["points"].([]model.ModbusPoint)[0].Address != 40001 {
		t.Fatal("normalization changed address")
	}
	points[0].RegisterCount = 1
	points[0].DataType = "uint64"
	draft.Config["points"] = points
	if err = NormalizeGeneratedModbusConfig(draft.Config); err == nil {
		t.Fatal("unsafe field width accepted")
	}
}

func TestGeneratedCabinetBitPointPreview(t *testing.T) {
	in := ProtocolAssistantInput{InputKind: "point-table", Transport: "MODBUS_RTU", DocumentFilename: "cabinet.csv", DocumentData: []byte("identifier,name,functionCode,address,addressNotation,dataType,bit\ninput4,输入4,3,8195,zero_based,bits,3\n")}
	draft, _, err := buildUploadedProtocol(in)
	if err != nil {
		t.Fatal(err)
	}
	if err = NormalizeGeneratedModbusConfig(draft.Config); err != nil {
		t.Fatal(err)
	}
	point := draft.Config["points"].([]model.ModbusPoint)[0]
	if point.DataType != "bits" || point.Bit == nil || *point.Bit != 3 || point.Address != 8195 {
		t.Fatal("point import changed bit definition")
	}
	draft.Config["startAddress"] = 8195
	// Unit 1, function 3, one register 0x0008, CRC 0x82B9 (low byte first).
	preview, err := PreviewProtocolAssistant(draft, "tenant", "01 03 02 00 08 B9 82")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Properties["input4"] != true {
		t.Fatalf("input4 = %v", preview.Properties["input4"])
	}
}
func TestGenerateFixedHexMappingAndPreview(t *testing.T) {
	engine := wrap(&core.Engine{AIWorkflows: &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) {
		return hexMappingAI{}.GenerateJSON(context.Background(), "", "", "")
	}}, HarnessTokens: aitest.Tokens(), Repo: memory.NewRepository()})
	draft, err := engine.GenerateProtocolAssistant(aitest.Context(context.Background()), "tenant", ProtocolAssistantInput{InputKind: "sample", PayloadFormat: "hex", SamplePayload: "AA 00 FA", PointTable: "temperature starts at byte 1, uint16 big endian, scale 0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Preview == nil || draft.Preview.Properties["temperature"] != float64(25) {
		t.Fatal(draft)
	}
	next, err := PreviewProtocolAssistant(draft, "tenant", "AA 01 2C")
	if err != nil || next.Properties["temperature"] != float64(30) {
		t.Fatalf("%v %v", next, err)
	}
}

type protocolAssistantAI struct{}

func (protocolAssistantAI) GenerateJSON(context.Context, string, string, string) (string, error) {
	return `{"name":"测试 Go 协议","protocol":"test-modbus","transport":"MODBUS_TCP","payloadFormat":"hex","parserType":"modbus_coil_parser","messageType":"PROPERTY_REPORT","config":{"frame":"tcp","startAddress":0,"functionCode":1,"fields":[{"name":"smoke","coilAddress":0}]},"fields":[{"name":"smoke","label":"烟雾","type":"boolean","coilAddress":0,"dataType":"BOOL"}]}`, nil
}

func protocolAssistantSpreadsheetFixture(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	shared, err := zw.Create("xl/sharedStrings.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = shared.Write([]byte(`<sst><si><t>序号</t></si><si><t>变量名称</t></si><si><t>PLC 线圈地址</t></si><si><t>Modbus地址（十进制）</t></si><si><t>数据类型</t></si><si><t>无报出状态</t></si><si><t>报出状态</t></si><si><t>备 注</t></si><si><t>通讯心跳测试</t></si><si><t>M100</t></si><si><t>BOOL</t></si><si><t>通断循环</t></si><si><t>火花探测组1报警</t></si><si><t>M3001</t></si><si><t>持续2秒</t></si></sst>`))
	sheet, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c><c r="G1" t="s"><v>6</v></c><c r="H1" t="s"><v>7</v></c></row><row r="2"><c r="A2"><v>1</v></c><c r="B2" t="s"><v>8</v></c><c r="C2" t="s"><v>9</v></c><c r="D2"><v>100</v></c><c r="E2" t="s"><v>10</v></c><c r="F2"><v>0</v></c><c r="G2"><v>1</v></c><c r="H2" t="s"><v>11</v></c></row><row r="3"><c r="A3"><v>2</v></c><c r="B3" t="s"><v>12</v></c><c r="C3" t="s"><v>13</v></c><c r="D3"><v>3001</v></c><c r="E3" t="s"><v>10</v></c><c r="F3"><v>0</v></c><c r="G3"><v>1</v></c><c r="H3" t="s"><v>14</v></c></row></sheetData></worksheet>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

type hexMappingAI struct{}

func (hexMappingAI) GenerateJSON(context.Context, string, string, string) (string, error) {
	return `{"name":"HEX temperature","protocol":"temp","transport":"MQTT","payloadFormat":"hex","parserType":"configurable_hex_parser","messageType":"PROPERTY_REPORT","config":{"startHex":"AA","fields":[{"name":"temperature","offset":1,"length":2,"type":"uint16","endian":"big","scale":0.1}]},"fields":[{"name":"temperature"}]}`, nil
}
