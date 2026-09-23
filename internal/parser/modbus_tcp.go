package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"math"            /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ModbusTCPParserName = "modbus_tcp_parser_v2" /* 声明 ModbusTCPParserName。 */
const ModbusTCPParserVersion = "2.0.0"             /* 声明 ModbusTCPParserVersion。 */

type ModbusTCPParser struct{} /* 定义 ModbusTCPParser 类型。 */

func (ModbusTCPParser) Name() string    { return ModbusTCPParserName }    /* 定义 Name 函数。 */
func (ModbusTCPParser) Version() string { return ModbusTCPParserVersion } /* 定义 Version 函数。 */
func (ModbusTCPParser) Match(Meta) bool { return false }                  /* 定义 Match 函数。 */
func (ModbusTCPParser) Parse(model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return nil, errors.New("modbus tcp parser requires a versioned point table") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (ModbusTCPParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	points, err := modbusTCPPoints(config["points"]) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frame, err := decodeHexPayload(raw.Payload) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(frame) < 9 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Modbus TCP response requires at least 9 bytes") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	protocolID := binary.BigEndian.Uint16(frame[2:4])    /* 更新 protocolID 的值。 */
	declared := int(binary.BigEndian.Uint16(frame[4:6])) /* 更新 declared 的值。 */
	if protocolID != 0 {                                 /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid Modbus TCP protocol id %d", protocolID) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if declared != len(frame)-6 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("Modbus TCP length is %d, received %d", declared, len(frame)-6) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	functionCode := int(frame[7]) /* 更新 functionCode 的值。 */
	if functionCode&0x80 != 0 {   /* 判断条件并选择处理分支。 */
		if len(frame) < 9 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("Modbus exception response is incomplete") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return nil, fmt.Errorf("Modbus exception code 0x%02X", frame[8]) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if functionCode < 1 || functionCode > 4 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("unsupported Modbus function code 0x%02X", functionCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	byteCount := int(frame[8])                       /* 更新 byteCount 的值。 */
	if byteCount <= 0 || 9+byteCount != len(frame) { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("Modbus byte count %d does not match response", byteCount) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	startAddress, ok := metadataInt(raw.Metadata, "startAddress") /* 更新 ok 的值。 */
	if !ok {                                                      /* 判断条件并选择处理分支。 */
		startAddress = configInt(config, "startAddress", -1) /* 更新 startAddress 的值。 */
	} /* 结束当前表达式或代码块。 */
	if startAddress < 0 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("raw metadata.startAddress is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	properties := map[string]any{}      /* 更新 properties 的值。 */
	event := map[string]any{}           /* 更新 event 的值。 */
	messageType := model.PropertyReport /* 更新 messageType 的值。 */
	for _, point := range points {      /* 循环处理当前数据。 */
		if point.FunctionCode != functionCode { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		width := point.RegisterCount /* 更新 width 的值。 */
		if functionCode <= 2 {       /* 判断条件并选择处理分支。 */
			width = 1 /* 更新 width 的值。 */
		} /* 结束当前表达式或代码块。 */
		if width <= 0 { /* 判断条件并选择处理分支。 */
			width = 1 /* 更新 width 的值。 */
		} /* 结束当前表达式或代码块。 */
		if point.Address < startAddress { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var value any          /* 声明 value。 */
		if functionCode <= 2 { /* 判断条件并选择处理分支。 */
			bitOffset := point.Address - startAddress /* 更新 bitOffset 的值。 */
			if bitOffset >= byteCount*8 {             /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			value = frame[9+bitOffset/8]&(1<<uint(bitOffset%8)) != 0 /* 更新 value 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			byteOffset := (point.Address - startAddress) * 2 /* 更新 byteOffset 的值。 */
			byteLength := width * 2                          /* 更新 byteLength 的值。 */
			if byteOffset+byteLength > byteCount {           /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			value, err = decodeModbusRegisterValue(frame[9+byteOffset:9+byteOffset+byteLength], point) /* 更新 err 的值。 */
			if err != nil {                                                                            /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("point %s: %w", point.Identifier, err) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		properties[point.Identifier] = value                               /* 更新 properties[point.Identifier] 的值。 */
		if alarm, match := modbusAlarm(point.AlarmMapping, value); match { /* 判断条件并选择处理分支。 */
			messageType = model.AlarmReport   /* 更新 messageType 的值。 */
			event["alarmType"] = alarm        /* 执行当前语句并推进处理流程。 */
			event["point"] = point.Identifier /* 执行当前语句并推进处理流程。 */
			event["value"] = value            /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(properties) == 0 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("no points matched functionCode=%d startAddress=%d", functionCode, startAddress) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType, Timestamp: raw.ReceivedAt, Properties: properties, Event: event, Tags: map[string]string{"protocolId": raw.ProtocolID, "protocolVersion": raw.ProtocolVersion, "pointTableVersion": raw.PointTableVersion}, Raw: map[string]any{"payloadFormat": "hex", "payload": strings.ToUpper(strings.Trim(string(raw.Payload), `"`)), "metadata": raw.Metadata}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func modbusTCPPoints(value any) ([]model.ModbusPoint, error) { /* 定义 modbusTCPPoints 函数。 */
	b, err := json.Marshal(value) /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var points []model.ModbusPoint                    /* 声明 points。 */
	if err = json.Unmarshal(b, &points); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("decode Modbus points: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(points) == 0 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("modbus tcp parser requires points") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return points, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func metadataInt(metadata map[string]any, key string) (int, bool) { /* 定义 metadataInt 函数。 */
	if metadata == nil { /* 判断条件并选择处理分支。 */
		return 0, false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v, ok := metadata[key] /* 更新 ok 的值。 */
	if !ok {               /* 判断条件并选择处理分支。 */
		return 0, false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch n := v.(type) { /* 根据条件选择处理路径。 */
	case int: /* 处理当前分支。 */
		return n, true /* 返回当前处理结果。 */
	case int64: /* 处理当前分支。 */
		return int(n), true /* 返回当前处理结果。 */
	case float64: /* 处理当前分支。 */
		return int(n), n == float64(int(n)) /* 返回当前处理结果。 */
	case json.Number: /* 处理当前分支。 */
		x, e := strconv.Atoi(string(n)) /* 更新 e 的值。 */
		return x, e == nil              /* 返回当前处理结果。 */
	case string: /* 处理当前分支。 */
		x, e := strconv.Atoi(strings.TrimSpace(n)) /* 更新 e 的值。 */
		return x, e == nil                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func decodeModbusRegisterValue(data []byte, p model.ModbusPoint) (any, error) { /* 定义 decodeModbusRegisterValue 函数。 */
	ordered := append([]byte(nil), data...)       /* 更新 ordered 的值。 */
	if strings.EqualFold(p.ByteOrder, "little") { /* 判断条件并选择处理分支。 */
		for i := 0; i+1 < len(ordered); i += 2 { /* 循环处理当前数据。 */
			ordered[i], ordered[i+1] = ordered[i+1], ordered[i] /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	ordered = reorderModbusWords(ordered, strings.ToUpper(strings.TrimSpace(p.WordOrder))) /* 更新 ordered 的值。 */
	var value any                                                                          /* 声明 value。 */
	switch strings.ToLower(p.DataType) {                                                   /* 根据条件选择处理路径。 */
	case "bool": /* 处理当前分支。 */
		value = binary.BigEndian.Uint16(ordered[:2]) != 0 /* 更新 value 的值。 */
	case "bits": /* 处理当前分支。 */
		value = uint64(binary.BigEndian.Uint16(ordered[:2])) /* 更新 value 的值。 */
	case "uint16": /* 处理当前分支。 */
		value = uint64(binary.BigEndian.Uint16(ordered)) /* 更新 value 的值。 */
	case "int16": /* 处理当前分支。 */
		value = int64(int16(binary.BigEndian.Uint16(ordered))) /* 更新 value 的值。 */
	case "uint32": /* 处理当前分支。 */
		value = uint64(binary.BigEndian.Uint32(ordered)) /* 更新 value 的值。 */
	case "int32": /* 处理当前分支。 */
		value = int64(int32(binary.BigEndian.Uint32(ordered))) /* 更新 value 的值。 */
	case "float32": /* 处理当前分支。 */
		value = float64(math.Float32frombits(binary.BigEndian.Uint32(ordered))) /* 更新 value 的值。 */
	case "uint64": /* 处理当前分支。 */
		value = binary.BigEndian.Uint64(ordered) /* 更新 value 的值。 */
	case "int64": /* 处理当前分支。 */
		value = int64(binary.BigEndian.Uint64(ordered)) /* 更新 value 的值。 */
	case "float64": /* 处理当前分支。 */
		value = math.Float64frombits(binary.BigEndian.Uint64(ordered)) /* 更新 value 的值。 */
	case "string": /* 处理当前分支。 */
		value = strings.TrimRight(string(ordered), "\x00 ") /* 更新 value 的值。 */
	default: /* 处理当前分支。 */
		return nil, fmt.Errorf("unsupported dataType %q", p.DataType) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.Bit != nil { /* 判断条件并选择处理分支。 */
		raw, ok := numericUint64(value) /* 更新 ok 的值。 */
		if !ok {                        /* 判断条件并选择处理分支。 */
			return nil, errors.New("bit extraction requires an integer data type") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return raw&(1<<uint(*p.Bit)) != 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if number, ok := numericFloat64(value); ok { /* 判断条件并选择处理分支。 */
		scale := p.Scale /* 更新 scale 的值。 */
		if scale == 0 {  /* 判断条件并选择处理分支。 */
			scale = 1 /* 更新 scale 的值。 */
		} /* 结束当前表达式或代码块。 */
		return number*scale + p.Offset, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return value, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func reorderModbusWords(data []byte, order string) []byte { /* 定义 reorderModbusWords 函数。 */
	if len(data) == 4 { /* 判断条件并选择处理分支。 */
		switch order { /* 根据条件选择处理路径。 */
		case "CDAB": /* 处理当前分支。 */
			return []byte{data[2], data[3], data[0], data[1]} /* 返回当前处理结果。 */
		case "BADC": /* 处理当前分支。 */
			return []byte{data[1], data[0], data[3], data[2]} /* 返回当前处理结果。 */
		case "DCBA": /* 处理当前分支。 */
			return []byte{data[3], data[2], data[1], data[0]} /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) == 8 { /* 判断条件并选择处理分支。 */
		switch order { /* 根据条件选择处理路径。 */
		case "GHEFCDAB": /* 处理当前分支。 */
			return []byte{data[6], data[7], data[4], data[5], data[2], data[3], data[0], data[1]} /* 返回当前处理结果。 */
		case "BADCFEHG": /* 处理当前分支。 */
			return []byte{data[1], data[0], data[3], data[2], data[5], data[4], data[7], data[6]} /* 返回当前处理结果。 */
		case "HGFEDCBA": /* 处理当前分支。 */
			return []byte{data[7], data[6], data[5], data[4], data[3], data[2], data[1], data[0]} /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return data /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func numericUint64(v any) (uint64, bool) { /* 定义 numericUint64 函数。 */
	switch n := v.(type) { /* 根据条件选择处理路径。 */
	case uint64: /* 处理当前分支。 */
		return n, true /* 返回当前处理结果。 */
	case int64: /* 处理当前分支。 */
		return uint64(n), n >= 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func numericFloat64(v any) (float64, bool) { /* 定义 numericFloat64 函数。 */
	switch n := v.(type) { /* 根据条件选择处理路径。 */
	case uint64: /* 处理当前分支。 */
		return float64(n), true /* 返回当前处理结果。 */
	case int64: /* 处理当前分支。 */
		return float64(n), true /* 返回当前处理结果。 */
	case float64: /* 处理当前分支。 */
		return n, true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func modbusAlarm(mapping map[string]any, value any) (string, bool) { /* 定义 modbusAlarm 函数。 */
	if len(mapping) == 0 { /* 判断条件并选择处理分支。 */
		return "", false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	keys := []string{fmt.Sprint(value)} /* 更新 keys 的值。 */
	if b, ok := value.(bool); ok {      /* 判断条件并选择处理分支。 */
		if b { /* 判断条件并选择处理分支。 */
			keys = append(keys, "1") /* 更新 keys 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			keys = append(keys, "0") /* 更新 keys 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, key := range keys { /* 循环处理当前数据。 */
		if alarm, ok := mapping[key]; ok { /* 判断条件并选择处理分支。 */
			return fmt.Sprint(alarm), true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "", false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
