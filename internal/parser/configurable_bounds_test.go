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
