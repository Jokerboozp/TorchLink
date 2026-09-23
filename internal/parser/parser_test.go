package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestJSONParser(t *testing.T) { /* 定义 TestJSONParser 函数。 */
	r := NewRegistry(JSONParser{})                                                                                                                                                                                                                                                     /* 更新 r 的值。 */
	m, err := r.Parse(model.RawMessage{MessageID: "raw_1", TenantID: "t", ProductID: "json_sensor", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000, Payload: json.RawMessage(`{"properties":{"temperature":82.5,"smoke":true},"tags":{"buildingId":"A"}}`)}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.Properties["temperature"] != 82.5 || m.Tags["buildingId"] != "A" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected message: %#v", m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGB26875ManualAlarmFrame(t *testing.T) { /* 定义 TestGB26875ManualAlarmFrame 函数。 */
	at := time.Date(2026, 8, 24, 14, 30, 15, 0, time.Local)                                                                                                                                                                                /* 更新 at 的值。 */
	frame := BuildGB26875ComponentStatusFrame(1, [6]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}, 128, 1, 23, 2, 7, 1<<1, "manual call point", at)                                                                                             /* 更新 frame 的值。 */
	payload, _ := json.Marshal(hex.EncodeToString(frame))                                                                                                                                                                                  /* 更新 _ 的值。 */
	r := NewRegistry(GB26875Parser{})                                                                                                                                                                                                      /* 更新 r 的值。 */
	m, err := r.Parse(model.RawMessage{MessageID: "raw_gb_1", TenantID: "t", ProductID: "dahua_lora_fire", DeviceID: "123456789012", Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", ReceivedAt: at.UnixMilli(), Payload: payload}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.MessageType != model.AlarmReport || m.Properties["fireAlarm"] != true || m.Properties["componentTypeName"] != "手动火灾报警按钮" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected message: %#v", m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.Parser != "gb26875_dahua_parser" || m.ParserVersion != "1.0.0" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected parser identity: %s@%s", m.Parser, m.ParserVersion) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGB26875RejectsBadChecksum(t *testing.T) { /* 定义 TestGB26875RejectsBadChecksum 函数。 */
	frame := BuildGB26875ComponentStatusFrame(2, [6]byte{1, 2, 3, 4, 5, 6}, 128, 1, 137, 1, 1, 1<<5, "sound light", time.Now()) /* 更新 frame 的值。 */
	frame[len(frame)-3]++                                                                                                       /* 执行当前语句并推进处理流程。 */
	payload, _ := json.Marshal(hex.EncodeToString(frame))                                                                       /* 更新 _ 的值。 */
	_, err := (GB26875Parser{}).Parse(model.RawMessage{Payload: payload, ReceivedAt: time.Now().UnixMilli()})                   /* 更新 err 的值。 */
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {                                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("expected checksum error, got %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGB26875RegistrationFrame(t *testing.T) { /* 定义 TestGB26875RegistrationFrame 函数。 */
	frame := BuildGB26875RegistrationFrame(1, [6]byte{1, 2, 3, 4, 5, 6}, time.Now()) /* 更新 frame 的值。 */
	if len(frame) != 142 {                                                           /* 判断条件并选择处理分支。 */
		t.Fatalf("v1.03 registration frame should be 142 bytes, got %d", len(frame)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	payload, _ := json.Marshal(hex.EncodeToString(frame))                                                                              /* 更新 _ 的值。 */
	message, err := (GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: payload}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.MessageType != model.StateChange || message.Event["type"] != "REGISTER" || message.Properties["registered"] != true { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected registration: %#v", message) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGB26875TimeSyncRequestAndResponse(t *testing.T) { /* 定义 TestGB26875TimeSyncRequestAndResponse 函数。 */
	at := time.Date(2026, 8, 25, 10, 11, 12, 0, time.Local)                                                                                                                      /* 更新 at 的值。 */
	source := [6]byte{1, 2, 3, 4, 5, 6}                                                                                                                                          /* 更新 source 的值。 */
	request := BuildGB26875TimeSyncRequestFrame(7, source, at)                                                                                                                   /* 更新 request 的值。 */
	requestPayload, _ := json.Marshal(hex.EncodeToString(request))                                                                                                               /* 更新 _ 的值。 */
	requestMessage, err := (GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: requestPayload, ReceivedAt: at.UnixMilli()}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if requestMessage.MessageType != model.EventReport || requestMessage.Event["type"] != "TIME_SYNC_REQUEST" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected time sync request: %#v", requestMessage) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	response := BuildGB26875TimeSyncFrame(7, source, at)                                                                                                                           /* 更新 response 的值。 */
	responsePayload, _ := json.Marshal(hex.EncodeToString(response))                                                                                                               /* 更新 _ 的值。 */
	responseMessage, err := (GB26875Parser{}).Parse(model.RawMessage{Protocol: "gb26875-dahua-v1.03", PayloadFormat: "hex", Payload: responsePayload, ReceivedAt: at.UnixMilli()}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if responseMessage.MessageType != model.CommandReply || responseMessage.Event["type"] != "TIME_SYNC" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected time sync response: %#v", responseMessage) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestConfigurableJSONParser(t *testing.T) { /* 定义 TestConfigurableJSONParser 函数。 */
	r := NewRegistry(ConfigurableJSONParser{})                              /* 更新 r 的值。 */
	m, err := r.ParseWithConfig("configurable_json_parser", map[string]any{ /* 更新 err 的值。 */
		"properties": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"temperature": map[string]any{"path": "$.data.temp", "type": "number", "scale": 0.1}, /* 执行当前语句并推进处理流程。 */
			"smoke":       "$.data.smoke",                                                        /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		"tags":          map[string]any{"deviceType": "$.kind"}, /* 执行当前语句并推进处理流程。 */
		"messageType":   "ALARM_REPORT",                         /* 执行当前语句并推进处理流程。 */
		"timestampPath": "$.occurredAt", "timestampUnit": "s",   /* 执行当前语句并推进处理流程。 */
	}, model.RawMessage{MessageID: "raw_config_json", TenantID: "t", ProductID: "p", DeviceID: "d", PayloadFormat: "json", Payload: json.RawMessage(`{"data":{"temp":805,"smoke":true},"kind":"smoke","occurredAt":100}`)}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.MessageType != model.AlarmReport || m.Properties["temperature"] != 80.5 || m.Properties["smoke"] != true || m.Tags["deviceType"] != "smoke" || m.Timestamp != 100000 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected configurable JSON result: %#v", m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestConfigurableHexParser(t *testing.T) { /* 定义 TestConfigurableHexParser 函数。 */
	r := NewRegistry(ConfigurableHexParser{})                              /* 更新 r 的值。 */
	m, err := r.ParseWithConfig("configurable_hex_parser", map[string]any{ /* 更新 err 的值。 */
		"startHex": "AA", "endHex": "55", "checksum": "sum8", "checksumStartOffset": 1, /* 执行当前语句并推进处理流程。 */
		"fields": []any{map[string]any{"name": "temperature", "offset": 1, "length": 2, "type": "int16", "endian": "little", "scale": 0.1}}, /* 执行当前语句并推进处理流程。 */
	}, model.RawMessage{MessageID: "raw_config_hex", TenantID: "t", ProductID: "p", DeviceID: "d", PayloadFormat: "hex", Payload: json.RawMessage(`"AA 20 03 00 23 55"`)}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.Properties["temperature"] != 80.0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected configurable hex result: %#v", m.Properties) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestJavaScriptParser(t *testing.T) { /* 定义 TestJavaScriptParser 函数。 */
	source := `function parse(raw) {
  const bytes = hexToBytes(raw.payload)
  return {
    messageType: bytes[0] === 1 ? 'ALARM_REPORT' : 'PROPERTY_REPORT',
    properties: { smoke: bytes[0] === 1, temperature: bytes[1] / 10 },
    tags: { source: 'javascript' },
    timestamp: 1234
  }
}`
	r := NewRegistry(JavaScriptParser{})                                                                  /* 更新 r 的值。 */
	m, err := r.ParseWithConfig(JavaScriptParserName, map[string]any{"source": source}, model.RawMessage{ /* 更新 err 的值。 */
		MessageID: "raw_js", TenantID: "t", ProductID: "p", DeviceID: "d", Protocol: "javascript", PayloadFormat: "hex", ReceivedAt: 999, /* 执行当前语句并推进处理流程。 */
		Payload: json.RawMessage(`"01 2A"`), /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.MessageType != model.AlarmReport || m.Properties["temperature"] != 4.2 || m.Tags["source"] != "javascript" || m.Timestamp != 1234 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected javascript result: %#v", m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.Parser != JavaScriptParserName || m.ParserVersion != JavaScriptParserVersion { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected parser identity: %s@%s", m.Parser, m.ParserVersion) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestJavaScriptParserTimeout(t *testing.T) { /* 定义 TestJavaScriptParserTimeout 函数。 */
	_, err := (JavaScriptParser{}).ParseWithConfig(model.RawMessage{Payload: json.RawMessage(`{}`)}, map[string]any{ /* 更新 err 的值。 */
		"source": "function parse(raw) { while (true) {} }", /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err == nil || !strings.Contains(err.Error(), "timeout") { /* 判断条件并选择处理分支。 */
		t.Fatalf("expected javascript timeout, got %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestSmokeHexParser(t *testing.T) { /* 定义 TestSmokeHexParser 函数。 */
	r := NewRegistry(FireSmokeHexParser{})                                                                                                                                       /* 更新 r 的值。 */
	m, err := r.Parse(model.RawMessage{MessageID: "raw_2", TenantID: "t", ProductID: "fire_smoke", DeviceID: "d", PayloadFormat: "hex", Payload: json.RawMessage(`"0102D050"`)}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if m.MessageType != model.AlarmReport || m.Properties["temperature"].(float64) != 72 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected message: %#v", m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
