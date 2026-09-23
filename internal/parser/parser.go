package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Meta struct{ TenantID, ProductID, DeviceID, Protocol, PayloadFormat string } /* 定义 Meta 类型。 */
type Parser interface {                                                           /* 定义 Parser 类型。 */
	Name() string                                           /* 执行当前语句并推进处理流程。 */
	Version() string                                        /* 执行当前语句并推进处理流程。 */
	Match(Meta) bool                                        /* 执行当前语句并推进处理流程。 */
	Parse(model.RawMessage) (*model.StandardMessage, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ConfigurableParser is implemented by parsers whose field layout is safe to
// describe in a protocol package's JSON config. The executable parser remains
// built into the service; only the mapping/layout is tenant-managed data.
type ConfigurableParser interface { /* 定义 ConfigurableParser 类型。 */
	ParseWithConfig(model.RawMessage, map[string]any) (*model.StandardMessage, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Registry struct{ parsers, automatic []Parser } /* 定义 Registry 类型。 */

func NewRegistry(parsers ...Parser) *Registry { return &Registry{parsers: parsers, automatic: parsers} } /* 定义 NewRegistry 函数。 */
func (r *Registry) Register(p Parser) { /* 定义 Register 函数。 */
	r.parsers = append(r.parsers, p)     /* 更新 r.parsers 的值。 */
	r.automatic = append(r.automatic, p) /* 更新 r.automatic 的值。 */
} /* 结束当前表达式或代码块。 */

// NewPlatformRegistry admits new protocols through configurable formats or Go
// artifacts. Older named parsers remain callable only by an explicit binding
// or historical replay, so existing devices can migrate without losing history.
func NewPlatformRegistry(root string) *Registry { /* 定义 NewPlatformRegistry 函数。 */
	r := NewRegistry(StandardParser{}, ConfigurableJSONParser{}, ConfigurableHexParser{}, ExternalParser{Root: root}, JSONParser{})                                                          /* 更新 r 的值。 */
	r.parsers = append(r.parsers, PollResponseParser{}, GB26875Parser{}, ModbusTCPParser{}, ModbusRTUParser{}, ModbusCoilParser{}, JavaScriptParser{}, FireSmokeHexParser{}, ModbusParser{}) /* 更新 r.parsers 的值。 */
	return r                                                                                                                                                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ManagedParserTypes() []string { /* 定义 ManagedParserTypes 函数。 */
	return []string{"custom_json_parser", "configurable_json_parser", "configurable_hex_parser", GoProtocolParserName} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func ManagedParserType(name string) bool { /* 定义 ManagedParserType 函数。 */
	for _, candidate := range ManagedParserTypes() { /* 循环处理当前数据。 */
		if name == candidate { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Registry) ParseWith(name string, raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 ParseWith 函数。 */
	return r.ParseVersionWithConfig(name, "", nil, raw) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Registry) ParseVersion(name, version string, raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 ParseVersion 函数。 */
	return r.ParseVersionWithConfig(name, version, nil, raw) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Registry) ParseWithConfig(name string, config map[string]any, raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	return r.ParseVersionWithConfig(name, "", config, raw) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Registry) ParseVersionWithConfig(name, version string, config map[string]any, raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 ParseVersionWithConfig 函数。 */
	for _, p := range r.parsers { /* 循环处理当前数据。 */
		if p.Name() != name || version != "" && p.Version() != version { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var m *model.StandardMessage                        /* 声明 m。 */
		var err error                                       /* 声明 err。 */
		if configurable, ok := p.(ConfigurableParser); ok { /* 判断条件并选择处理分支。 */
			m, err = configurable.ParseWithConfig(raw, config) /* 更新 err 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			m, err = p.Parse(raw) /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("%s@%s: %w", p.Name(), p.Version(), err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		m.Parser = p.Name()           /* 更新 m.Parser 的值。 */
		m.ParserVersion = p.Version() /* 更新 m.ParserVersion 的值。 */
		return m, nil                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if version != "" { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("parser %q version %q is not registered", name, version) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil, fmt.Errorf("parser %q is not registered", name) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Registry) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	meta := Meta{raw.TenantID, raw.ProductID, raw.DeviceID, raw.Protocol, raw.PayloadFormat} /* 更新 meta 的值。 */
	for _, p := range r.automatic {                                                          /* 循环处理当前数据。 */
		if p.Match(meta) { /* 判断条件并选择处理分支。 */
			m, err := p.Parse(raw) /* 更新 err 的值。 */
			if err != nil {        /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("%s@%s: %w", p.Name(), p.Version(), err) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			m.Parser = p.Name()           /* 更新 m.Parser 的值。 */
			m.ParserVersion = p.Version() /* 更新 m.ParserVersion 的值。 */
			return m, nil                 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil, fmt.Errorf("no parser matched product=%s protocol=%s format=%s", raw.ProductID, raw.Protocol, raw.PayloadFormat) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type JSONParser struct{} /* 定义 JSONParser 类型。 */

func (JSONParser) Name() string    { return "custom_json_parser" } /* 定义 Name 函数。 */
func (JSONParser) Version() string { return "1.0.0" }              /* 定义 Version 函数。 */
func (JSONParser) Match(m Meta) bool { /* 定义 Match 函数。 */
	return strings.EqualFold(m.PayloadFormat, "json") || strings.EqualFold(m.Protocol, "json") || strings.Contains(m.ProductID, "json") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (JSONParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	var body map[string]any                                    /* 声明 body。 */
	if err := json.Unmarshal(raw.Payload, &body); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	messageType := model.PropertyReport            /* 更新 messageType 的值。 */
	if v, ok := body["messageType"].(string); ok { /* 判断条件并选择处理分支。 */
		messageType = model.MessageType(strings.ToUpper(v)) /* 更新 messageType 的值。 */
	} else if _, ok := body["event"]; ok { /* 结束当前表达式或代码块。 */
		messageType = model.EventReport /* 更新 messageType 的值。 */
	} else if _, ok := body["businessStatus"]; ok { /* 结束当前表达式或代码块。 */
		messageType = model.StateChange /* 更新 messageType 的值。 */
	} else if v, ok := body["alarm"].(bool); ok && v { /* 结束当前表达式或代码块。 */
		messageType = model.AlarmReport /* 更新 messageType 的值。 */
	} /* 结束当前表达式或代码块。 */
	props := map[string]any{}                             /* 更新 props 的值。 */
	if v, ok := body["properties"].(map[string]any); ok { /* 判断条件并选择处理分支。 */
		props = v /* 更新 props 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		for k, v := range body { /* 循环处理当前数据。 */
			switch k { /* 根据条件选择处理路径。 */
			case "messageType", "event", "tags", "timestamp", "alarm": /* 处理当前分支。 */
			default: /* 处理当前分支。 */
				props[k] = v /* 更新 props[k] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	tags := map[string]string{}                     /* 更新 tags 的值。 */
	if v, ok := body["tags"].(map[string]any); ok { /* 判断条件并选择处理分支。 */
		for k, x := range v { /* 循环处理当前数据。 */
			tags[k] = fmt.Sprint(x) /* 更新 tags[k] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	ts := raw.ReceivedAt                        /* 更新 ts 的值。 */
	if v, ok := number(body["timestamp"]); ok { /* 判断条件并选择处理分支。 */
		ts = int64(v) /* 更新 ts 的值。 */
	} /* 结束当前表达式或代码块。 */
	event := map[string]any{}                        /* 更新 event 的值。 */
	if v, ok := body["event"].(map[string]any); ok { /* 判断条件并选择处理分支。 */
		event = v /* 更新 event 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType, Timestamp: ts, Properties: props, Event: event, Tags: tags, Raw: map[string]any{"payloadFormat": raw.PayloadFormat, "payload": json.RawMessage(raw.Payload)}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type FireSmokeHexParser struct{} /* 定义 FireSmokeHexParser 类型。 */

func (FireSmokeHexParser) Name() string    { return "fire_smoke_parser" } /* 定义 Name 函数。 */
func (FireSmokeHexParser) Version() string { return "1.0.0" }             /* 定义 Version 函数。 */
func (FireSmokeHexParser) Match(m Meta) bool { /* 定义 Match 函数。 */
	return strings.Contains(strings.ToLower(m.ProductID), "smoke") && strings.EqualFold(m.PayloadFormat, "hex") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (FireSmokeHexParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	s := strings.Trim(string(raw.Payload), "\"")                  /* 更新 s 的值。 */
	data, err := hex.DecodeString(strings.ReplaceAll(s, " ", "")) /* 更新 err 的值。 */
	if err != nil {                                               /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < 4 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("smoke payload requires at least 4 bytes") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	smoke := data[0]&1 == 1                                                /* 更新 smoke 的值。 */
	temperature := float64(int16(uint16(data[1])<<8|uint16(data[2]))) / 10 /* 更新 temperature 的值。 */
	battery := int(data[3])                                                /* 更新 battery 的值。 */
	kind := model.PropertyReport                                           /* 更新 kind 的值。 */
	if smoke {                                                             /* 判断条件并选择处理分支。 */
		kind = model.AlarmReport /* 更新 kind 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: kind, Timestamp: raw.ReceivedAt, Properties: map[string]any{"smoke": smoke, "temperature": temperature, "battery": battery}, Tags: map[string]string{}, Raw: map[string]any{"payloadFormat": "hex", "payload": s}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type ModbusParser struct{} /* 定义 ModbusParser 类型。 */

func (ModbusParser) Name() string    { return "modbus_parser" } /* 定义 Name 函数。 */
func (ModbusParser) Version() string { return "1.0.0" }         /* 定义 Version 函数。 */
func (ModbusParser) Match(m Meta) bool { /* 定义 Match 函数。 */
	return strings.Contains(strings.ToLower(m.Protocol), "modbus") && strings.EqualFold(m.PayloadFormat, "hex") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (ModbusParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	s := strings.Trim(string(raw.Payload), "\"")                  /* 更新 s 的值。 */
	data, err := hex.DecodeString(strings.ReplaceAll(s, " ", "")) /* 更新 err 的值。 */
	if err != nil {                                               /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < 5 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("modbus payload too short") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	byteCount := int(data[2])    /* 更新 byteCount 的值。 */
	if len(data) < 3+byteCount { /* 判断条件并选择处理分支。 */
		return nil, errors.New("modbus byte count exceeds payload") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	props := map[string]any{}             /* 更新 props 的值。 */
	for i := 0; i+1 < byteCount; i += 2 { /* 循环处理当前数据。 */
		props["register_"+strconv.Itoa(i/2)] = int(uint16(data[3+i])<<8 | uint16(data[4+i])) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: model.PropertyReport, Timestamp: raw.ReceivedAt, Properties: props, Tags: map[string]string{}, Raw: map[string]any{"payloadFormat": "hex", "payload": s}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func number(v any) (float64, bool) { /* 定义 number 函数。 */
	switch x := v.(type) { /* 根据条件选择处理路径。 */
	case float64: /* 处理当前分支。 */
		return x, true /* 返回当前处理结果。 */
	case int: /* 处理当前分支。 */
		return float64(x), true /* 返回当前处理结果。 */
	case int64: /* 处理当前分支。 */
		return float64(x), true /* 返回当前处理结果。 */
	case uint64: /* 处理当前分支。 */
		return float64(x), true /* 返回当前处理结果。 */
	case json.Number: /* 处理当前分支。 */
		n, e := x.Float64() /* 更新 e 的值。 */
		return n, e == nil  /* 返回当前处理结果。 */
	case string: /* 处理当前分支。 */
		n, e := strconv.ParseFloat(x, 64) /* 更新 e 的值。 */
		return n, e == nil                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
