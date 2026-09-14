package parser

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"iot-platform/internal/model"
)

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
