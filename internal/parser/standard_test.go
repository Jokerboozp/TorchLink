package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"strings"                     /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestStandardVersionOneAndLegacy(t *testing.T) { /* 定义 TestStandardVersionOneAndLegacy 函数。 */
	for _, tc := range []struct { /* 循环处理当前数据。 */
		kind, payload string /* 执行当前语句并推进处理流程。 */
		valid         bool   /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{"property", `{"id":"1","version":"1.0","timestamp":1000,"data":{"temperature":26.5}}`, true},                                   /* 执行当前语句并推进处理流程。 */
		{"event", `{"id":"1","version":"1.0","timestamp":1000,"event":"fire_alarm","data":{"zone":3}}`, true},                           /* 执行当前语句并推进处理流程。 */
		{"state", `{"id":"1","version":"1.0","timestamp":1000,"online":true}`, true},                                                    /* 执行当前语句并推进处理流程。 */
		{"command-reply", `{"id":"1","version":"1.0","timestamp":1000,"commandId":"c","success":false,"data":{}}`, true},                /* 执行当前语句并推进处理流程。 */
		{"state", `{"id":"1","timestamp":1000,"data":{"connectionStatus":"CONNECTED"}}`, true},                                          /* 执行当前语句并推进处理流程。 */
		{"property", `{"id":"1","version":"2.0","timestamp":1000,"data":{"x":1}}`, false},                                               /* 执行当前语句并推进处理流程。 */
		{"event", `{"id":"1","version":"1.0","timestamp":1000,"data":{"zone":3}}`, false},                                               /* 执行当前语句并推进处理流程。 */
		{"property", `{"id":"1","id":"2","timestamp":1000,"data":{"x":1}}`, false},                                                      /* 执行当前语句并推进处理流程。 */
		{"state", `{"id":"1","timestamp":1000,"online":true,"data":{"connectionStatus":"DISCONNECTED"}}`, false},                        /* 执行当前语句并推进处理流程。 */
		{"command-reply", `{"id":"1","timestamp":1000,"commandId":"c","success":"true"}`, false},                                        /* 执行当前语句并推进处理流程。 */
		{"property", `{"id":"1","timestamp":999999999999999,"data":{"x":1}}`, false},                                                    /* 执行当前语句并推进处理流程。 */
		{"property", `{"id":"1","timestamp":1000,"data":{"x":` + strings.Repeat("[", 17) + "1" + strings.Repeat("]", 17) + `}}`, false}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		t.Run(tc.kind+tc.payload, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			raw := model.RawMessage{Payload: json.RawMessage(tc.payload), ReceivedAt: 2000, Headers: map[string]string{"messageKind": tc.kind}, TenantID: "trusted", DeviceID: "device"} /* 更新 raw 的值。 */
			m, err := (StandardParser{}).Parse(raw)                                                                                                                                      /* 更新 err 的值。 */
			if (err == nil) != tc.valid {                                                                                                                                                /* 判断条件并选择处理分支。 */
				t.Fatalf("valid=%v error=%v", tc.valid, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if !tc.valid && m != nil { /* 判断条件并选择处理分支。 */
				t.Fatal("invalid input produced business message") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if tc.valid && (m.TenantID != "trusted" || m.DeviceID != "device" || m.MessageType == model.AlarmReport) { /* 判断条件并选择处理分支。 */
				t.Fatal("identity or alarm semantics changed") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
