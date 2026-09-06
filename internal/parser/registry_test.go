package parser

import (
	"encoding/hex"
	"encoding/json"
	"iot-platform/internal/model"
	"testing"
	"time"
)

func TestPlatformRegistryRequiresExplicitLegacyBinding(t *testing.T) {
	r := NewPlatformRegistry(t.TempDir())
	raw := model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: json.RawMessage(`"4040"`)}
	if _, err := r.Parse(raw); err == nil {
		t.Fatal("automatically selected a specialized parser")
	}
	frame := BuildGB26875RegistrationFrame(1, [6]byte{1, 2, 3, 4, 5, 6}, time.Now())
	// Existing bindings and historical replay still resolve the old parser.
	raw.Payload, _ = json.Marshal(hex.EncodeToString(frame))
	if _, err := r.ParseWith((GB26875Parser{}).Name(), raw); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{(GB26875Parser{}).Name(), ModbusTCPParserName, ModbusCoilParserName, JavaScriptParserName} {
		if ManagedParserType(name) {
			t.Fatalf("legacy parser offered for new packages: %s", name)
		}
	}
}
