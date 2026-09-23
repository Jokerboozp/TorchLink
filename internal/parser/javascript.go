package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"github.com/dop251/goja"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// JavaScriptParserName is a deliberately narrow extension point for protocol
// packages. The source is executed by Goja without access to the host process,
// filesystem, network or platform services; only a few pure conversion helpers
// are exposed. It is intended for payload transformations, not application
// code.
const JavaScriptParserName = "javascript_sandbox_parser" /* 声明 JavaScriptParserName。 */

const ( /* 执行当前语句并推进处理流程。 */
	JavaScriptParserVersion    = "1.0.0"                /* 更新 JavaScriptParserVersion 的值。 */
	MaxJavaScriptSourceBytes   = 64 * 1024              /* 更新 MaxJavaScriptSourceBytes 的值。 */
	MaxJavaScriptOutputBytes   = 512 * 1024             /* 更新 MaxJavaScriptOutputBytes 的值。 */
	JavaScriptExecutionTimeout = 250 * time.Millisecond /* 更新 JavaScriptExecutionTimeout 的值。 */
) /* 结束当前表达式或代码块。 */

type JavaScriptParser struct{} /* 定义 JavaScriptParser 类型。 */

func (JavaScriptParser) Name() string    { return JavaScriptParserName }    /* 定义 Name 函数。 */
func (JavaScriptParser) Version() string { return JavaScriptParserVersion } /* 定义 Version 函数。 */
func (JavaScriptParser) Match(m Meta) bool { /* 定义 Match 函数。 */
	return strings.EqualFold(m.Protocol, "javascript") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (p JavaScriptParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return p.ParseWithConfig(raw, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (JavaScriptParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	source, err := JavaScriptSource(config) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	input, err := rawInput(raw) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	vm := goja.New()             /* 更新 vm 的值。 */
	vm.SetMaxCallStackSize(128)  /* 执行当前语句并推进处理流程。 */
	installJavaScriptHelpers(vm) /* 执行当前语句并推进处理流程。 */

	timer := time.AfterFunc(JavaScriptExecutionTimeout, func() { /* 更新 timer 的值。 */
		vm.Interrupt("javascript parser execution timeout") /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	defer timer.Stop() /* 安排函数结束时执行清理。 */

	if _, err = vm.RunString(source); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("javascript parser compile failed: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parseValue := vm.Get("parse")                /* 更新 parseValue 的值。 */
	parse, ok := goja.AssertFunction(parseValue) /* 更新 ok 的值。 */
	if !ok {                                     /* 判断条件并选择处理分支。 */
		return nil, errors.New("javascript parser must define function parse(raw)") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err := parse(goja.Undefined(), vm.ToValue(input)) /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("javascript parser execution failed: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if result == nil || goja.IsUndefined(result) || goja.IsNull(result) { /* 判断条件并选择处理分支。 */
		return nil, errors.New("javascript parser returned an empty result") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	encoded, err := json.Marshal(result.Export()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("javascript parser returned non-JSON data: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(encoded) > MaxJavaScriptOutputBytes { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("javascript parser output exceeds %d bytes", MaxJavaScriptOutputBytes) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var output map[string]any                               /* 声明 output。 */
	if err = json.Unmarshal(encoded, &output); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("javascript parser must return an object: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return standardMessageFromScript(raw, output) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// JavaScriptSource validates the source stored in a protocol package. The
// same check is used by the management API before a package can be published.
func JavaScriptSource(config map[string]any) (string, error) { /* 定义 JavaScriptSource 函数。 */
	source, _ := config["source"].(string) /* 更新 _ 的值。 */
	source = strings.TrimSpace(source)     /* 更新 source 的值。 */
	if source == "" {                      /* 判断条件并选择处理分支。 */
		return "", errors.New("javascript parser source is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(source) > MaxJavaScriptSourceBytes { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("javascript parser source exceeds %d bytes", MaxJavaScriptSourceBytes) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return source, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func rawInput(raw model.RawMessage) (map[string]any, error) { /* 定义 rawInput 函数。 */
	b, err := json.Marshal(raw) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("encode raw message for javascript parser: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var input map[string]any                         /* 声明 input。 */
	if err = json.Unmarshal(b, &input); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("decode raw message for javascript parser: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return input, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func standardMessageFromScript(raw model.RawMessage, output map[string]any) (*model.StandardMessage, error) { /* 定义 standardMessageFromScript 函数。 */
	properties := map[string]any{}                              /* 更新 properties 的值。 */
	if value, ok := output["properties"].(map[string]any); ok { /* 判断条件并选择处理分支。 */
		properties = value /* 更新 properties 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		for name, value := range output { /* 循环处理当前数据。 */
			switch name { /* 根据条件选择处理路径。 */
			case "messageType", "timestamp", "event", "tags", "alarm": /* 处理当前分支。 */
			default: /* 处理当前分支。 */
				properties[name] = value /* 更新 properties[name] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	event := map[string]any{}                              /* 更新 event 的值。 */
	if value, ok := output["event"].(map[string]any); ok { /* 判断条件并选择处理分支。 */
		event = value /* 更新 event 的值。 */
	} /* 结束当前表达式或代码块。 */
	tags := map[string]string{}                           /* 更新 tags 的值。 */
	if value, ok := output["tags"].(map[string]any); ok { /* 判断条件并选择处理分支。 */
		for name, item := range value { /* 循环处理当前数据。 */
			tags[name] = fmt.Sprint(item) /* 更新 tags[name] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	messageType := model.PropertyReport                                                    /* 更新 messageType 的值。 */
	if value, ok := output["messageType"].(string); ok && strings.TrimSpace(value) != "" { /* 判断条件并选择处理分支。 */
		messageType = model.MessageType(strings.ToUpper(strings.TrimSpace(value))) /* 更新 messageType 的值。 */
	} else if len(event) > 0 { /* 结束当前表达式或代码块。 */
		messageType = model.EventReport /* 更新 messageType 的值。 */
	} else if alarm, ok := output["alarm"].(bool); ok && alarm { /* 结束当前表达式或代码块。 */
		messageType = model.AlarmReport /* 更新 messageType 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !validMessageType(messageType) { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("javascript parser returned unsupported messageType %q", messageType) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	timestamp := raw.ReceivedAt               /* 更新 timestamp 的值。 */
	if value, ok := output["timestamp"]; ok { /* 判断条件并选择处理分支。 */
		timestamp = configuredTimestamp(value, "", timestamp) /* 更新 timestamp 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{ /* 返回当前处理结果。 */
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, /* 执行当前语句并推进处理流程。 */
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, /* 执行当前语句并推进处理流程。 */
		MessageType: messageType, Timestamp: timestamp, Properties: properties, Event: event, Tags: tags, /* 执行当前语句并推进处理流程。 */
		Raw: map[string]any{"payloadFormat": raw.PayloadFormat, "payload": json.RawMessage(raw.Payload)}, /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func validMessageType(value model.MessageType) bool { /* 定义 validMessageType 函数。 */
	switch value { /* 根据条件选择处理路径。 */
	case model.PropertyReport, model.EventReport, model.StateChange, model.AlarmReport, model.CommandReply, model.LogReport: /* 处理当前分支。 */
		return true /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func installJavaScriptHelpers(vm *goja.Runtime) { /* 定义 installJavaScriptHelpers 函数。 */
	vm.Set("hexToBytes", func(call goja.FunctionCall) goja.Value { /* 执行当前语句并推进处理流程。 */
		text := strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(call.Argument(0).String()) /* 更新 text 的值。 */
		data, err := hex.DecodeString(text)                                                                   /* 更新 err 的值。 */
		if err != nil {                                                                                       /* 判断条件并选择处理分支。 */
			panic(vm.ToValue("invalid hex: " + err.Error())) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		values := make([]any, len(data)) /* 更新 values 的值。 */
		for i, value := range data {     /* 循环处理当前数据。 */
			values[i] = int(value) /* 更新 values[i] 的值。 */
		} /* 结束当前表达式或代码块。 */
		return vm.ToValue(values) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	vm.Set("toInt", func(call goja.FunctionCall) goja.Value { /* 执行当前语句并推进处理流程。 */
		value, err := strconv.ParseInt(strings.TrimSpace(call.Argument(0).String()), 10, 64) /* 更新 err 的值。 */
		if err != nil {                                                                      /* 判断条件并选择处理分支。 */
			panic(vm.ToValue("invalid integer: " + err.Error())) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return vm.ToValue(value) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
