package parser

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

const ModbusTCPParserName = "modbus_tcp_parser_v2"
const ModbusTCPParserVersion = "2.0.0"

type ModbusTCPParser struct{}

func (ModbusTCPParser) Name() string    { return ModbusTCPParserName }
func (ModbusTCPParser) Version() string { return ModbusTCPParserVersion }
func (ModbusTCPParser) Match(Meta) bool { return false }
func (ModbusTCPParser) Parse(model.RawMessage) (*model.StandardMessage, error) {
	return nil, errors.New("modbus tcp parser requires a versioned point table")
}

func (ModbusTCPParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	points, err := modbusTCPPoints(config["points"])
	if err != nil {
		return nil, err
	}
	frame, err := decodeHexPayload(raw.Payload)
	if err != nil {
		return nil, err
	}
	if len(frame) < 9 {
		return nil, errors.New("Modbus TCP response requires at least 9 bytes")
	}
	protocolID := binary.BigEndian.Uint16(frame[2:4])
	declared := int(binary.BigEndian.Uint16(frame[4:6]))
	if protocolID != 0 {
		return nil, fmt.Errorf("invalid Modbus TCP protocol id %d", protocolID)
	}
	if declared != len(frame)-6 {
		return nil, fmt.Errorf("Modbus TCP length is %d, received %d", declared, len(frame)-6)
	}
	functionCode := int(frame[7])
	if functionCode&0x80 != 0 {
		if len(frame) < 9 {
			return nil, errors.New("Modbus exception response is incomplete")
		}
		return nil, fmt.Errorf("Modbus exception code 0x%02X", frame[8])
	}
	if functionCode < 1 || functionCode > 4 {
		return nil, fmt.Errorf("unsupported Modbus function code 0x%02X", functionCode)
	}
	byteCount := int(frame[8])
	if byteCount <= 0 || 9+byteCount != len(frame) {
		return nil, fmt.Errorf("Modbus byte count %d does not match response", byteCount)
	}
	startAddress, ok := metadataInt(raw.Metadata, "startAddress")
	if !ok {
		startAddress = configInt(config, "startAddress", -1)
	}
	if startAddress < 0 {
		return nil, errors.New("raw metadata.startAddress is required")
	}
	properties := map[string]any{}
	event := map[string]any{}
	messageType := model.PropertyReport
	for _, point := range points {
		if point.FunctionCode != functionCode {
			continue
		}
		width := point.RegisterCount
		if functionCode <= 2 {
			width = 1
		}
		if width <= 0 {
			width = 1
		}
		if point.Address < startAddress {
			continue
		}
		var value any
		if functionCode <= 2 {
			bitOffset := point.Address - startAddress
			if bitOffset >= byteCount*8 {
				continue
			}
			value = frame[9+bitOffset/8]&(1<<uint(bitOffset%8)) != 0
		} else {
			byteOffset := (point.Address - startAddress) * 2
			byteLength := width * 2
			if byteOffset+byteLength > byteCount {
				continue
			}
			value, err = decodeModbusRegisterValue(frame[9+byteOffset:9+byteOffset+byteLength], point)
			if err != nil {
				return nil, fmt.Errorf("point %s: %w", point.Identifier, err)
			}
		}
		properties[point.Identifier] = value
		if alarm, match := modbusAlarm(point.AlarmMapping, value); match {
			messageType = model.AlarmReport
			event["alarmType"] = alarm
			event["point"] = point.Identifier
			event["value"] = value
		}
	}
	if len(properties) == 0 {
		return nil, fmt.Errorf("no points matched functionCode=%d startAddress=%d", functionCode, startAddress)
	}
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType, Timestamp: raw.ReceivedAt, Properties: properties, Event: event, Tags: map[string]string{"protocolId": raw.ProtocolID, "protocolVersion": raw.ProtocolVersion, "pointTableVersion": raw.PointTableVersion}, Raw: map[string]any{"payloadFormat": "hex", "payload": strings.ToUpper(strings.Trim(string(raw.Payload), `"`)), "metadata": raw.Metadata}}, nil
}

func modbusTCPPoints(value any) ([]model.ModbusPoint, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var points []model.ModbusPoint
	if err = json.Unmarshal(b, &points); err != nil {
		return nil, fmt.Errorf("decode Modbus points: %w", err)
	}
	if len(points) == 0 {
		return nil, errors.New("modbus tcp parser requires points")
	}
	return points, nil
}
func metadataInt(metadata map[string]any, key string) (int, bool) {
	if metadata == nil {
		return 0, false
	}
	v, ok := metadata[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), n == float64(int(n))
	case json.Number:
		x, e := strconv.Atoi(string(n))
		return x, e == nil
	case string:
		x, e := strconv.Atoi(strings.TrimSpace(n))
		return x, e == nil
	}
	return 0, false
}
func decodeModbusRegisterValue(data []byte, p model.ModbusPoint) (any, error) {
	ordered := append([]byte(nil), data...)
	if strings.EqualFold(p.ByteOrder, "little") {
		for i := 0; i+1 < len(ordered); i += 2 {
			ordered[i], ordered[i+1] = ordered[i+1], ordered[i]
		}
	}
	ordered = reorderModbusWords(ordered, strings.ToUpper(strings.TrimSpace(p.WordOrder)))
	var value any
	switch strings.ToLower(p.DataType) {
	case "bool":
		value = binary.BigEndian.Uint16(ordered[:2]) != 0
	case "bits":
		value = uint64(binary.BigEndian.Uint16(ordered[:2]))
	case "uint16":
		value = uint64(binary.BigEndian.Uint16(ordered))
	case "int16":
		value = int64(int16(binary.BigEndian.Uint16(ordered)))
	case "uint32":
		value = uint64(binary.BigEndian.Uint32(ordered))
	case "int32":
		value = int64(int32(binary.BigEndian.Uint32(ordered)))
	case "float32":
		value = float64(math.Float32frombits(binary.BigEndian.Uint32(ordered)))
	case "uint64":
		value = binary.BigEndian.Uint64(ordered)
	case "int64":
		value = int64(binary.BigEndian.Uint64(ordered))
	case "float64":
		value = math.Float64frombits(binary.BigEndian.Uint64(ordered))
	case "string":
		value = strings.TrimRight(string(ordered), "\x00 ")
	default:
		return nil, fmt.Errorf("unsupported dataType %q", p.DataType)
	}
	if p.Bit != nil {
		raw, ok := numericUint64(value)
		if !ok {
			return nil, errors.New("bit extraction requires an integer data type")
		}
		return raw&(1<<uint(*p.Bit)) != 0, nil
	}
	if number, ok := numericFloat64(value); ok {
		scale := p.Scale
		if scale == 0 {
			scale = 1
		}
		return number*scale + p.Offset, nil
	}
	return value, nil
}
func reorderModbusWords(data []byte, order string) []byte {
	if len(data) == 4 {
		switch order {
		case "CDAB":
			return []byte{data[2], data[3], data[0], data[1]}
		case "BADC":
			return []byte{data[1], data[0], data[3], data[2]}
		case "DCBA":
			return []byte{data[3], data[2], data[1], data[0]}
		}
	}
	if len(data) == 8 {
		switch order {
		case "GHEFCDAB":
			return []byte{data[6], data[7], data[4], data[5], data[2], data[3], data[0], data[1]}
		case "BADCFEHG":
			return []byte{data[1], data[0], data[3], data[2], data[5], data[4], data[7], data[6]}
		case "HGFEDCBA":
			return []byte{data[7], data[6], data[5], data[4], data[3], data[2], data[1], data[0]}
		}
	}
	return data
}
func numericUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case int64:
		return uint64(n), n >= 0
	}
	return 0, false
}
func numericFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case uint64:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}
func modbusAlarm(mapping map[string]any, value any) (string, bool) {
	if len(mapping) == 0 {
		return "", false
	}
	keys := []string{fmt.Sprint(value)}
	if b, ok := value.(bool); ok {
		if b {
			keys = append(keys, "1")
		} else {
			keys = append(keys, "0")
		}
	}
	for _, key := range keys {
		if alarm, ok := mapping[key]; ok {
			return fmt.Sprint(alarm), true
		}
	}
	return "", false
}
