package protocolworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"reflect"
	"strconv"
	"time"
)

type SampleManifest struct {
	ID, Runtime, Transport, PayloadFormat string
	Capabilities                          []string
}
type SampleBundle struct {
	Cases      json.RawMessage `json:"cases"`
	Operations json.RawMessage `json:"operations,omitempty"`
}

func sampleName(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type SampleCase struct {
	Name                string            `json:"name"`
	Input               model.RawMessage  `json:"input"`
	ExpectedMessageType model.MessageType `json:"expectedMessageType"`
	ExpectedProperties  map[string]any    `json:"expectedProperties,omitempty"`
	ExpectedEvent       map[string]any    `json:"expectedEvent,omitempty"`
	ExpectedTags        map[string]string `json:"expectedTags,omitempty"`
	ExpectedEventType   string            `json:"expectedEventType,omitempty"`
	ExpectedTimestamp   int64             `json:"expectedTimestamp,omitempty"`
}

func ValidateSamples(parent context.Context, root string, artifact map[string]any, entries map[string][]byte, manifest SampleManifest, required bool) (int, error) {
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	defer cancel()
	data := entries["samples/cases.json"]
	if len(data) == 0 {
		if required {
			return 0, errors.New("samples/cases.json with at least one passing case is required for immediate publication")
		}
		return 0, nil
	}
	var cases []SampleCase
	if err := json.Unmarshal(data, &cases); len(data) > 1<<20 || err != nil || len(cases) == 0 || len(cases) > 100 {
		return 0, errors.New("samples/cases.json must contain 1 to 100 valid test cases")
	}
	runner := parser.ExternalParser{Root: root}
	// First execution can include OS signature/antivirus checks. Publication
	// validation therefore gets a wider bound than the steady-state parser.
	config := map[string]any{"artifact": artifact, "timeoutMs": 10000}
	for index, testCase := range cases {
		if err := ctx.Err(); err != nil {
			return index, fmt.Errorf("protocol sample validation canceled or exceeded 60 seconds: %w", err)
		}
		if testCase.Input.MessageID == "" {
			testCase.Input.MessageID = fmt.Sprintf("raw_package_test_%d", index+1)
		}
		if testCase.Input.TenantID == "" {
			testCase.Input.TenantID = "package-test"
		}
		if testCase.Input.ProductID == "" {
			testCase.Input.ProductID = "package-test"
		}
		if testCase.Input.DeviceID == "" {
			testCase.Input.DeviceID = "package-test"
		}
		if testCase.Input.Protocol == "" {
			testCase.Input.Protocol = manifest.ID
		}
		if testCase.Input.Transport == "" {
			testCase.Input.Transport = manifest.Transport
		}
		if testCase.Input.PayloadFormat == "" {
			testCase.Input.PayloadFormat = manifest.PayloadFormat
		}
		message, err := runner.ParseWithContext(ctx, testCase.Input, config)
		if err != nil {
			return index, fmt.Errorf("protocol package case %q failed: %w", sampleName(testCase.Name, strconv.Itoa(index+1)), err)
		}
		if testCase.ExpectedMessageType != "" && message.MessageType != testCase.ExpectedMessageType {
			return index, fmt.Errorf("protocol package case %q returned messageType %s, want %s", sampleName(testCase.Name, strconv.Itoa(index+1)), message.MessageType, testCase.ExpectedMessageType)
		}
		if testCase.ExpectedTimestamp != 0 && message.Timestamp != testCase.ExpectedTimestamp {
			return index, fmt.Errorf("protocol package case %q timestamp mismatch", testCase.Name)
		}
		if testCase.ExpectedEventType != "" && message.Event["type"] != testCase.ExpectedEventType {
			return index, fmt.Errorf("protocol package case %q event type mismatch", testCase.Name)
		}
		for key, expected := range testCase.ExpectedEvent {
			actual, exists := message.Event[key]
			if !exists || !reflect.DeepEqual(actual, expected) {
				return index, fmt.Errorf("protocol package case %q event %s mismatch", testCase.Name, key)
			}
		}
		for key, expected := range testCase.ExpectedTags {
			if actual, exists := message.Tags[key]; !exists || actual != expected {
				return index, fmt.Errorf("protocol package case %q tag %s mismatch", testCase.Name, key)
			}
		}
		for key, expected := range testCase.ExpectedProperties {
			actual, exists := message.Properties[key]
			if !exists || !reflect.DeepEqual(actual, expected) {
				return index, fmt.Errorf("protocol package case %q property %s=%v, want %v", sampleName(testCase.Name, strconv.Itoa(index+1)), key, message.Properties[key], expected)
			}
		}
	}
	operationCount, err := validateProtocolOperationCases(ctx, root, artifact, entries, manifest)
	return len(cases) + operationCount, err
}

type protocolOperationCase struct {
	Name     string         `json:"name"`
	Request  Request        `json:"request"`
	Expected map[string]any `json:"expected"`
}

func validateProtocolOperationCases(ctx context.Context, root string, artifact map[string]any, entries map[string][]byte, manifest SampleManifest) (int, error) {
	if manifest.Runtime != Runtime {
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
		response, err := Call(ctx, root, release, c.Request)
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
