package parser

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

type Meta struct{ TenantID, ProductID, DeviceID, Protocol, PayloadFormat string }
type Parser interface {
	Name() string
	Version() string
	Match(Meta) bool
	Parse(model.RawMessage) (*model.StandardMessage, error)
}

// ConfigurableParser is implemented by parsers whose field layout is safe to
// describe in a protocol package's JSON config. The executable parser remains
// built into the service; only the mapping/layout is tenant-managed data.
type ConfigurableParser interface {
	ParseWithConfig(model.RawMessage, map[string]any) (*model.StandardMessage, error)
}

type Registry struct{ parsers, automatic []Parser }

func NewRegistry(parsers ...Parser) *Registry { return &Registry{parsers: parsers, automatic: parsers} }
func (r *Registry) Register(p Parser) {
	r.parsers = append(r.parsers, p)
	r.automatic = append(r.automatic, p)
}

// NewPlatformRegistry admits new protocols through configurable formats or Go
// artifacts. Older named parsers remain callable only by an explicit binding
// or historical replay, so existing devices can migrate without losing history.
func NewPlatformRegistry(root string) *Registry {
	r := NewRegistry(ConfigurableJSONParser{}, ConfigurableHexParser{}, ExternalParser{Root: root}, JSONParser{})
	r.parsers = append(r.parsers, GB26875Parser{}, ModbusTCPParser{}, ModbusCoilParser{}, JavaScriptParser{}, FireSmokeHexParser{}, ModbusParser{})
	return r
}

func ManagedParserTypes() []string {
	return []string{"custom_json_parser", "configurable_json_parser", "configurable_hex_parser", GoProtocolParserName}
}
func ManagedParserType(name string) bool {
	for _, candidate := range ManagedParserTypes() {
		if name == candidate {
			return true
		}
	}
	return false
}
func (r *Registry) ParseWith(name string, raw model.RawMessage) (*model.StandardMessage, error) {
	return r.ParseVersionWithConfig(name, "", nil, raw)
}
func (r *Registry) ParseVersion(name, version string, raw model.RawMessage) (*model.StandardMessage, error) {
	return r.ParseVersionWithConfig(name, version, nil, raw)
}
func (r *Registry) ParseWithConfig(name string, config map[string]any, raw model.RawMessage) (*model.StandardMessage, error) {
	return r.ParseVersionWithConfig(name, "", config, raw)
}
func (r *Registry) ParseVersionWithConfig(name, version string, config map[string]any, raw model.RawMessage) (*model.StandardMessage, error) {
	for _, p := range r.parsers {
		if p.Name() != name || version != "" && p.Version() != version {
			continue
		}
		var m *model.StandardMessage
		var err error
		if configurable, ok := p.(ConfigurableParser); ok {
			m, err = configurable.ParseWithConfig(raw, config)
		} else {
			m, err = p.Parse(raw)
		}
		if err != nil {
			return nil, fmt.Errorf("%s@%s: %w", p.Name(), p.Version(), err)
		}
		m.Parser = p.Name()
		m.ParserVersion = p.Version()
		return m, nil
	}
	if version != "" {
		return nil, fmt.Errorf("parser %q version %q is not registered", name, version)
	}
	return nil, fmt.Errorf("parser %q is not registered", name)
}
func (r *Registry) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	meta := Meta{raw.TenantID, raw.ProductID, raw.DeviceID, raw.Protocol, raw.PayloadFormat}
	for _, p := range r.automatic {
		if p.Match(meta) {
			m, err := p.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("%s@%s: %w", p.Name(), p.Version(), err)
			}
			m.Parser = p.Name()
			m.ParserVersion = p.Version()
			return m, nil
		}
	}
	return nil, fmt.Errorf("no parser matched product=%s protocol=%s format=%s", raw.ProductID, raw.Protocol, raw.PayloadFormat)
}

type JSONParser struct{}

func (JSONParser) Name() string    { return "custom_json_parser" }
func (JSONParser) Version() string { return "1.0.0" }
func (JSONParser) Match(m Meta) bool {
	return strings.EqualFold(m.PayloadFormat, "json") || strings.EqualFold(m.Protocol, "json") || strings.Contains(m.ProductID, "json")
}
func (JSONParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	var body map[string]any
	if err := json.Unmarshal(raw.Payload, &body); err != nil {
		return nil, err
	}
	messageType := model.PropertyReport
	if v, ok := body["messageType"].(string); ok {
		messageType = model.MessageType(strings.ToUpper(v))
	} else if _, ok := body["event"]; ok {
		messageType = model.EventReport
	} else if _, ok := body["businessStatus"]; ok {
		messageType = model.StateChange
	} else if v, ok := body["alarm"].(bool); ok && v {
		messageType = model.AlarmReport
	}
	props := map[string]any{}
	if v, ok := body["properties"].(map[string]any); ok {
		props = v
	} else {
		for k, v := range body {
			switch k {
			case "messageType", "event", "tags", "timestamp", "alarm":
			default:
				props[k] = v
			}
		}
	}
	tags := map[string]string{}
	if v, ok := body["tags"].(map[string]any); ok {
		for k, x := range v {
			tags[k] = fmt.Sprint(x)
		}
	}
	ts := raw.ReceivedAt
	if v, ok := number(body["timestamp"]); ok {
		ts = int64(v)
	}
	event := map[string]any{}
	if v, ok := body["event"].(map[string]any); ok {
		event = v
	}
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType, Timestamp: ts, Properties: props, Event: event, Tags: tags, Raw: map[string]any{"payloadFormat": raw.PayloadFormat, "payload": json.RawMessage(raw.Payload)}}, nil
}

type FireSmokeHexParser struct{}

func (FireSmokeHexParser) Name() string    { return "fire_smoke_parser" }
func (FireSmokeHexParser) Version() string { return "1.0.0" }
func (FireSmokeHexParser) Match(m Meta) bool {
	return strings.Contains(strings.ToLower(m.ProductID), "smoke") && strings.EqualFold(m.PayloadFormat, "hex")
}
func (FireSmokeHexParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	s := strings.Trim(string(raw.Payload), "\"")
	data, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return nil, err
	}
	if len(data) < 4 {
		return nil, errors.New("smoke payload requires at least 4 bytes")
	}
	smoke := data[0]&1 == 1
	temperature := float64(int16(uint16(data[1])<<8|uint16(data[2]))) / 10
	battery := int(data[3])
	kind := model.PropertyReport
	if smoke {
		kind = model.AlarmReport
	}
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: kind, Timestamp: raw.ReceivedAt, Properties: map[string]any{"smoke": smoke, "temperature": temperature, "battery": battery}, Tags: map[string]string{}, Raw: map[string]any{"payloadFormat": "hex", "payload": s}}, nil
}

type ModbusParser struct{}

func (ModbusParser) Name() string    { return "modbus_parser" }
func (ModbusParser) Version() string { return "1.0.0" }
func (ModbusParser) Match(m Meta) bool {
	return strings.Contains(strings.ToLower(m.Protocol), "modbus") && strings.EqualFold(m.PayloadFormat, "hex")
}
func (ModbusParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	s := strings.Trim(string(raw.Payload), "\"")
	data, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		return nil, err
	}
	if len(data) < 5 {
		return nil, errors.New("modbus payload too short")
	}
	byteCount := int(data[2])
	if len(data) < 3+byteCount {
		return nil, errors.New("modbus byte count exceeds payload")
	}
	props := map[string]any{}
	for i := 0; i+1 < byteCount; i += 2 {
		props["register_"+strconv.Itoa(i/2)] = int(uint16(data[3+i])<<8 | uint16(data[4+i]))
	}
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: model.PropertyReport, Timestamp: raw.ReceivedAt, Properties: props, Tags: map[string]string{}, Raw: map[string]any{"payloadFormat": "hex", "payload": s}}, nil
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		n, e := x.Float64()
		return n, e == nil
	case string:
		n, e := strconv.ParseFloat(x, 64)
		return n, e == nil
	}
	return 0, false
}
