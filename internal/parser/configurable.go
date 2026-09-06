package parser

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// ConfigurableJSONParser turns a JSON protocol package into a standard
// message using JSONPath-lite mappings. It supports paths such as
// $.data.temperature and $.events[0].code; it deliberately does not execute
// arbitrary scripts from a tenant's config.
type ConfigurableJSONParser struct{}

func (ConfigurableJSONParser) Name() string    { return "configurable_json_parser" }
func (ConfigurableJSONParser) Version() string { return "1.0.0" }
func (ConfigurableJSONParser) Match(m Meta) bool {
	return strings.EqualFold(m.Protocol, "config-json") && strings.EqualFold(m.PayloadFormat, "json")
}
func (p ConfigurableJSONParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	return p.ParseWithConfig(raw, nil)
}
func (ConfigurableJSONParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	var body any
	if err := json.Unmarshal(raw.Payload, &body); err != nil {
		return nil, err
	}
	root, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New("configurable JSON parser expects an object payload")
	}
	properties := map[string]any{}
	if mappings, ok := configMap(config, "properties", "propertyMappings"); ok && len(mappings) > 0 {
		for name, specification := range mappings {
			value, found, err := resolveMapping(body, specification)
			if err != nil {
				return nil, fmt.Errorf("property %q: %w", name, err)
			}
			if found {
				properties[name] = value
			}
		}
	} else if value, ok := root["properties"].(map[string]any); ok {
		properties = value
	} else {
		for name, value := range root {
			switch name {
			case "messageType", "event", "tags", "timestamp", "alarm":
			default:
				properties[name] = value
			}
		}
	}
	tags := map[string]string{}
	if mappings, ok := configMap(config, "tags", "tagMappings"); ok && len(mappings) > 0 {
		for name, specification := range mappings {
			value, found, err := resolveMapping(body, specification)
			if err != nil {
				return nil, fmt.Errorf("tag %q: %w", name, err)
			}
			if found {
				tags[name] = fmt.Sprint(value)
			}
		}
	} else if value, ok := root["tags"].(map[string]any); ok {
		for name, item := range value {
			tags[name] = fmt.Sprint(item)
		}
	}
	event := map[string]any{}
	if mappings, ok := configMap(config, "event", "eventMappings"); ok && len(mappings) > 0 {
		for name, specification := range mappings {
			value, found, err := resolveMapping(body, specification)
			if err != nil {
				return nil, fmt.Errorf("event %q: %w", name, err)
			}
			if found {
				event[name] = value
			}
		}
	} else if value, ok := root["event"].(map[string]any); ok {
		event = value
	}
	messageType := model.PropertyReport
	if value := firstConfig(config, "messageType"); value != "" {
		messageType = model.MessageType(strings.ToUpper(value))
	} else if path := firstConfig(config, "messageTypePath"); path != "" {
		if value, found := lookupPath(body, path); found {
			messageType = model.MessageType(strings.ToUpper(fmt.Sprint(value)))
		}
	} else if value, ok := root["messageType"].(string); ok {
		messageType = model.MessageType(strings.ToUpper(value))
	} else if len(event) > 0 {
		messageType = model.EventReport
	} else if alarm, ok := root["alarm"].(bool); ok && alarm {
		messageType = model.AlarmReport
	}
	timestamp := raw.ReceivedAt
	if path := firstConfig(config, "timestampPath"); path != "" {
		if value, found := lookupPath(body, path); found {
			timestamp = configuredTimestamp(value, firstConfig(config, "timestampUnit"), timestamp)
		}
	} else if value, ok := root["timestamp"]; ok {
		timestamp = configuredTimestamp(value, firstConfig(config, "timestampUnit"), timestamp)
	}
	return &model.StandardMessage{
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID,
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType,
		Timestamp: timestamp, Properties: properties, Event: event, Tags: tags,
		Raw: map[string]any{"payloadFormat": "json", "payload": json.RawMessage(raw.Payload), "mappingConfig": config},
	}, nil
}

// ConfigurableHexParser handles fixed-offset hex protocols. It is useful for
// simple sensors and lets a protocol package describe framing, checksum and
// scalar fields without compiling a new parser. Variable-length/TLV protocols
// use uploaded Go protocol packages.
type ConfigurableHexParser struct{}

func (ConfigurableHexParser) Name() string    { return "configurable_hex_parser" }
func (ConfigurableHexParser) Version() string { return "1.0.0" }
func (ConfigurableHexParser) Match(m Meta) bool {
	return strings.EqualFold(m.Protocol, "config-hex") && strings.EqualFold(m.PayloadFormat, "hex")
}
func (p ConfigurableHexParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	return p.ParseWithConfig(raw, nil)
}
func (ConfigurableHexParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	text := strings.NewReplacer("\"", "", " ", "", "\r", "", "\n", "", "\t", "").Replace(string(raw.Payload))
	data, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("invalid hex payload: %w", err)
	}
	if start := firstConfig(config, "startHex"); start != "" {
		prefix, decodeErr := hex.DecodeString(strings.ReplaceAll(start, " ", ""))
		if decodeErr != nil || len(data) < len(prefix) || !equalBytes(data[:len(prefix)], prefix) {
			return nil, errors.New("hex payload does not match configured startHex")
		}
	}
	if end := firstConfig(config, "endHex"); end != "" {
		suffix, decodeErr := hex.DecodeString(strings.ReplaceAll(end, " ", ""))
		if decodeErr != nil || len(data) < len(suffix) || !equalBytes(data[len(data)-len(suffix):], suffix) {
			return nil, errors.New("hex payload does not match configured endHex")
		}
	}
	if strings.EqualFold(firstConfig(config, "checksum"), "sum8") {
		checksumAt := len(data) - 1
		if firstConfig(config, "endHex") != "" {
			end, _ := hex.DecodeString(strings.ReplaceAll(firstConfig(config, "endHex"), " ", ""))
			checksumAt = len(data) - len(end) - 1
		}
		if checksumAt < 0 {
			return nil, errors.New("hex checksum position is invalid")
		}
		var sum byte
		start := 0
		if firstConfig(config, "checksumStartOffset") != "" {
			start = configInt(config, "checksumStartOffset", 0)
		}
		for _, value := range data[start:checksumAt] {
			sum += value
		}
		if data[checksumAt] != sum {
			return nil, fmt.Errorf("hex checksum mismatch: got %02X, want %02X", data[checksumAt], sum)
		}
	}
	properties := map[string]any{}
	fields, ok := config["fields"].([]any)
	if !ok {
		return nil, errors.New("configurable hex parser requires a fields array")
	}
	for i, rawField := range fields {
		field, ok := rawField.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("fields[%d] must be an object", i)
		}
		name := fmt.Sprint(field["name"])
		offset, length := intFrom(field["offset"]), intFrom(field["length"])
		if name == "" || offset < 0 || length <= 0 || offset+length > len(data) {
			return nil, fmt.Errorf("fields[%d] has an invalid name, offset or length", i)
		}
		value, err := decodeHexField(data[offset:offset+length], field)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		properties[name] = value
	}
	messageType := model.PropertyReport
	if value := firstConfig(config, "messageType"); value != "" {
		messageType = model.MessageType(strings.ToUpper(value))
	}
	return &model.StandardMessage{
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID,
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType,
		Timestamp: raw.ReceivedAt, Properties: properties, Event: map[string]any{}, Tags: map[string]string{},
		Raw: map[string]any{"payloadFormat": "hex", "payload": strings.ToUpper(text), "mappingConfig": config},
	}, nil
}

func decodeHexField(data []byte, spec map[string]any) (any, error) {
	kind := strings.ToLower(fmt.Sprint(spec["type"]))
	if kind == "" {
		kind = "uint16"
	}
	var endian binary.ByteOrder = binary.LittleEndian
	if strings.EqualFold(fmt.Sprint(spec["endian"]), "big") {
		endian = binary.BigEndian
	}
	var value any
	switch kind {
	case "uint8":
		if len(data) != 1 {
			return nil, errors.New("uint8 requires length 1")
		}
		value = uint64(data[0])
	case "int8":
		if len(data) != 1 {
			return nil, errors.New("int8 requires length 1")
		}
		value = int64(int8(data[0]))
	case "uint16":
		if len(data) != 2 {
			return nil, errors.New("uint16 requires length 2")
		}
		value = uint64(endian.Uint16(data))
	case "int16":
		if len(data) != 2 {
			return nil, errors.New("int16 requires length 2")
		}
		value = int64(int16(endian.Uint16(data)))
	case "uint32":
		if len(data) != 4 {
			return nil, errors.New("uint32 requires length 4")
		}
		value = uint64(endian.Uint32(data))
	case "int32":
		if len(data) != 4 {
			return nil, errors.New("int32 requires length 4")
		}
		value = int64(int32(endian.Uint32(data)))
	case "float32":
		if len(data) != 4 {
			return nil, errors.New("float32 requires length 4")
		}
		value = float64(math.Float32frombits(endian.Uint32(data)))
	case "ascii":
		value = strings.TrimRight(string(data), "\x00 ")
	case "hex":
		value = strings.ToUpper(hex.EncodeToString(data))
	default:
		return nil, fmt.Errorf("unsupported field type %q", kind)
	}
	if scale := floatFrom(spec["scale"]); scale != 0 && scale != 1 {
		if numberValue, ok := number(value); ok {
			value = numberValue * scale
		}
	}
	return value, nil
}

func configMap(config map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		if value, ok := config[key].(map[string]any); ok {
			return value, true
		}
	}
	return nil, false
}
func firstConfig(config map[string]any, key string) string {
	if value, ok := config[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}
func configInt(config map[string]any, key string, fallback int) int {
	if value, ok := config[key]; ok {
		if parsed, ok := value.(float64); ok {
			return int(parsed)
		}
		if parsed, err := strconv.Atoi(fmt.Sprint(value)); err == nil {
			return parsed
		}
	}
	return fallback
}
func intFrom(value any) int {
	if parsed, ok := value.(float64); ok {
		return int(parsed)
	}
	parsed, _ := strconv.Atoi(fmt.Sprint(value))
	return parsed
}
func floatFrom(value any) float64 {
	if parsed, ok := value.(float64); ok {
		return parsed
	}
	parsed, _ := strconv.ParseFloat(fmt.Sprint(value), 64)
	return parsed
}

func resolveMapping(root any, specification any) (any, bool, error) {
	path, kind, scale := "", "", float64(1)
	var fallback any
	hasFallback := false
	switch value := specification.(type) {
	case string:
		path = value
	case map[string]any:
		path = fmt.Sprint(value["path"])
		kind = strings.ToLower(fmt.Sprint(value["type"]))
		if value, ok := value["scale"]; ok {
			scale = floatFrom(value)
		}
		fallback, hasFallback = value["default"]
	default:
		return nil, false, errors.New("mapping must be a JSON path or object")
	}
	value, found := lookupPath(root, path)
	if !found {
		if hasFallback {
			return fallback, true, nil
		}
		return nil, false, nil
	}
	converted, err := convertValue(value, kind)
	if err != nil {
		return nil, false, err
	}
	if scale != 1 {
		if numberValue, ok := number(converted); ok {
			converted = numberValue * scale
		}
	}
	return converted, true, nil
}

func convertValue(value any, kind string) (any, error) {
	switch kind {
	case "", "json":
		return value, nil
	case "number":
		if n, ok := number(value); ok {
			return n, nil
		}
	case "integer":
		if n, ok := number(value); ok {
			return int64(n), nil
		}
	case "boolean":
		if parsed, ok := value.(bool); ok {
			return parsed, nil
		}
		parsed, err := strconv.ParseBool(fmt.Sprint(value))
		if err == nil {
			return parsed, nil
		}
	case "string":
		return fmt.Sprint(value), nil
	default:
		return nil, fmt.Errorf("unsupported mapping type %q", kind)
	}
	return nil, fmt.Errorf("cannot convert %v to %s", value, kind)
}

func lookupPath(root any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" || path == "$" {
		return root, true
	}
	if strings.HasPrefix(path, "$.") {
		path = strings.TrimPrefix(path, "$")
	}
	if strings.HasPrefix(path, ".") {
		path = strings.TrimPrefix(path, ".")
	}
	current := root
	for len(path) > 0 {
		part := path
		if dot := strings.IndexAny(path, ".["); dot >= 0 {
			part, path = path[:dot], path[dot:]
		} else {
			path = ""
		}
		if part != "" {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok = object[part]
			if !ok {
				return nil, false
			}
		}
		if strings.HasPrefix(path, ".") {
			path = strings.TrimPrefix(path, ".")
			continue
		}
		if strings.HasPrefix(path, "[") {
			end := strings.Index(path, "]")
			if end < 0 {
				return nil, false
			}
			index, err := strconv.Atoi(path[1:end])
			if err != nil {
				return nil, false
			}
			array, ok := current.([]any)
			if !ok || index < 0 || index >= len(array) {
				return nil, false
			}
			current = array[index]
			path = path[end+1:]
			if strings.HasPrefix(path, ".") {
				path = strings.TrimPrefix(path, ".")
			}
			continue
		}
		if path != "" {
			return nil, false
		}
	}
	return current, true
}

func configuredTimestamp(value any, unit string, fallback int64) int64 {
	if numeric, ok := number(value); ok {
		if strings.EqualFold(unit, "s") || strings.EqualFold(unit, "second") || strings.EqualFold(unit, "seconds") {
			return int64(numeric * 1000)
		}
		return int64(numeric)
	}
	if text, ok := value.(string); ok {
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed.UnixMilli()
		}
	}
	return fallback
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
