package parser

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"iot-platform/internal/model"
)

// JavaScriptParserName is a deliberately narrow extension point for protocol
// packages. The source is executed by Goja without access to the host process,
// filesystem, network or platform services; only a few pure conversion helpers
// are exposed. It is intended for payload transformations, not application
// code.
const JavaScriptParserName = "javascript_sandbox_parser"

const (
	JavaScriptParserVersion    = "1.0.0"
	MaxJavaScriptSourceBytes   = 64 * 1024
	MaxJavaScriptOutputBytes   = 512 * 1024
	JavaScriptExecutionTimeout = 250 * time.Millisecond
)

type JavaScriptParser struct{}

func (JavaScriptParser) Name() string    { return JavaScriptParserName }
func (JavaScriptParser) Version() string { return JavaScriptParserVersion }
func (JavaScriptParser) Match(m Meta) bool {
	return strings.EqualFold(m.Protocol, "javascript")
}
func (p JavaScriptParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	return p.ParseWithConfig(raw, nil)
}

func (JavaScriptParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	source, err := JavaScriptSource(config)
	if err != nil {
		return nil, err
	}

	input, err := rawInput(raw)
	if err != nil {
		return nil, err
	}
	vm := goja.New()
	vm.SetMaxCallStackSize(128)
	installJavaScriptHelpers(vm)

	timer := time.AfterFunc(JavaScriptExecutionTimeout, func() {
		vm.Interrupt("javascript parser execution timeout")
	})
	defer timer.Stop()

	if _, err = vm.RunString(source); err != nil {
		return nil, fmt.Errorf("javascript parser compile failed: %w", err)
	}
	parseValue := vm.Get("parse")
	parse, ok := goja.AssertFunction(parseValue)
	if !ok {
		return nil, errors.New("javascript parser must define function parse(raw)")
	}
	result, err := parse(goja.Undefined(), vm.ToValue(input))
	if err != nil {
		return nil, fmt.Errorf("javascript parser execution failed: %w", err)
	}
	if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
		return nil, errors.New("javascript parser returned an empty result")
	}

	encoded, err := json.Marshal(result.Export())
	if err != nil {
		return nil, fmt.Errorf("javascript parser returned non-JSON data: %w", err)
	}
	if len(encoded) > MaxJavaScriptOutputBytes {
		return nil, fmt.Errorf("javascript parser output exceeds %d bytes", MaxJavaScriptOutputBytes)
	}
	var output map[string]any
	if err = json.Unmarshal(encoded, &output); err != nil {
		return nil, fmt.Errorf("javascript parser must return an object: %w", err)
	}
	return standardMessageFromScript(raw, output)
}

// JavaScriptSource validates the source stored in a protocol package. The
// same check is used by the management API before a package can be published.
func JavaScriptSource(config map[string]any) (string, error) {
	source, _ := config["source"].(string)
	source = strings.TrimSpace(source)
	if source == "" {
		return "", errors.New("javascript parser source is required")
	}
	if len(source) > MaxJavaScriptSourceBytes {
		return "", fmt.Errorf("javascript parser source exceeds %d bytes", MaxJavaScriptSourceBytes)
	}
	return source, nil
}

func rawInput(raw model.RawMessage) (map[string]any, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode raw message for javascript parser: %w", err)
	}
	var input map[string]any
	if err = json.Unmarshal(b, &input); err != nil {
		return nil, fmt.Errorf("decode raw message for javascript parser: %w", err)
	}
	return input, nil
}

func standardMessageFromScript(raw model.RawMessage, output map[string]any) (*model.StandardMessage, error) {
	properties := map[string]any{}
	if value, ok := output["properties"].(map[string]any); ok {
		properties = value
	} else {
		for name, value := range output {
			switch name {
			case "messageType", "timestamp", "event", "tags", "alarm":
			default:
				properties[name] = value
			}
		}
	}
	event := map[string]any{}
	if value, ok := output["event"].(map[string]any); ok {
		event = value
	}
	tags := map[string]string{}
	if value, ok := output["tags"].(map[string]any); ok {
		for name, item := range value {
			tags[name] = fmt.Sprint(item)
		}
	}
	messageType := model.PropertyReport
	if value, ok := output["messageType"].(string); ok && strings.TrimSpace(value) != "" {
		messageType = model.MessageType(strings.ToUpper(strings.TrimSpace(value)))
	} else if len(event) > 0 {
		messageType = model.EventReport
	} else if alarm, ok := output["alarm"].(bool); ok && alarm {
		messageType = model.AlarmReport
	}
	if !validMessageType(messageType) {
		return nil, fmt.Errorf("javascript parser returned unsupported messageType %q", messageType)
	}
	timestamp := raw.ReceivedAt
	if value, ok := output["timestamp"]; ok {
		timestamp = configuredTimestamp(value, "", timestamp)
	}
	return &model.StandardMessage{
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID,
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID,
		MessageType: messageType, Timestamp: timestamp, Properties: properties, Event: event, Tags: tags,
		Raw: map[string]any{"payloadFormat": raw.PayloadFormat, "payload": json.RawMessage(raw.Payload)},
	}, nil
}

func validMessageType(value model.MessageType) bool {
	switch value {
	case model.PropertyReport, model.EventReport, model.StateChange, model.AlarmReport, model.CommandReply, model.LogReport:
		return true
	default:
		return false
	}
}

func installJavaScriptHelpers(vm *goja.Runtime) {
	vm.Set("hexToBytes", func(call goja.FunctionCall) goja.Value {
		text := strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(call.Argument(0).String())
		data, err := hex.DecodeString(text)
		if err != nil {
			panic(vm.ToValue("invalid hex: " + err.Error()))
		}
		values := make([]any, len(data))
		for i, value := range data {
			values[i] = int(value)
		}
		return vm.ToValue(values)
	})
	vm.Set("toInt", func(call goja.FunctionCall) goja.Value {
		value, err := strconv.ParseInt(strings.TrimSpace(call.Argument(0).String()), 10, 64)
		if err != nil {
			panic(vm.ToValue("invalid integer: " + err.Error()))
		}
		return vm.ToValue(value)
	})
}
