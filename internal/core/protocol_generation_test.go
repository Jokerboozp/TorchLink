package core

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"testing"
)

func TestGenerateMessageWithoutAI(t *testing.T) {
	engine := &Engine{}
	draft, err := engine.GenerateProtocolAssistant(context.Background(), "tenant", ProtocolAssistantInput{InputKind: "sample", DocumentText: `{"messageType":"ALARM_REPORT","data":{"smoke":true},"event":{"alarmType":"FIRE"}}`, Transport: "HTTP", PayloadFormat: "json"})
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

type hexMappingAI struct{ protocolAssistantAI }

func (hexMappingAI) GenerateJSON(context.Context, string, string, string) (string, error) {
	return `{"name":"HEX temperature","protocol":"temp","transport":"MQTT","payloadFormat":"hex","parserType":"configurable_hex_parser","messageType":"PROPERTY_REPORT","config":{"startHex":"AA","fields":[{"name":"temperature","offset":1,"length":2,"type":"uint16","endian":"big","scale":0.1}]},"fields":[{"name":"temperature"}]}`, nil
}
func TestGenerateFixedHexMappingAndPreview(t *testing.T) {
	engine := &Engine{AI: hexMappingAI{}}
	draft, err := engine.GenerateProtocolAssistant(context.Background(), "tenant", ProtocolAssistantInput{InputKind: "sample", PayloadFormat: "hex", SamplePayload: "AA 00 FA", PointTable: "temperature starts at byte 1, uint16 big endian, scale 0.1"})
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
