// 本地预检 Go 样例，上传时仍执行平台完整校验。无需修改此文件，直接运行 go test ./...。
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

type sampleOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *sampleOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, err := b.Buffer.Write(p)
	return n, err
}
func sampleSubset(actual, expected any) bool {
	if fields, ok := expected.(map[string]any); ok {
		values, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range fields {
			if !sampleSubset(values[key], value) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(actual, expected)
}
func TestProtocolSamples(t *testing.T) {
	name := "worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	worker := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", worker, ".")
	var buildLog sampleOutput
	build.Stdout = &buildLog
	build.Stderr = &buildLog
	if err := build.Run(); err != nil {
		t.Fatalf("编译失败：%v\n%s", err, buildLog.String())
	}
	call := func(t *testing.T, request any) map[string]any {
		t.Helper()
		input, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, worker)
		command.Stdin = bytes.NewReader(input)
		command.WaitDelay = time.Second
		var output, logs sampleOutput
		command.Stdout = &output
		command.Stderr = &logs
		if err := command.Run(); err != nil {
			t.Fatalf("协议执行失败：%v\n%s", err, logs.String())
		}
		if output.overflow {
			t.Fatal("协议输出超过 1 MiB")
		}
		var result map[string]any
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatalf("协议输出无效：%v", err)
		}
		if failure, ok := result["error"]; ok {
			t.Fatalf("协议返回错误：%v", failure)
		}
		return result
	}
	description := call(t, map[string]any{"operation": "describe"})
	cases, _ := description["cases"].([]any)
	if len(cases) < 1 || len(cases) > 100 {
		t.Fatal("请在 Protocol 中提供 1 至 100 条 Samples")
	}
	for i, item := range cases {
		c := item.(map[string]any)
		t.Run(fmt.Sprintf("解析/%d-%v", i+1, c["name"]), func(t *testing.T) {
			raw := c["input"].(map[string]any)
			if raw["deviceId"] == "" {
				raw["deviceId"] = "package-test"
			}
			request := map[string]any{"version": 2, "operation": "decode", "raw": raw, "now": raw["receivedAt"]}
			if metadata, ok := raw["metadata"].(map[string]any); ok {
				request["state"] = metadata["protocolState"]
			}
			result := call(t, request)
			message, ok := result["standardMessage"].(map[string]any)
			if !ok {
				t.Fatal("缺少标准消息")
			}
			valid := false
			for _, kind := range []string{"PROPERTY_REPORT", "EVENT_REPORT", "STATE_CHANGE", "ALARM_REPORT", "COMMAND_REPLY", "LOG_REPORT"} {
				if message["messageType"] == kind {
					valid = true
				}
			}
			if !valid || message["messageType"] != c["expectedMessageType"] {
				t.Fatal("消息类型与预期不符")
			}
			for expected, field := range map[string]string{"expectedProperties": "properties", "expectedEvent": "event", "expectedTags": "tags", "expectedTimestamp": "timestamp"} {
				want := c[expected]
				if want == nil {
					continue
				}
				if fields, ok := want.(map[string]any); ok {
					values, _ := message[field].(map[string]any)
					for key, expected := range fields {
						actual, exists := values[key]
						if !exists || !reflect.DeepEqual(actual, expected) {
							t.Errorf("%s.%s 与预期不符：实际 %v，预期 %v", field, key, actual, expected)
						}
					}
				} else if !reflect.DeepEqual(message[field], want) {
					t.Errorf("%s 与预期不符：实际 %v，预期 %v", field, message[field], want)
				}
			}
		})
	}
	metadata := description["metadata"].(map[string]any)
	required := map[string]bool{}
	for _, capability := range metadata["capabilities"].([]any) {
		if capability != "decode" {
			required[capability.(string)] = true
		}
	}
	operations, _ := description["operations"].([]any)
	if len(operations) > 100 {
		t.Fatal("Operations 不能超过 100 条")
	}
	for i, item := range operations {
		c := item.(map[string]any)
		t.Run(fmt.Sprintf("接入/%d-%v", i+1, c["name"]), func(t *testing.T) {
			request := c["request"].(map[string]any)
			result := call(t, request)
			reply, _ := result["reply"].(string)
			replyBytes, err := hex.DecodeString(reply)
			if err != nil || len(replyBytes) > 64<<10 {
				t.Fatal("应答无效或过大")
			}
			switch request["operation"] {
			case "ingress":
				consumed, _ := result["consumed"].(float64)
				device, _ := result["deviceId"].(string)
				if result["needMore"] == true {
					if consumed != 0 || device != "" || reply != "" {
						t.Fatal("半帧不能消耗数据、识别设备或应答")
					}
				} else {
					data, err := hex.DecodeString(request["data"].(string))
					if err != nil || consumed <= 0 || consumed > float64(len(data)) || device == "" {
						t.Fatal("完整帧必须返回有效长度和设备标识")
					}
				}
			case "encode":
				if reply == "" {
					t.Fatal("下行未返回报文")
				}
			default:
				t.Fatal("未知操作")
			}
			if !sampleSubset(result, c["expected"]) {
				t.Fatalf("结果与预期不符：实际 %v，预期 %v", result, c["expected"])
			}
			if request["operation"] != "ingress" || result["needMore"] != true {
				delete(required, request["operation"].(string))
			}
		})
	}
	if len(required) > 0 {
		t.Fatalf("Operations 缺少完整成功样例：%v", required)
	}
}
