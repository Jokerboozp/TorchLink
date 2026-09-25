package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                 /* 执行当前语句并推进处理流程。 */
	"encoding/json"         /* 执行当前语句并推进处理流程。 */
	"gb26875-dahua/gb26875" /* 执行当前语句并推进处理流程。 */
	"os"                    /* 执行当前语句并推进处理流程。 */
	"strconv"
	"strings" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestSamplesAndWireOperations(t *testing.T) { /* 定义 TestSamplesAndWireOperations 函数。 */
	data, err := os.ReadFile("samples/cases.json") /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var cases []struct { /* 声明 cases。 */
		Name                string              /* 执行当前语句并推进处理流程。 */
		Input               gb26875.RawMessage  /* 执行当前语句并推进处理流程。 */
		ExpectedMessageType gb26875.MessageType /* 执行当前语句并推进处理流程。 */
		ExpectedProperties  map[string]any      /* 执行当前语句并推进处理流程。 */
		ExpectedEventType   string              /* 执行当前语句并推进处理流程。 */
		ExpectedTimestamp   int64               /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(data, &cases); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, c := range cases { /* 循环处理当前数据。 */
		t.Run(c.Name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			msg, err := gb26875.Decode(c.Input) /* 更新 err 的值。 */
			if err != nil {                     /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if msg.MessageType != c.ExpectedMessageType { /* 判断条件并选择处理分支。 */
				t.Fatalf("type %s", msg.MessageType) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			encoded, _ := json.Marshal(msg.Properties) /* 更新 _ 的值。 */
			var props map[string]any                   /* 声明 props。 */
			_ = json.Unmarshal(encoded, &props)        /* 更新 _ 的值。 */
			for k, v := range c.ExpectedProperties {   /* 循环处理当前数据。 */
				if props[k] != v { /* 判断条件并选择处理分支。 */
					t.Fatalf("%s=%v want %v", k, props[k], v) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if c.ExpectedEventType != "" && msg.Event["type"] != c.ExpectedEventType { /* 判断条件并选择处理分支。 */
				t.Fatalf("event %v", msg.Event) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if c.ExpectedTimestamp != 0 && msg.Timestamp != c.ExpectedTimestamp { /* 判断条件并选择处理分支。 */
				t.Fatalf("timestamp %d", msg.Timestamp) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	raw := cases[0].Input                   /* 更新 raw 的值。 */
	var frame string                        /* 声明 frame。 */
	_ = json.Unmarshal(raw.Payload, &frame) /* 更新 _ 的值。 */
	for n := 2; n < len(frame); n += 2 {    /* 循环处理当前数据。 */
		r := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: frame[:n]}) /* 更新 r 的值。 */
		if !r.NeedMore || r.Consumed != 0 || r.Reply != "" {                                    /* 判断条件并选择处理分支。 */
			t.Fatalf("fragment %d: %+v", n, r) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, bad := range []string{"00" + frame, frame[:len(frame)-6] + "002323", "404001000103000000000000000000000000000000000000FFFF02"} { /* 循环处理当前数据。 */
		if r := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: bad}); r.Error == "" { /* 判断条件并选择处理分支。 */
			t.Fatalf("accepted corrupt frame: %+v", r) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	r := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: frame + frame, Now: raw.ReceivedAt}) /* 更新 r 的值。 */
	if r.Error != "" || r.Consumed != len(frame)/2 || r.Reply == "" {                                                /* 判断条件并选择处理分支。 */
		t.Fatalf("coalesced frame: %+v", r) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var output bytes.Buffer                                                                                   /* 声明 output。 */
	state, _ := json.Marshal(gb26875.SessionState{Source: "123456789012", Sequence: 12})                      /* 更新 _ 的值。 */
	preserved := gb26875.Handle(gb26875.Request{Version: 2, Operation: "ingress", Data: frame, State: state}) /* 更新 preserved 的值。 */
	if preserved.State == nil || preserved.State.Sequence != 12 {                                             /* 判断条件并选择处理分支。 */
		t.Fatal("incoming report rewound command sequence") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	encoded, _ := json.Marshal(raw)                                                                                        /* 更新 _ 的值。 */
	if err = run(bytes.NewReader(encoded), &output); err != nil || !strings.Contains(output.String(), "standardMessage") { /* 判断条件并选择处理分支。 */
		t.Fatalf("v1 compatibility: %v %s", err, output.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestMultipleComponentsPreserveSeparateAlarmLocations(t *testing.T) { /* 定义 TestMultipleComponentsPreserveSeparateAlarmLocations 函数。 */
	b, err := os.ReadFile("samples/cases.json") /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var cases []struct { /* 声明 cases。 */
		Name  string             /* 执行当前语句并推进处理流程。 */
		Input gb26875.RawMessage /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(b, &cases); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, c := range cases { /* 循环处理当前数据。 */
		if c.Name != "multiple-components" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		msg, err := gb26875.Decode(c.Input) /* 更新 err 的值。 */
		if err != nil {                     /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if _, exists := msg.Properties["nodeAddress"]; exists { /* 判断条件并选择处理分支。 */
			t.Fatal("aggregate carries first object's address") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		components := msg.Event["components"].([]map[string]any)                /* 更新 components 的值。 */
		if len(components) != 2 || components[0]["id"] == components[1]["id"] { /* 判断条件并选择处理分支。 */
			t.Fatal(components) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if components[0]["alarms"].(map[string]bool)["FIRE"] || !components[1]["alarms"].(map[string]bool)["FIRE"] || components[1]["location"] != "alarm second" { /* 判断条件并选择处理分支。 */
			t.Fatal(components) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	t.Fatal("fixture missing") /* 验证实际结果符合预期。 */
} /* 结束当前表达式或代码块。 */

func TestServeAnswersEachRequestWithItsID(t *testing.T) {
	input := strings.NewReader(`{"requestId":"1","version":2,"operation":"encode","command":{"type":"unknown"}}` + "\n" + `{"requestId":"2","version":2,"operation":"decode"}` + "\n")
	var output bytes.Buffer
	if err := serve(input, &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("serve must answer every request on its own line: %q", output.String())
	}
	for index, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil || response["requestId"] != strconv.Itoa(index+1) {
			t.Fatalf("response %d lost its requestId: %q err=%v", index+1, line, err)
		}
	}
}
