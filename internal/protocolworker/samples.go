package protocolworker /* 声明 protocolworker 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                      /* 执行当前语句并推进处理流程。 */
	"encoding/json"                /* 执行当前语句并推进处理流程。 */
	"errors"                       /* 执行当前语句并推进处理流程。 */
	"fmt"                          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
	"reflect"                      /* 执行当前语句并推进处理流程。 */
	"strconv"                      /* 执行当前语句并推进处理流程。 */
	"time"                         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type SampleManifest struct { /* 定义 SampleManifest 类型。 */
	ID, Runtime, Transport, PayloadFormat string   /* 执行当前语句并推进处理流程。 */
	Capabilities                          []string /* 执行当前语句并推进处理流程。 */
}                          /* 结束当前表达式或代码块。 */
type SampleBundle struct { /* 定义 SampleBundle 类型。 */
	Cases      json.RawMessage `json:"cases"`                /* 执行当前语句并推进处理流程。 */
	Operations json.RawMessage `json:"operations,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func sampleName(values ...string) string { /* 定义 sampleName 函数。 */
	for _, value := range values { /* 循环处理当前数据。 */
		if value != "" { /* 判断条件并选择处理分支。 */
			return value /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type SampleCase struct { /* 定义 SampleCase 类型。 */
	Name                string            `json:"name"`                         /* 执行当前语句并推进处理流程。 */
	Input               model.RawMessage  `json:"input"`                        /* 执行当前语句并推进处理流程。 */
	ExpectedMessageType model.MessageType `json:"expectedMessageType"`          /* 执行当前语句并推进处理流程。 */
	ExpectedProperties  map[string]any    `json:"expectedProperties,omitempty"` /* 执行当前语句并推进处理流程。 */
	ExpectedEvent       map[string]any    `json:"expectedEvent,omitempty"`      /* 执行当前语句并推进处理流程。 */
	ExpectedTags        map[string]string `json:"expectedTags,omitempty"`       /* 执行当前语句并推进处理流程。 */
	ExpectedEventType   string            `json:"expectedEventType,omitempty"`  /* 执行当前语句并推进处理流程。 */
	ExpectedTimestamp   int64             `json:"expectedTimestamp,omitempty"`  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func ValidateSamples(parent context.Context, root string, artifact map[string]any, entries map[string][]byte, manifest SampleManifest, required bool) (int, error) { /* 定义 ValidateSamples 函数。 */
	ctx, cancel := context.WithTimeout(parent, time.Minute) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	data := entries["samples/cases.json"]                   /* 更新 data 的值。 */
	if len(data) == 0 {                                     /* 判断条件并选择处理分支。 */
		if required { /* 判断条件并选择处理分支。 */
			return 0, errors.New("samples/cases.json with at least one passing case is required for immediate publication") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var cases []SampleCase                                                                                           /* 声明 cases。 */
	if err := json.Unmarshal(data, &cases); len(data) > 1<<20 || err != nil || len(cases) == 0 || len(cases) > 100 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("samples/cases.json must contain 1 to 100 valid test cases") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	runner := parser.ExternalParser{Root: root} /* 更新 runner 的值。 */
	// First execution can include OS signature/antivirus checks. Publication
	// validation therefore gets a wider bound than the steady-state parser.
	config := map[string]any{"artifact": artifact, "timeoutMs": 10000} /* 更新 config 的值。 */
	for index, testCase := range cases {                               /* 循环处理当前数据。 */
		if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("protocol sample validation canceled or exceeded 60 seconds: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.Input.MessageID == "" { /* 判断条件并选择处理分支。 */
			testCase.Input.MessageID = fmt.Sprintf("raw_package_test_%d", index+1) /* 更新 testCase.Input.MessageID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.Input.TenantID == "" { /* 判断条件并选择处理分支。 */
			testCase.Input.TenantID = "package-test" /* 更新 testCase.Input.TenantID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.Input.ProductID == "" { /* 判断条件并选择处理分支。 */
			testCase.Input.ProductID = "package-test" /* 更新 testCase.Input.ProductID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.Input.DeviceID == "" { /* 判断条件并选择处理分支。 */
			testCase.Input.DeviceID = "package-test" /* 更新 testCase.Input.DeviceID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.Input.Protocol == "" { /* 判断条件并选择处理分支。 */
			testCase.Input.Protocol = manifest.ID /* 更新 testCase.Input.Protocol 的值。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.Input.Transport == "" { /* 判断条件并选择处理分支。 */
			testCase.Input.Transport = manifest.Transport /* 更新 testCase.Input.Transport 的值。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.Input.PayloadFormat == "" { /* 判断条件并选择处理分支。 */
			testCase.Input.PayloadFormat = manifest.PayloadFormat /* 更新 testCase.Input.PayloadFormat 的值。 */
		} /* 结束当前表达式或代码块。 */
		message, err := runner.ParseWithContext(ctx, testCase.Input, config) /* 更新 err 的值。 */
		if err != nil {                                                      /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("protocol package case %q failed: %w", sampleName(testCase.Name, strconv.Itoa(index+1)), err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.ExpectedMessageType != "" && message.MessageType != testCase.ExpectedMessageType { /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("protocol package case %q returned messageType %s, want %s", sampleName(testCase.Name, strconv.Itoa(index+1)), message.MessageType, testCase.ExpectedMessageType) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.ExpectedTimestamp != 0 && message.Timestamp != testCase.ExpectedTimestamp { /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("protocol package case %q timestamp mismatch", testCase.Name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if testCase.ExpectedEventType != "" && message.Event["type"] != testCase.ExpectedEventType { /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("protocol package case %q event type mismatch", testCase.Name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for key, expected := range testCase.ExpectedEvent { /* 循环处理当前数据。 */
			actual, exists := message.Event[key]                 /* 更新 exists 的值。 */
			if !exists || !reflect.DeepEqual(actual, expected) { /* 判断条件并选择处理分支。 */
				return index, fmt.Errorf("protocol package case %q event %s mismatch", testCase.Name, key) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		for key, expected := range testCase.ExpectedTags { /* 循环处理当前数据。 */
			if actual, exists := message.Tags[key]; !exists || actual != expected { /* 判断条件并选择处理分支。 */
				return index, fmt.Errorf("protocol package case %q tag %s mismatch", testCase.Name, key) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		for key, expected := range testCase.ExpectedProperties { /* 循环处理当前数据。 */
			actual, exists := message.Properties[key]            /* 更新 exists 的值。 */
			if !exists || !reflect.DeepEqual(actual, expected) { /* 判断条件并选择处理分支。 */
				return index, fmt.Errorf("protocol package case %q property %s=%v, want %v", sampleName(testCase.Name, strconv.Itoa(index+1)), key, message.Properties[key], expected) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	operationCount, err := validateProtocolOperationCases(ctx, root, artifact, entries, manifest) /* 更新 err 的值。 */
	return len(cases) + operationCount, err                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type protocolOperationCase struct { /* 定义 protocolOperationCase 类型。 */
	Name     string         `json:"name"`     /* 执行当前语句并推进处理流程。 */
	Request  Request        `json:"request"`  /* 执行当前语句并推进处理流程。 */
	Expected map[string]any `json:"expected"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func validateProtocolOperationCases(ctx context.Context, root string, artifact map[string]any, entries map[string][]byte, manifest SampleManifest) (int, error) { /* 定义 validateProtocolOperationCases 函数。 */
	if manifest.Runtime != Runtime { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	required := map[string]bool{}             /* 更新 required 的值。 */
	for _, c := range manifest.Capabilities { /* 循环处理当前数据。 */
		if c != "decode" { /* 判断条件并选择处理分支。 */
			required[c] = true /* 更新 required[c] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	data := entries["samples/operations.json"] /* 更新 data 的值。 */
	if len(required) == 0 && len(data) == 0 {  /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var cases []protocolOperationCase                                                                    /* 声明 cases。 */
	if len(data) > 1<<20 || json.Unmarshal(data, &cases) != nil || len(cases) == 0 || len(cases) > 100 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("完整协议包须包含 samples/operations.json（1 至 100 条操作样例）") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release := model.ProtocolRelease{ParserType: parser.GoProtocolParserName, Artifact: artifact, Capabilities: manifest.Capabilities, Config: map[string]any{"artifact": artifact, "timeoutMs": 10000}} /* 更新 release 的值。 */
	for index, c := range cases {                                                                                                                                                                        /* 循环处理当前数据。 */
		if len(c.Expected) == 0 { /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("操作样例 %q 缺少预期结果", c.Name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		response, err := Call(ctx, root, release, c.Request) /* 更新 err 的值。 */
		if err != nil {                                      /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("操作样例 %q 失败: %w", c.Name, err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		encoded, _ := json.Marshal(response)                                                                                                                           /* 更新 _ 的值。 */
		var actual map[string]any                                                                                                                                      /* 声明 actual。 */
		_ = json.Unmarshal(encoded, &actual)                                                                                                                           /* 更新 _ 的值。 */
		actual["consumed"], actual["needMore"], actual["reply"], actual["deviceId"] = float64(response.Consumed), response.NeedMore, response.Reply, response.DeviceID /* 执行当前语句并推进处理流程。 */
		if !protocolSubset(actual, c.Expected) {                                                                                                                       /* 判断条件并选择处理分支。 */
			return index, fmt.Errorf("操作样例 %q 结果与预期不符", c.Name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if c.Request.Operation != "ingress" || !response.NeedMore { /* 判断条件并选择处理分支。 */
			delete(required, c.Request.Operation) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(required) > 0 { /* 判断条件并选择处理分支。 */
		return len(cases), errors.New("操作样例须覆盖声明的每项 ingress/encode 能力，并至少成功处理一个完整帧") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return len(cases), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func protocolSubset(actual, expected any) bool { /* 定义 protocolSubset 函数。 */
	if wanted, ok := expected.(map[string]any); ok { /* 判断条件并选择处理分支。 */
		values, ok := actual.(map[string]any) /* 更新 ok 的值。 */
		if !ok {                              /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for key, value := range wanted { /* 循环处理当前数据。 */
			if !protocolSubset(values[key], value) { /* 判断条件并选择处理分支。 */
				return false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return reflect.DeepEqual(actual, expected) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
