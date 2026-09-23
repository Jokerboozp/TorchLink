// 本地预检 Go 样例，上传时仍执行平台完整校验。无需修改此文件，直接运行 go test ./...。
package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"os/exec"       /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"reflect"       /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type sampleOutput struct { /* 定义 sampleOutput 类型。 */
	bytes.Buffer      /* 执行当前语句并推进处理流程。 */
	overflow     bool /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (b *sampleOutput) Write(p []byte) (int, error) { /* 定义 Write 函数。 */
	n := len(p)                      /* 更新 n 的值。 */
	remaining := (1 << 20) - b.Len() /* 更新 remaining 的值。 */
	if len(p) > remaining {          /* 判断条件并选择处理分支。 */
		b.overflow = true /* 更新 b.overflow 的值。 */
		p = p[:remaining] /* 更新 p 的值。 */
	} /* 结束当前表达式或代码块。 */
	_, err := b.Buffer.Write(p) /* 更新 err 的值。 */
	return n, err               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func sampleSubset(actual, expected any) bool { /* 定义 sampleSubset 函数。 */
	if fields, ok := expected.(map[string]any); ok { /* 判断条件并选择处理分支。 */
		values, ok := actual.(map[string]any) /* 更新 ok 的值。 */
		if !ok {                              /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for key, value := range fields { /* 循环处理当前数据。 */
			if !sampleSubset(values[key], value) { /* 判断条件并选择处理分支。 */
				return false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return reflect.DeepEqual(actual, expected) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func TestProtocolSamples(t *testing.T) { /* 定义 TestProtocolSamples 函数。 */
	name := "worker"               /* 更新 name 的值。 */
	if runtime.GOOS == "windows" { /* 判断条件并选择处理分支。 */
		name += ".exe" /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	worker := filepath.Join(t.TempDir(), name)                              /* 更新 worker 的值。 */
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) /* 更新 cancel 的值。 */
	defer cancel()                                                          /* 安排函数结束时执行清理。 */
	build := exec.CommandContext(ctx, "go", "build", "-o", worker, ".")     /* 更新 build 的值。 */
	var buildLog sampleOutput                                               /* 声明 buildLog。 */
	build.Stdout = &buildLog                                                /* 更新 build.Stdout 的值。 */
	build.Stderr = &buildLog                                                /* 更新 build.Stderr 的值。 */
	if err := build.Run(); err != nil {                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("编译失败：%v\n%s", err, buildLog.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	call := func(t *testing.T, request any) map[string]any { /* 更新 call 的值。 */
		t.Helper()                          /* 执行当前语句并推进处理流程。 */
		input, err := json.Marshal(request) /* 更新 err 的值。 */
		if err != nil {                     /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) /* 更新 cancel 的值。 */
		defer cancel()                                                          /* 安排函数结束时执行清理。 */
		command := exec.CommandContext(ctx, worker)                             /* 更新 command 的值。 */
		command.Stdin = bytes.NewReader(input)                                  /* 更新 command.Stdin 的值。 */
		command.WaitDelay = time.Second                                         /* 更新 command.WaitDelay 的值。 */
		var output, logs sampleOutput                                           /* 声明 output。 */
		command.Stdout = &output                                                /* 更新 command.Stdout 的值。 */
		command.Stderr = &logs                                                  /* 更新 command.Stderr 的值。 */
		if err := command.Run(); err != nil {                                   /* 判断条件并选择处理分支。 */
			t.Fatalf("协议执行失败：%v\n%s", err, logs.String()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if output.overflow { /* 判断条件并选择处理分支。 */
			t.Fatal("协议输出超过 1 MiB") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		var result map[string]any                                       /* 声明 result。 */
		if err := json.Unmarshal(output.Bytes(), &result); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("协议输出无效：%v", err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if failure, ok := result["error"]; ok { /* 判断条件并选择处理分支。 */
			t.Fatalf("协议返回错误：%v", failure) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return result /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	description := call(t, map[string]any{"operation": "describe"}) /* 更新 description 的值。 */
	cases, _ := description["cases"].([]any)                        /* 更新 _ 的值。 */
	if len(cases) < 1 || len(cases) > 100 {                         /* 判断条件并选择处理分支。 */
		t.Fatal("请在 Protocol 中提供 1 至 100 条 Samples") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for i, item := range cases { /* 循环处理当前数据。 */
		c := item.(map[string]any)                                          /* 更新 c 的值。 */
		t.Run(fmt.Sprintf("解析/%d-%v", i+1, c["name"]), func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			raw := c["input"].(map[string]any) /* 更新 raw 的值。 */
			if raw["deviceId"] == "" {         /* 判断条件并选择处理分支。 */
				raw["deviceId"] = "package-test" /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			request := map[string]any{"version": 2, "operation": "decode", "raw": raw, "now": raw["receivedAt"]} /* 更新 request 的值。 */
			if metadata, ok := raw["metadata"].(map[string]any); ok {                                            /* 判断条件并选择处理分支。 */
				request["state"] = metadata["protocolState"] /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			result := call(t, request)                                /* 更新 result 的值。 */
			message, ok := result["standardMessage"].(map[string]any) /* 更新 ok 的值。 */
			if !ok {                                                  /* 判断条件并选择处理分支。 */
				t.Fatal("缺少标准消息") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			valid := false                                                                                                                    /* 更新 valid 的值。 */
			for _, kind := range []string{"PROPERTY_REPORT", "EVENT_REPORT", "STATE_CHANGE", "ALARM_REPORT", "COMMAND_REPLY", "LOG_REPORT"} { /* 循环处理当前数据。 */
				if message["messageType"] == kind { /* 判断条件并选择处理分支。 */
					valid = true /* 更新 valid 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if !valid || message["messageType"] != c["expectedMessageType"] { /* 判断条件并选择处理分支。 */
				t.Fatal("消息类型与预期不符") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			for expected, field := range map[string]string{"expectedProperties": "properties", "expectedEvent": "event", "expectedTags": "tags", "expectedTimestamp": "timestamp"} { /* 循环处理当前数据。 */
				want := c[expected] /* 更新 want 的值。 */
				if want == nil {    /* 判断条件并选择处理分支。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				if fields, ok := want.(map[string]any); ok { /* 判断条件并选择处理分支。 */
					values, _ := message[field].(map[string]any) /* 更新 _ 的值。 */
					for key, expected := range fields {          /* 循环处理当前数据。 */
						actual, exists := values[key]                        /* 更新 exists 的值。 */
						if !exists || !reflect.DeepEqual(actual, expected) { /* 判断条件并选择处理分支。 */
							t.Errorf("%s.%s 与预期不符：实际 %v，预期 %v", field, key, actual, expected) /* 验证实际结果符合预期。 */
						} /* 结束当前表达式或代码块。 */
					} /* 结束当前表达式或代码块。 */
				} else if !reflect.DeepEqual(message[field], want) { /* 结束当前表达式或代码块。 */
					t.Errorf("%s 与预期不符：实际 %v，预期 %v", field, message[field], want) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	metadata := description["metadata"].(map[string]any)          /* 更新 metadata 的值。 */
	required := map[string]bool{}                                 /* 更新 required 的值。 */
	for _, capability := range metadata["capabilities"].([]any) { /* 循环处理当前数据。 */
		if capability != "decode" { /* 判断条件并选择处理分支。 */
			required[capability.(string)] = true /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	operations, _ := description["operations"].([]any) /* 更新 _ 的值。 */
	if len(operations) > 100 {                         /* 判断条件并选择处理分支。 */
		t.Fatal("Operations 不能超过 100 条") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for i, item := range operations { /* 循环处理当前数据。 */
		c := item.(map[string]any)                                          /* 更新 c 的值。 */
		t.Run(fmt.Sprintf("接入/%d-%v", i+1, c["name"]), func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			request := c["request"].(map[string]any)    /* 更新 request 的值。 */
			result := call(t, request)                  /* 更新 result 的值。 */
			reply, _ := result["reply"].(string)        /* 更新 _ 的值。 */
			replyBytes, err := hex.DecodeString(reply)  /* 更新 err 的值。 */
			if err != nil || len(replyBytes) > 64<<10 { /* 判断条件并选择处理分支。 */
				t.Fatal("应答无效或过大") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			switch request["operation"] { /* 根据条件选择处理路径。 */
			case "ingress": /* 处理当前分支。 */
				consumed, _ := result["consumed"].(float64) /* 更新 _ 的值。 */
				device, _ := result["deviceId"].(string)    /* 更新 _ 的值。 */
				if result["needMore"] == true {             /* 判断条件并选择处理分支。 */
					if consumed != 0 || device != "" || reply != "" { /* 判断条件并选择处理分支。 */
						t.Fatal("半帧不能消耗数据、识别设备或应答") /* 验证实际结果符合预期。 */
					} /* 结束当前表达式或代码块。 */
				} else { /* 结束当前表达式或代码块。 */
					data, err := hex.DecodeString(request["data"].(string))                           /* 更新 err 的值。 */
					if err != nil || consumed <= 0 || consumed > float64(len(data)) || device == "" { /* 判断条件并选择处理分支。 */
						t.Fatal("完整帧必须返回有效长度和设备标识") /* 验证实际结果符合预期。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			case "encode": /* 处理当前分支。 */
				if reply == "" { /* 判断条件并选择处理分支。 */
					t.Fatal("下行未返回报文") /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			default: /* 处理当前分支。 */
				t.Fatal("未知操作") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if !sampleSubset(result, c["expected"]) { /* 判断条件并选择处理分支。 */
				t.Fatalf("结果与预期不符：实际 %v，预期 %v", result, c["expected"]) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if request["operation"] != "ingress" || result["needMore"] != true { /* 判断条件并选择处理分支。 */
				delete(required, request["operation"].(string)) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(required) > 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("Operations 缺少完整成功样例：%v", required) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
