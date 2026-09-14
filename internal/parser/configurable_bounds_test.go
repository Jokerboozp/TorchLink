package parser

import (
	"encoding/json"
	"iot-platform/internal/model"
	"testing"
)

func TestGeneratedHexRejectsInvalidBounds(t *testing.T) {
	raw := model.RawMessage{Payload: json.RawMessage(`"01 02 03"`)}
	for _, config := range []map[string]any{
		{"checksum": "sum8", "checksumStartOffset": -1},
		{"checksum": "sum8", "checksumStartOffset": 9},
		{"fields": []any{map[string]any{"name": "overflow", "offset": int(^uint(0) >> 1), "length": 2, "type": "uint16"}}},
	} {
		if _, err := (ConfigurableHexParser{}).ParseWithConfig(raw, config); err == nil {
			t.Fatal("invalid bounds accepted")
		}
	}
}

func TestGeneratedHexRejectsUnknownChecksum(t *testing.T) {
	// The pressure-history document requires CRC16. A mapping must not
	// silently skip that integrity requirement when the parser lacks it.
	raw := model.RawMessage{Payload: json.RawMessage(`"0146001900050a66da8098000000000023f60b"`)}
	for _, checksum := range []string{"crc16", "crc16-modbus", "sum16", "typo"} {
		t.Run(checksum, func(t *testing.T) {
			config := map[string]any{"checksum": checksum, "fields": []any{map[string]any{"name": "pressure", "offset": 13, "length": 4, "type": "int32", "endian": "big", "scale": 0.01}}}
			if _, err := (ConfigurableHexParser{}).ParseWithConfig(raw, config); err == nil {
				t.Fatal("unsupported checksum was silently ignored")
			}
		})
	}
}
