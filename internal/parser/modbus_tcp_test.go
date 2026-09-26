package parser

import (
	"encoding/json"
	"testing"

	"iot-platform/internal/model"
)

func TestModbusTCPParserWithVersionedPoints(t *testing.T) {
	payload, _ := json.Marshal("00010000000701030400FA0001")
	bit := 0
	points := []model.ModbusPoint{
		{Identifier: "temperature", FunctionCode: 3, Address: 100, DataType: "int16", RegisterCount: 1, ByteOrder: "big", WordOrder: "ABCD", Scale: 0.1},
		{Identifier: "running", FunctionCode: 3, Address: 101, DataType: "uint16", RegisterCount: 1, ByteOrder: "big", WordOrder: "ABCD", Scale: 1, Bit: &bit},
	}
	message, err := (ModbusTCPParser{}).ParseWithConfig(model.RawMessage{MessageID: "raw_1", TenantID: "t", ProductID: "p", DeviceID: "d", ReceivedAt: 1, ProtocolID: "modbus", ProtocolVersion: "2.0.0", PointTableVersion: "2.0.0", Payload: payload, Metadata: map[string]any{"startAddress": 100}}, map[string]any{"points": points})
	if err != nil {
		t.Fatal(err)
	}
	if message.Properties["temperature"] != float64(25) || message.Properties["running"] != true {
		t.Fatalf("unexpected properties: %#v", message.Properties)
	}
	if message.Tags["protocolVersion"] != "2.0.0" {
		t.Fatalf("release trace was lost: %#v", message.Tags)
	}
}

func TestModbusTCPParserRejectsException(t *testing.T) {
	payload, _ := json.Marshal("000100000003018302")
	_, err := (ModbusTCPParser{}).ParseWithConfig(model.RawMessage{Payload: payload}, map[string]any{"points": []model.ModbusPoint{{Identifier: "x", FunctionCode: 3, Address: 0, DataType: "uint16"}}})
	if err == nil {
		t.Fatal("expected Modbus exception to fail parsing")
	}
}
