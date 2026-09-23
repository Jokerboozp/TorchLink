package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"math"            /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// ConfigurableJSONParser turns a JSON protocol package into a standard
// message using JSONPath-lite mappings. It supports paths such as
// $.data.temperature and $.events[0].code; it deliberately does not execute
// arbitrary scripts from a tenant's config.
type ConfigurableJSONParser struct{} /* 定义 ConfigurableJSONParser 类型。 */

func (ConfigurableJSONParser) Name() string    { return "configurable_json_parser" } /* 定义 Name 函数。 */
func (ConfigurableJSONParser) Version() string { return "1.0.0" }                    /* 定义 Version 函数。 */
func (ConfigurableJSONParser) Match(m Meta) bool { /* 定义 Match 函数。 */
	return strings.EqualFold(m.Protocol, "config-json") && strings.EqualFold(m.PayloadFormat, "json") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (p ConfigurableJSONParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return p.ParseWithConfig(raw, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (ConfigurableJSONParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	var body any                                               /* 声明 body。 */
	if err := json.Unmarshal(raw.Payload, &body); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root, ok := body.(map[string]any) /* 更新 ok 的值。 */
	if !ok {                          /* 判断条件并选择处理分支。 */
		return nil, errors.New("configurable JSON parser expects an object payload") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	properties := map[string]any{}                                                                    /* 更新 properties 的值。 */
	if mappings, ok := configMap(config, "properties", "propertyMappings"); ok && len(mappings) > 0 { /* 判断条件并选择处理分支。 */
		for name, specification := range mappings { /* 循环处理当前数据。 */
			value, found, err := resolveMapping(body, specification) /* 更新 err 的值。 */
			if err != nil {                                          /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("property %q: %w", name, err) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if found { /* 判断条件并选择处理分支。 */
				properties[name] = value /* 更新 properties[name] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} else if value, ok := root["properties"].(map[string]any); ok { /* 结束当前表达式或代码块。 */
		properties = value /* 更新 properties 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		for name, value := range root { /* 循环处理当前数据。 */
			switch name { /* 根据条件选择处理路径。 */
			case "messageType", "event", "tags", "timestamp", "alarm": /* 处理当前分支。 */
			default: /* 处理当前分支。 */
				properties[name] = value /* 更新 properties[name] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	tags := map[string]string{}                                                            /* 更新 tags 的值。 */
	if mappings, ok := configMap(config, "tags", "tagMappings"); ok && len(mappings) > 0 { /* 判断条件并选择处理分支。 */
		for name, specification := range mappings { /* 循环处理当前数据。 */
			value, found, err := resolveMapping(body, specification) /* 更新 err 的值。 */
			if err != nil {                                          /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("tag %q: %w", name, err) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if found { /* 判断条件并选择处理分支。 */
				tags[name] = fmt.Sprint(value) /* 更新 tags[name] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} else if value, ok := root["tags"].(map[string]any); ok { /* 结束当前表达式或代码块。 */
		for name, item := range value { /* 循环处理当前数据。 */
			tags[name] = fmt.Sprint(item) /* 更新 tags[name] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	event := map[string]any{}                                                                 /* 更新 event 的值。 */
	if mappings, ok := configMap(config, "event", "eventMappings"); ok && len(mappings) > 0 { /* 判断条件并选择处理分支。 */
		for name, specification := range mappings { /* 循环处理当前数据。 */
			value, found, err := resolveMapping(body, specification) /* 更新 err 的值。 */
			if err != nil {                                          /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("event %q: %w", name, err) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if found { /* 判断条件并选择处理分支。 */
				event[name] = value /* 更新 event[name] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} else if value, ok := root["event"].(map[string]any); ok { /* 结束当前表达式或代码块。 */
		event = value /* 更新 event 的值。 */
	} /* 结束当前表达式或代码块。 */
	messageType := model.PropertyReport                           /* 更新 messageType 的值。 */
	if value := firstConfig(config, "messageType"); value != "" { /* 判断条件并选择处理分支。 */
		messageType = model.MessageType(strings.ToUpper(value)) /* 更新 messageType 的值。 */
	} else if path := firstConfig(config, "messageTypePath"); path != "" { /* 结束当前表达式或代码块。 */
		if value, found := lookupPath(body, path); found { /* 判断条件并选择处理分支。 */
			messageType = model.MessageType(strings.ToUpper(fmt.Sprint(value))) /* 更新 messageType 的值。 */
		} /* 结束当前表达式或代码块。 */
	} else if value, ok := root["messageType"].(string); ok { /* 结束当前表达式或代码块。 */
		messageType = model.MessageType(strings.ToUpper(value)) /* 更新 messageType 的值。 */
	} else if len(event) > 0 { /* 结束当前表达式或代码块。 */
		messageType = model.EventReport /* 更新 messageType 的值。 */
	} else if alarm, ok := root["alarm"].(bool); ok && alarm { /* 结束当前表达式或代码块。 */
		messageType = model.AlarmReport /* 更新 messageType 的值。 */
	} /* 结束当前表达式或代码块。 */
	timestamp := raw.ReceivedAt                                   /* 更新 timestamp 的值。 */
	if path := firstConfig(config, "timestampPath"); path != "" { /* 判断条件并选择处理分支。 */
		if value, found := lookupPath(body, path); found { /* 判断条件并选择处理分支。 */
			timestamp = configuredTimestamp(value, firstConfig(config, "timestampUnit"), timestamp) /* 更新 timestamp 的值。 */
		} /* 结束当前表达式或代码块。 */
	} else if value, ok := root["timestamp"]; ok { /* 结束当前表达式或代码块。 */
		timestamp = configuredTimestamp(value, firstConfig(config, "timestampUnit"), timestamp) /* 更新 timestamp 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{ /* 返回当前处理结果。 */
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, /* 执行当前语句并推进处理流程。 */
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType, /* 执行当前语句并推进处理流程。 */
		Timestamp: timestamp, Properties: properties, Event: event, Tags: tags, /* 执行当前语句并推进处理流程。 */
		Raw: map[string]any{"payloadFormat": "json", "payload": json.RawMessage(raw.Payload), "mappingConfig": config}, /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// ConfigurableHexParser handles fixed-offset hex protocols. It is useful for
// simple sensors and lets a protocol package describe framing, checksum and
// scalar fields without compiling a new parser. Variable-length/TLV protocols
// use uploaded Go protocol packages.
type ConfigurableHexParser struct{} /* 定义 ConfigurableHexParser 类型。 */

func (ConfigurableHexParser) Name() string    { return "configurable_hex_parser" } /* 定义 Name 函数。 */
func (ConfigurableHexParser) Version() string { return "1.0.0" }                   /* 定义 Version 函数。 */
func (ConfigurableHexParser) Match(m Meta) bool { /* 定义 Match 函数。 */
	return strings.EqualFold(m.Protocol, "config-hex") && strings.EqualFold(m.PayloadFormat, "hex") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (p ConfigurableHexParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return p.ParseWithConfig(raw, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (ConfigurableHexParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	text := strings.NewReplacer("\"", "", " ", "", "\r", "", "\n", "", "\t", "").Replace(string(raw.Payload)) /* 更新 text 的值。 */
	data, err := hex.DecodeString(text)                                                                       /* 更新 err 的值。 */
	if err != nil {                                                                                           /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid hex payload: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if start := firstConfig(config, "startHex"); start != "" { /* 判断条件并选择处理分支。 */
		prefix, decodeErr := hex.DecodeString(strings.ReplaceAll(start, " ", ""))                   /* 更新 decodeErr 的值。 */
		if decodeErr != nil || len(data) < len(prefix) || !equalBytes(data[:len(prefix)], prefix) { /* 判断条件并选择处理分支。 */
			return nil, errors.New("hex payload does not match configured startHex") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if end := firstConfig(config, "endHex"); end != "" { /* 判断条件并选择处理分支。 */
		suffix, decodeErr := hex.DecodeString(strings.ReplaceAll(end, " ", ""))                               /* 更新 decodeErr 的值。 */
		if decodeErr != nil || len(data) < len(suffix) || !equalBytes(data[len(data)-len(suffix):], suffix) { /* 判断条件并选择处理分支。 */
			return nil, errors.New("hex payload does not match configured endHex") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	checksum := strings.ToLower(strings.TrimSpace(firstConfig(config, "checksum"))) /* 更新 checksum 的值。 */
	if checksum != "" && checksum != "sum8" {                                       /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("unsupported HEX checksum %q; use a Go protocol package for this checksum", checksum) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if checksum == "sum8" { /* 判断条件并选择处理分支。 */
		checksumAt := len(data) - 1              /* 更新 checksumAt 的值。 */
		if firstConfig(config, "endHex") != "" { /* 判断条件并选择处理分支。 */
			end, _ := hex.DecodeString(strings.ReplaceAll(firstConfig(config, "endHex"), " ", "")) /* 更新 _ 的值。 */
			checksumAt = len(data) - len(end) - 1                                                  /* 更新 checksumAt 的值。 */
		} /* 结束当前表达式或代码块。 */
		if checksumAt < 0 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("hex checksum position is invalid") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var sum byte                                          /* 声明 sum。 */
		start := 0                                            /* 更新 start 的值。 */
		if firstConfig(config, "checksumStartOffset") != "" { /* 判断条件并选择处理分支。 */
			start = configInt(config, "checksumStartOffset", 0) /* 更新 start 的值。 */
		} /* 结束当前表达式或代码块。 */
		if start < 0 || start > checksumAt { /* 判断条件并选择处理分支。 */
			return nil, errors.New("hex checksum start offset is invalid") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, value := range data[start:checksumAt] { /* 循环处理当前数据。 */
			sum += value /* 更新 sum 的值。 */
		} /* 结束当前表达式或代码块。 */
		if data[checksumAt] != sum { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("hex checksum mismatch: got %02X, want %02X", data[checksumAt], sum) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	properties := map[string]any{}         /* 更新 properties 的值。 */
	fields, ok := config["fields"].([]any) /* 更新 ok 的值。 */
	if !ok {                               /* 判断条件并选择处理分支。 */
		return nil, errors.New("configurable hex parser requires a fields array") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i, rawField := range fields { /* 循环处理当前数据。 */
		field, ok := rawField.(map[string]any) /* 更新 ok 的值。 */
		if !ok {                               /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("fields[%d] must be an object", i) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		name := fmt.Sprint(field["name"])                                                               /* 更新 name 的值。 */
		offset, length := intFrom(field["offset"]), intFrom(field["length"])                            /* 更新 length 的值。 */
		if name == "" || offset < 0 || length <= 0 || length > len(data) || offset > len(data)-length { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("fields[%d] has an invalid name, offset or length", i) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value, err := decodeHexField(data[offset:offset+length], field) /* 更新 err 的值。 */
		if err != nil {                                                 /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("field %q: %w", name, err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		properties[name] = value /* 更新 properties[name] 的值。 */
	} /* 结束当前表达式或代码块。 */
	messageType := model.PropertyReport                           /* 更新 messageType 的值。 */
	if value := firstConfig(config, "messageType"); value != "" { /* 判断条件并选择处理分支。 */
		messageType = model.MessageType(strings.ToUpper(value)) /* 更新 messageType 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{ /* 返回当前处理结果。 */
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, /* 执行当前语句并推进处理流程。 */
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType, /* 执行当前语句并推进处理流程。 */
		Timestamp: raw.ReceivedAt, Properties: properties, Event: map[string]any{}, Tags: map[string]string{}, /* 执行当前语句并推进处理流程。 */
		Raw: map[string]any{"payloadFormat": "hex", "payload": strings.ToUpper(text), "mappingConfig": config}, /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func decodeHexField(data []byte, spec map[string]any) (any, error) { /* 定义 decodeHexField 函数。 */
	kind := strings.ToLower(fmt.Sprint(spec["type"])) /* 更新 kind 的值。 */
	if kind == "" {                                   /* 判断条件并选择处理分支。 */
		kind = "uint16" /* 更新 kind 的值。 */
	} /* 结束当前表达式或代码块。 */
	var endian binary.ByteOrder = binary.LittleEndian         /* 声明 endian。 */
	if strings.EqualFold(fmt.Sprint(spec["endian"]), "big") { /* 判断条件并选择处理分支。 */
		endian = binary.BigEndian /* 更新 endian 的值。 */
	} /* 结束当前表达式或代码块。 */
	var value any /* 声明 value。 */
	switch kind { /* 根据条件选择处理路径。 */
	case "uint8": /* 处理当前分支。 */
		if len(data) != 1 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("uint8 requires length 1") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value = uint64(data[0]) /* 更新 value 的值。 */
	case "int8": /* 处理当前分支。 */
		if len(data) != 1 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("int8 requires length 1") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value = int64(int8(data[0])) /* 更新 value 的值。 */
	case "uint16": /* 处理当前分支。 */
		if len(data) != 2 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("uint16 requires length 2") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value = uint64(endian.Uint16(data)) /* 更新 value 的值。 */
	case "int16": /* 处理当前分支。 */
		if len(data) != 2 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("int16 requires length 2") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value = int64(int16(endian.Uint16(data))) /* 更新 value 的值。 */
	case "uint32": /* 处理当前分支。 */
		if len(data) != 4 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("uint32 requires length 4") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value = uint64(endian.Uint32(data)) /* 更新 value 的值。 */
	case "int32": /* 处理当前分支。 */
		if len(data) != 4 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("int32 requires length 4") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value = int64(int32(endian.Uint32(data))) /* 更新 value 的值。 */
	case "float32": /* 处理当前分支。 */
		if len(data) != 4 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("float32 requires length 4") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value = float64(math.Float32frombits(endian.Uint32(data))) /* 更新 value 的值。 */
	case "ascii": /* 处理当前分支。 */
		value = strings.TrimRight(string(data), "\x00 ") /* 更新 value 的值。 */
	case "hex": /* 处理当前分支。 */
		value = strings.ToUpper(hex.EncodeToString(data)) /* 更新 value 的值。 */
	default: /* 处理当前分支。 */
		return nil, fmt.Errorf("unsupported field type %q", kind) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if scale := floatFrom(spec["scale"]); scale != 0 && scale != 1 { /* 判断条件并选择处理分支。 */
		if numberValue, ok := number(value); ok { /* 判断条件并选择处理分支。 */
			value = numberValue * scale /* 更新 value 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return value, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func configMap(config map[string]any, keys ...string) (map[string]any, bool) { /* 定义 configMap 函数。 */
	for _, key := range keys { /* 循环处理当前数据。 */
		if value, ok := config[key].(map[string]any); ok { /* 判断条件并选择处理分支。 */
			return value, true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func firstConfig(config map[string]any, key string) string { /* 定义 firstConfig 函数。 */
	if value, ok := config[key]; ok && value != nil { /* 判断条件并选择处理分支。 */
		return strings.TrimSpace(fmt.Sprint(value)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func configInt(config map[string]any, key string, fallback int) int { /* 定义 configInt 函数。 */
	if value, ok := config[key]; ok { /* 判断条件并选择处理分支。 */
		if parsed, ok := value.(float64); ok { /* 判断条件并选择处理分支。 */
			return int(parsed) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if parsed, err := strconv.Atoi(fmt.Sprint(value)); err == nil { /* 判断条件并选择处理分支。 */
			return parsed /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func intFrom(value any) int { /* 定义 intFrom 函数。 */
	if parsed, ok := value.(float64); ok { /* 判断条件并选择处理分支。 */
		return int(parsed) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parsed, _ := strconv.Atoi(fmt.Sprint(value)) /* 更新 _ 的值。 */
	return parsed                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func floatFrom(value any) float64 { /* 定义 floatFrom 函数。 */
	if parsed, ok := value.(float64); ok { /* 判断条件并选择处理分支。 */
		return parsed /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64) /* 更新 _ 的值。 */
	return parsed                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func resolveMapping(root any, specification any) (any, bool, error) { /* 定义 resolveMapping 函数。 */
	path, kind, scale := "", "", float64(1) /* 更新 scale 的值。 */
	var fallback any                        /* 声明 fallback。 */
	hasFallback := false                    /* 更新 hasFallback 的值。 */
	switch value := specification.(type) {  /* 根据条件选择处理路径。 */
	case string: /* 处理当前分支。 */
		path = value /* 更新 path 的值。 */
	case map[string]any: /* 处理当前分支。 */
		path = fmt.Sprint(value["path"])                  /* 更新 path 的值。 */
		kind = strings.ToLower(fmt.Sprint(value["type"])) /* 更新 kind 的值。 */
		if value, ok := value["scale"]; ok {              /* 判断条件并选择处理分支。 */
			scale = floatFrom(value) /* 更新 scale 的值。 */
		} /* 结束当前表达式或代码块。 */
		fallback, hasFallback = value["default"] /* 更新 hasFallback 的值。 */
	default: /* 处理当前分支。 */
		return nil, false, errors.New("mapping must be a JSON path or object") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	value, found := lookupPath(root, path) /* 更新 found 的值。 */
	if !found {                            /* 判断条件并选择处理分支。 */
		if hasFallback { /* 判断条件并选择处理分支。 */
			return fallback, true, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return nil, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	converted, err := convertValue(value, kind) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		return nil, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if scale != 1 { /* 判断条件并选择处理分支。 */
		if numberValue, ok := number(converted); ok { /* 判断条件并选择处理分支。 */
			converted = numberValue * scale /* 更新 converted 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return converted, true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func convertValue(value any, kind string) (any, error) { /* 定义 convertValue 函数。 */
	switch kind { /* 根据条件选择处理路径。 */
	case "", "json": /* 处理当前分支。 */
		return value, nil /* 返回当前处理结果。 */
	case "number": /* 处理当前分支。 */
		if n, ok := number(value); ok { /* 判断条件并选择处理分支。 */
			return n, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case "integer": /* 处理当前分支。 */
		if n, ok := number(value); ok { /* 判断条件并选择处理分支。 */
			return int64(n), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case "boolean": /* 处理当前分支。 */
		if parsed, ok := value.(bool); ok { /* 判断条件并选择处理分支。 */
			return parsed, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		parsed, err := strconv.ParseBool(fmt.Sprint(value)) /* 更新 err 的值。 */
		if err == nil {                                     /* 判断条件并选择处理分支。 */
			return parsed, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case "string": /* 处理当前分支。 */
		return fmt.Sprint(value), nil /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return nil, fmt.Errorf("unsupported mapping type %q", kind) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil, fmt.Errorf("cannot convert %v to %s", value, kind) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func lookupPath(root any, path string) (any, bool) { /* 定义 lookupPath 函数。 */
	path = strings.TrimSpace(path) /* 更新 path 的值。 */
	if path == "" || path == "$" { /* 判断条件并选择处理分支。 */
		return root, true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasPrefix(path, "$.") { /* 判断条件并选择处理分支。 */
		path = strings.TrimPrefix(path, "$") /* 更新 path 的值。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasPrefix(path, ".") { /* 判断条件并选择处理分支。 */
		path = strings.TrimPrefix(path, ".") /* 更新 path 的值。 */
	} /* 结束当前表达式或代码块。 */
	current := root     /* 更新 current 的值。 */
	for len(path) > 0 { /* 循环处理当前数据。 */
		part := path                                       /* 更新 part 的值。 */
		if dot := strings.IndexAny(path, ".["); dot >= 0 { /* 判断条件并选择处理分支。 */
			part, path = path[:dot], path[dot:] /* 更新 path 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			path = "" /* 更新 path 的值。 */
		} /* 结束当前表达式或代码块。 */
		if part != "" { /* 判断条件并选择处理分支。 */
			object, ok := current.(map[string]any) /* 更新 ok 的值。 */
			if !ok {                               /* 判断条件并选择处理分支。 */
				return nil, false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			current, ok = object[part] /* 更新 ok 的值。 */
			if !ok {                   /* 判断条件并选择处理分支。 */
				return nil, false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if strings.HasPrefix(path, ".") { /* 判断条件并选择处理分支。 */
			path = strings.TrimPrefix(path, ".") /* 更新 path 的值。 */
			continue                             /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if strings.HasPrefix(path, "[") { /* 判断条件并选择处理分支。 */
			end := strings.Index(path, "]") /* 更新 end 的值。 */
			if end < 0 {                    /* 判断条件并选择处理分支。 */
				return nil, false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			index, err := strconv.Atoi(path[1:end]) /* 更新 err 的值。 */
			if err != nil {                         /* 判断条件并选择处理分支。 */
				return nil, false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			array, ok := current.([]any)                 /* 更新 ok 的值。 */
			if !ok || index < 0 || index >= len(array) { /* 判断条件并选择处理分支。 */
				return nil, false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			current = array[index]            /* 更新 current 的值。 */
			path = path[end+1:]               /* 更新 path 的值。 */
			if strings.HasPrefix(path, ".") { /* 判断条件并选择处理分支。 */
				path = strings.TrimPrefix(path, ".") /* 更新 path 的值。 */
			} /* 结束当前表达式或代码块。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if path != "" { /* 判断条件并选择处理分支。 */
			return nil, false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return current, true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func configuredTimestamp(value any, unit string, fallback int64) int64 { /* 定义 configuredTimestamp 函数。 */
	if numeric, ok := number(value); ok { /* 判断条件并选择处理分支。 */
		if strings.EqualFold(unit, "s") || strings.EqualFold(unit, "second") || strings.EqualFold(unit, "seconds") { /* 判断条件并选择处理分支。 */
			return int64(numeric * 1000) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return int64(numeric) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if text, ok := value.(string); ok { /* 判断条件并选择处理分支。 */
		if parsed, err := time.Parse(time.RFC3339, text); err == nil { /* 判断条件并选择处理分支。 */
			return parsed.UnixMilli() /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func equalBytes(a, b []byte) bool { /* 定义 equalBytes 函数。 */
	if len(a) != len(b) { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i := range a { /* 循环处理当前数据。 */
		if a[i] != b[i] { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
