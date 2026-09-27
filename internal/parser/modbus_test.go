package parser

import (
	"encoding/hex"
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

// Synthetic read responses use the addresses in the supplied BPW, 2XP,
// dual-power and inspection-cabinet documents. No control writes are sent.
func TestReferenceCabinetModbusReads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address int
		fields  []string
		values  []byte
	}{
		{"BPW", 0x1100, []string{"pump1State", "pump2State"}, []byte{0, 1, 0, 2}},
		{"2XP", 0x2000, []string{"pump1State", "pump2State"}, []byte{0, 1, 0, 2}},
		{"dual-power", 0x3000, []string{"mainVoltage", "mainCurrent", "backupVoltage", "backupCurrent"}, []byte{0, 220, 0, 10, 0, 219, 0, 0}},
		{"inspection", 0x2000, []string{"pump1State", "pump2State", "pump3State", "pump4State", "pump5State", "pump6State", "pump7State", "pump8State"}, []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			points := []model.ModbusPoint{}
			for i, field := range tc.fields {
				points = append(points, model.ModbusPoint{Identifier: field, FunctionCode: 3, Address: tc.address + i, DataType: "uint16", RegisterCount: 1, Scale: 1})
			}
			frame := referenceCRC(append([]byte{1, 3, byte(len(tc.values))}, tc.values...))
			payload, _ := json.Marshal(hex.EncodeToString(frame))
			raw := model.RawMessage{MessageID: "reference", TenantID: "test", ProductID: "cabinet", DeviceID: tc.name, ReceivedAt: 1, Payload: payload, Metadata: map[string]any{"startAddress": tc.address}}
			message, err := (ModbusRTUParser{}).ParseWithConfig(raw, map[string]any{"points": points})
			if err != nil {
				t.Fatal(err)
			}
			for i, field := range tc.fields {
				if message.Properties[field] != float64(int(tc.values[i*2])<<8|int(tc.values[i*2+1])) {
					t.Fatalf("%s decoded as %v", field, message.Properties[field])
				}
			}
			frame[len(frame)-1] ^= 1
			raw.Payload, _ = json.Marshal(hex.EncodeToString(frame))
			if _, err := (ModbusRTUParser{}).ParseWithConfig(raw, map[string]any{"points": points}); err == nil {
				t.Fatal("corrupted CRC was accepted")
			}
		})
	}
}

func referenceCRC(data []byte) []byte {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return append(data, byte(crc), byte(crc>>8))
}

func TestReferenceCabinetInputBits(t *testing.T) {
	bit := 3
	payload, _ := json.Marshal(hex.EncodeToString(referenceCRC([]byte{1, 3, 2, 0, 8})))
	raw := model.RawMessage{Payload: payload, Metadata: map[string]any{"startAddress": 0x2003}}
	for _, dataType := range []string{"uint16", "bits"} {
		t.Run(dataType, func(t *testing.T) {
			m, err := (ModbusRTUParser{}).ParseWithConfig(raw, map[string]any{"points": []model.ModbusPoint{{Identifier: "input4", FunctionCode: 3, Address: 0x2003, DataType: dataType, RegisterCount: 1, Bit: &bit}}})
			if err != nil {
				t.Fatal(err)
			}
			if m.Properties["input4"] != true {
				t.Fatalf("input 4 must be true, got %v", m.Properties["input4"])
			}
		})
	}
}
