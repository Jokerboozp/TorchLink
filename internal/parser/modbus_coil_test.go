package parser

import (
	"encoding/json"
	"testing"

	"iot-platform/internal/model"
)

func TestModbusCoilParserTCPResponse(t *testing.T) {
	message, err := (ModbusCoilParser{}).ParseWithConfig(model.RawMessage{
		MessageID: "raw_coil", TenantID: "tenant", ProductID: "product", DeviceID: "device", ReceivedAt: 123,
		Protocol: "modbus", PayloadFormat: "hex", Payload: json.RawMessage(`"00 01 00 00 00 05 01 01 02 03 01"`),
	}, map[string]any{
		"frame": "tcp", "startAddress": 0, "messageType": "PROPERTY_REPORT",
		"fields": []any{
			map[string]any{"name": "coil_0", "coilAddress": 0},
			map[string]any{"name": "coil_1", "coilAddress": 1},
			map[string]any{"name": "coil_8", "coilAddress": 8},
			map[string]any{"name": "coil_9", "coilAddress": 9},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Properties["coil_0"] != true || message.Properties["coil_1"] != true || message.Properties["coil_8"] != true || message.Properties["coil_9"] != false {
		t.Fatalf("unexpected coil properties: %#v", message.Properties)
	}
	if message.Parser != "" || message.MessageType != model.PropertyReport {
		t.Fatalf("unexpected standard message: %#v", message)
	}
}

func TestModbusCoilParserRejectsMissingMapping(t *testing.T) {
	if err := ValidateModbusCoilConfig(map[string]any{}); err == nil {
		t.Fatal("expected fields validation error")
	}
}

func TestModbusCoilParserRTUResponse(t *testing.T) {
	message, err := (ModbusCoilParser{}).ParseWithConfig(model.RawMessage{
		MessageID: "raw_rtu", Protocol: "modbus", PayloadFormat: "hex", Payload: json.RawMessage(`"01 01 01 01"`),
	}, map[string]any{"frame": "rtu", "startAddress": 0, "fields": []any{map[string]any{"name": "coil_0", "coilAddress": 0}}})
	if err != nil || message.Properties["coil_0"] != true {
		t.Fatalf("unexpected RTU response message=%#v err=%v", message, err)
	}
}
