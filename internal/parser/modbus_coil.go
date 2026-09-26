package parser

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

// ModbusCoilParserName is a reviewed Go parser for a Modbus read-coils
// response. It is intentionally declarative: the assistant stores addresses
// and field metadata in Config, while the runtime conversion stays in Go.
const ModbusCoilParserName = "modbus_coil_parser"

const ModbusCoilParserVersion = "1.0.0"

type ModbusCoilParser struct{}

func (ModbusCoilParser) Name() string    { return ModbusCoilParserName }
func (ModbusCoilParser) Version() string { return ModbusCoilParserVersion }
func (ModbusCoilParser) Match(Meta) bool { return false }

func (p ModbusCoilParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	return nil, errors.New("modbus coil parser requires a mapping configuration")
}

func (p ModbusCoilParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	fields, err := validateModbusCoilConfig(config)
	if err != nil {
		return nil, err
	}
	data, err := decodeHexPayload(raw.Payload)
	if err != nil {
		return nil, err
	}
	frame := strings.ToLower(strings.TrimSpace(firstConfig(config, "frame")))
	if frame == "" {
		frame = "auto"
	}
	dataOffset, byteCountIndex, functionIndex, err := modbusCoilFrame(data, frame)
	if err != nil {
		return nil, err
	}
	functionCode := configInt(config, "functionCode", 1)
	if functionCode <= 0 || functionCode > 0xff {
		return nil, errors.New("modbus coil parser functionCode must be between 1 and 255")
	}
	if data[functionIndex]&0x80 != 0 {
		return nil, fmt.Errorf("modbus exception response function code 0x%02X", data[functionIndex])
	}
	if int(data[functionIndex]) != functionCode {
		return nil, fmt.Errorf("unexpected Modbus function code 0x%02X, want 0x%02X", data[functionIndex], functionCode)
	}
	byteCount := int(data[byteCountIndex])
	if byteCount <= 0 || dataOffset+byteCount > len(data) {
		return nil, fmt.Errorf("modbus coil byte count %d exceeds payload", byteCount)
	}
	startAddress := configInt(config, "startAddress", 0)
	properties := make(map[string]any, len(fields))
	for _, field := range fields {
		address := modbusCoilFieldAddress(field)
		bitOffset := address - startAddress
		if bitOffset < 0 {
			return nil, fmt.Errorf("field %q address %d is before startAddress %d", fieldName(field), address, startAddress)
		}
		byteOffset := bitOffset / 8
		if byteOffset >= byteCount {
			return nil, fmt.Errorf("field %q address %d is outside the response", fieldName(field), address)
		}
		properties[fieldName(field)] = data[dataOffset+byteOffset]&(1<<uint(bitOffset%8)) != 0
	}
	messageType := model.MessageType(strings.ToUpper(firstConfig(config, "messageType")))
	if messageType == "" {
		messageType = model.PropertyReport
	}
	return &model.StandardMessage{
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID,
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType,
		Timestamp: raw.ReceivedAt, Properties: properties, Event: map[string]any{}, Tags: map[string]string{},
		Raw: map[string]any{"payloadFormat": "hex", "payload": strings.ToUpper(strings.Trim(string(raw.Payload), `"`)), "mappingConfig": config},
	}, nil
}

// ValidateModbusCoilConfig is used by the HTTP layer before a package is
// saved. It does not need a sample payload, so a user can save an address map
// first and attach/verify a real response later.
func ValidateModbusCoilConfig(config map[string]any) error {
	_, err := validateModbusCoilConfig(config)
	return err
}

func validateModbusCoilConfig(config map[string]any) ([]map[string]any, error) {
	if config == nil {
		return nil, errors.New("modbus coil parser configuration is required")
	}
	messageType := model.MessageType(strings.ToUpper(firstConfig(config, "messageType")))
	if messageType != "" && !validModbusMessageType(messageType) {
		return nil, fmt.Errorf("unsupported messageType %q", messageType)
	}
	frame := strings.ToLower(strings.TrimSpace(firstConfig(config, "frame")))
	if frame != "" && frame != "auto" && frame != "tcp" && frame != "rtu" {
		return nil, fmt.Errorf("unsupported Modbus frame %q", frame)
	}
	startAddress := configInt(config, "startAddress", 0)
	if startAddress < 0 {
		return nil, errors.New("modbus coil parser startAddress must not be negative")
	}
	fields, err := modbusCoilFields(config["fields"])
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for index, field := range fields {
		name := fieldName(field)
		if name == "" {
			return nil, fmt.Errorf("fields[%d] name is required", index)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate Modbus coil field %q", name)
		}
		seen[name] = struct{}{}
		address, ok := parseCoilAddress(field)
		if !ok || address < startAddress {
			return nil, fmt.Errorf("fields[%d] coilAddress is invalid", index)
		}
	}
	return fields, nil
}

func modbusCoilFields(value any) ([]map[string]any, error) {
	switch values := value.(type) {
	case []any:
		fields := make([]map[string]any, 0, len(values))
		for index, value := range values {
			field, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("fields[%d] must be an object", index)
			}
			fields = append(fields, field)
		}
		return fields, nil
	case []map[string]any:
		return values, nil
	default:
		return nil, errors.New("modbus coil parser requires a fields array")
	}
}

func modbusCoilFieldAddress(field map[string]any) int {
	address, _ := parseCoilAddress(field)
	return address
}

var coilAddressPattern = regexp.MustCompile(`(?i)^[A-Z]*\s*(\d+)$`)

func parseCoilAddress(field map[string]any) (int, bool) {
	for _, key := range []string{"coilAddress", "address", "modbusAddress"} {
		value, exists := field[key]
		if !exists || value == nil {
			continue
		}
		if number, ok := value.(float64); ok {
			return int(number), number >= 0 && number == float64(int(number))
		}
		if number, ok := value.(int); ok {
			return number, number >= 0
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		match := coilAddressPattern.FindStringSubmatch(text)
		if len(match) == 2 {
			number, err := strconv.Atoi(match[1])
			return number, err == nil && number >= 0
		}
	}
	return 0, false
}

func fieldName(field map[string]any) string {
	return strings.TrimSpace(fmt.Sprint(field["name"]))
}

func validModbusMessageType(value model.MessageType) bool {
	switch value {
	case model.PropertyReport, model.EventReport, model.StateChange, model.AlarmReport, model.CommandReply, model.LogReport:
		return true
	default:
		return false
	}
}

func decodeHexPayload(payload json.RawMessage) ([]byte, error) {
	var text string
	if err := json.Unmarshal(payload, &text); err != nil {
		text = strings.Trim(string(payload), `"`)
	}
	cleaned := strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(strings.TrimSpace(text))
	data, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("invalid hex payload: %w", err)
	}
	return data, nil
}

func modbusCoilFrame(data []byte, frame string) (dataOffset, byteCountIndex, functionIndex int, err error) {
	if frame == "auto" {
		if len(data) >= 9 && data[2] == 0 && data[3] == 0 && data[7] != 0 {
			frame = "tcp"
		} else {
			frame = "rtu"
		}
	}
	if frame == "tcp" {
		if len(data) < 9 {
			return 0, 0, 0, errors.New("Modbus TCP coil response requires at least 9 bytes")
		}
		return 9, 8, 7, nil
	}
	if frame == "rtu" {
		if len(data) < 4 {
			return 0, 0, 0, errors.New("Modbus RTU coil response requires at least 4 bytes")
		}
		return 3, 2, 1, nil
	}
	return 0, 0, 0, fmt.Errorf("unsupported Modbus frame %q", frame)
}
