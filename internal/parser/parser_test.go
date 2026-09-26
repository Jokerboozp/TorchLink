package parser

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func TestJSONParser(t *testing.T) {
	r := NewRegistry(JSONParser{})
	m, err := r.Parse(model.RawMessage{MessageID: "raw_1", TenantID: "t", ProductID: "json_sensor", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000, Payload: json.RawMessage(`{"properties":{"temperature":82.5,"smoke":true},"tags":{"buildingId":"A"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if m.Properties["temperature"] != 82.5 || m.Tags["buildingId"] != "A" {
		t.Fatalf("unexpected message: %#v", m)
	}
}

func TestGB26875ManualAlarmFrame(t *testing.T) {
	at := time.Date(2026, 8, 24, 14, 30, 15, 0, time.Local)
	frame := BuildGB26875ComponentStatusFrame(1, [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}, 128, 1, 23, 2, 7, 1<<1, "manual call point", at)
	payload, _ := json.Marshal(hex.EncodeToString(frame))
	r := NewRegistry(GB26875Parser{})
	m, err := r.Parse(model.RawMessage{MessageID: "raw_gb_1", TenantID: "t", ProductID: "dahua_lora_fire", DeviceID: "123456789012", Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", ReceivedAt: at.UnixMilli(), Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageType != model.AlarmReport || m.Properties["fireAlarm"] != true || m.Properties["componentTypeName"] != "手动火灾报警按钮" {
		t.Fatalf("unexpected message: %#v", m)
	}
	if m.Parser != "gb26875_dahua_parser" || m.ParserVersion != "1.0.0" {
		t.Fatalf("unexpected parser identity: %s@%s", m.Parser, m.ParserVersion)
	}
}

func TestGB26875RejectsBadChecksum(t *testing.T) {
	frame := BuildGB26875ComponentStatusFrame(2, [6]byte{1, 2, 3, 4, 5, 6}, 128, 1, 137, 1, 1, 1<<5, "sound light", time.Now())
	frame[len(frame)-3]++
	payload, _ := json.Marshal(hex.EncodeToString(frame))
	_, err := (GB26875Parser{}).Parse(model.RawMessage{Payload: payload, ReceivedAt: time.Now().UnixMilli()})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum error, got %v", err)
	}
}

func TestGB26875RegistrationFrame(t *testing.T) {
	frame := BuildGB26875RegistrationFrame(1, [6]byte{1, 2, 3, 4, 5, 6}, time.Now())
	if len(frame) != 142 {
		t.Fatalf("v1.03 registration frame should be 142 bytes, got %d", len(frame))
	}
	payload, _ := json.Marshal(hex.EncodeToString(frame))
	message, err := (GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if message.MessageType != model.StateChange || message.Event["type"] != "REGISTER" || message.Properties["registered"] != true {
		t.Fatalf("unexpected registration: %#v", message)
	}
}

func TestGB26875TimeSyncRequestAndResponse(t *testing.T) {
	at := time.Date(2026, 8, 25, 10, 11, 12, 0, time.Local)
	source := [6]byte{1, 2, 3, 4, 5, 6}
	request := BuildGB26875TimeSyncRequestFrame(7, source, at)
	requestPayload, _ := json.Marshal(hex.EncodeToString(request))
	requestMessage, err := (GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: requestPayload, ReceivedAt: at.UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if requestMessage.MessageType != model.EventReport || requestMessage.Event["type"] != "TIME_SYNC_REQUEST" {
		t.Fatalf("unexpected time sync request: %#v", requestMessage)
	}
	response := BuildGB26875TimeSyncFrame(7, source, at)
	responsePayload, _ := json.Marshal(hex.EncodeToString(response))
	responseMessage, err := (GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: responsePayload, ReceivedAt: at.UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if responseMessage.MessageType != model.CommandReply || responseMessage.Event["type"] != "TIME_SYNC" {
		t.Fatalf("unexpected time sync response: %#v", responseMessage)
	}
}

func TestConfigurableJSONParser(t *testing.T) {
	r := NewRegistry(ConfigurableJSONParser{})
	m, err := r.ParseWithConfig("configurable_json_parser", map[string]any{
		"properties": map[string]any{
			"temperature": map[string]any{"path": "$.data.temp", "type": "number", "scale": 0.1},
			"smoke":       "$.data.smoke",
		},
		"tags":          map[string]any{"deviceType": "$.kind"},
		"messageType":   "ALARM_REPORT",
		"timestampPath": "$.occurredAt", "timestampUnit": "s",
	}, model.RawMessage{MessageID: "raw_config_json", TenantID: "t", ProductID: "p", DeviceID: "d", PayloadFormat: "json", Payload: json.RawMessage(`{"data":{"temp":805,"smoke":true},"kind":"smoke","occurredAt":100}`)})
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageType != model.AlarmReport || m.Properties["temperature"] != 80.5 || m.Properties["smoke"] != true || m.Tags["deviceType"] != "smoke" || m.Timestamp != 100000 {
		t.Fatalf("unexpected configurable JSON result: %#v", m)
	}
}

func TestConfigurableHexParser(t *testing.T) {
	r := NewRegistry(ConfigurableHexParser{})
	m, err := r.ParseWithConfig("configurable_hex_parser", map[string]any{
		"startHex": "AA", "endHex": "55", "checksum": "sum8", "checksumStartOffset": 1,
		"fields": []any{map[string]any{"name": "temperature", "offset": 1, "length": 2, "type": "int16", "endian": "little", "scale": 0.1}},
	}, model.RawMessage{MessageID: "raw_config_hex", TenantID: "t", ProductID: "p", DeviceID: "d", PayloadFormat: "hex", Payload: json.RawMessage(`"AA 20 03 00 23 55"`)})
	if err != nil {
		t.Fatal(err)
	}
	if m.Properties["temperature"] != 80.0 {
		t.Fatalf("unexpected configurable hex result: %#v", m.Properties)
	}
}

func TestJavaScriptParser(t *testing.T) {
	source := `function parse(raw) {
  const bytes = hexToBytes(raw.payload)
  return {
    messageType: bytes[0] === 1 ? 'ALARM_REPORT' : 'PROPERTY_REPORT',
    properties: { smoke: bytes[0] === 1, temperature: bytes[1] / 10 },
    tags: { source: 'javascript' },
    timestamp: 1234
  }
}`
	r := NewRegistry(JavaScriptParser{})
	m, err := r.ParseWithConfig(JavaScriptParserName, map[string]any{"source": source}, model.RawMessage{
		MessageID: "raw_js", TenantID: "t", ProductID: "p", DeviceID: "d", Protocol: "javascript", PayloadFormat: "hex", ReceivedAt: 999,
		Payload: json.RawMessage(`"01 2A"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageType != model.AlarmReport || m.Properties["temperature"] != 4.2 || m.Tags["source"] != "javascript" || m.Timestamp != 1234 {
		t.Fatalf("unexpected javascript result: %#v", m)
	}
	if m.Parser != JavaScriptParserName || m.ParserVersion != JavaScriptParserVersion {
		t.Fatalf("unexpected parser identity: %s@%s", m.Parser, m.ParserVersion)
	}
}

func TestJavaScriptParserTimeout(t *testing.T) {
	_, err := (JavaScriptParser{}).ParseWithConfig(model.RawMessage{Payload: json.RawMessage(`{}`)}, map[string]any{
		"source": "function parse(raw) { while (true) {} }",
	})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected javascript timeout, got %v", err)
	}
}

func TestSmokeHexParser(t *testing.T) {
	r := NewRegistry(FireSmokeHexParser{})
	m, err := r.Parse(model.RawMessage{MessageID: "raw_2", TenantID: "t", ProductID: "fire_smoke", DeviceID: "d", PayloadFormat: "hex", Payload: json.RawMessage(`"0102D050"`)})
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageType != model.AlarmReport || m.Properties["temperature"].(float64) != 72 {
		t.Fatalf("unexpected message: %#v", m)
	}
}
