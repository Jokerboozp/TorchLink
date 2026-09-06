package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
)

type protocolOperationCase struct {
	Name     string                 `json:"name"`
	Request  protocolworker.Request `json:"request"`
	Expected map[string]any         `json:"expected"`
}

func validateProtocolOperationCases(ctx context.Context, root string, artifact map[string]any, entries map[string][]byte, manifest protocolPackageManifestV2) (int, error) {
	if manifest.Runtime != protocolworker.Runtime {
		return 0, nil
	}
	required := map[string]bool{}
	for _, c := range manifest.Capabilities {
		if c != "decode" {
			required[c] = true
		}
	}
	data := entries["samples/operations.json"]
	if len(required) == 0 && len(data) == 0 {
		return 0, nil
	}
	var cases []protocolOperationCase
	if len(data) > 1<<20 || json.Unmarshal(data, &cases) != nil || len(cases) == 0 || len(cases) > 100 {
		return 0, errors.New("完整协议包须包含 samples/operations.json（1 至 100 条操作样例）")
	}
	release := model.ProtocolRelease{ParserType: parser.GoProtocolParserName, Artifact: artifact, Capabilities: manifest.Capabilities, Config: map[string]any{"artifact": artifact, "timeoutMs": 10000}}
	for index, c := range cases {
		if len(c.Expected) == 0 {
			return index, fmt.Errorf("操作样例 %q 缺少预期结果", c.Name)
		}
		response, err := protocolworker.Call(ctx, root, release, c.Request)
		if err != nil {
			return index, fmt.Errorf("操作样例 %q 失败: %w", c.Name, err)
		}
		encoded, _ := json.Marshal(response)
		var actual map[string]any
		_ = json.Unmarshal(encoded, &actual)
		actual["consumed"], actual["needMore"], actual["reply"], actual["deviceId"] = float64(response.Consumed), response.NeedMore, response.Reply, response.DeviceID
		if !protocolSubset(actual, c.Expected) {
			return index, fmt.Errorf("操作样例 %q 结果与预期不符", c.Name)
		}
		if c.Request.Operation != "ingress" || !response.NeedMore {
			delete(required, c.Request.Operation)
		}
	}
	if len(required) > 0 {
		return len(cases), errors.New("操作样例须覆盖声明的每项 ingress/encode 能力，并至少成功处理一个完整帧")
	}
	return len(cases), nil
}

func protocolSubset(actual, expected any) bool {
	if wanted, ok := expected.(map[string]any); ok {
		values, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range wanted {
			if !protocolSubset(values[key], value) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(actual, expected)
}
