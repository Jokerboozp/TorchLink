package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// ModbusCoilParserName is a reviewed Go parser for a Modbus read-coils
// response. It is intentionally declarative: the assistant stores addresses
// and field metadata in Config, while the runtime conversion stays in Go.
const ModbusCoilParserName = "modbus_coil_parser" /* 声明 ModbusCoilParserName。 */

const ModbusCoilParserVersion = "1.0.0" /* 声明 ModbusCoilParserVersion。 */

type ModbusCoilParser struct{} /* 定义 ModbusCoilParser 类型。 */

func (ModbusCoilParser) Name() string    { return ModbusCoilParserName }    /* 定义 Name 函数。 */
func (ModbusCoilParser) Version() string { return ModbusCoilParserVersion } /* 定义 Version 函数。 */
func (ModbusCoilParser) Match(Meta) bool { return false }                   /* 定义 Match 函数。 */

func (p ModbusCoilParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return nil, errors.New("modbus coil parser requires a mapping configuration") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (p ModbusCoilParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	fields, err := validateModbusCoilConfig(config) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	data, err := decodeHexPayload(raw.Payload) /* 更新 err 的值。 */
	if err != nil {                            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frame := strings.ToLower(strings.TrimSpace(firstConfig(config, "frame"))) /* 更新 frame 的值。 */
	if frame == "" {                                                          /* 判断条件并选择处理分支。 */
		frame = "auto" /* 更新 frame 的值。 */
	} /* 结束当前表达式或代码块。 */
	dataOffset, byteCountIndex, functionIndex, err := modbusCoilFrame(data, frame) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	functionCode := configInt(config, "functionCode", 1) /* 更新 functionCode 的值。 */
	if functionCode <= 0 || functionCode > 0xff {        /* 判断条件并选择处理分支。 */
		return nil, errors.New("modbus coil parser functionCode must be between 1 and 255") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[functionIndex]&0x80 != 0 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("modbus exception response function code 0x%02X", data[functionIndex]) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if int(data[functionIndex]) != functionCode { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("unexpected Modbus function code 0x%02X, want 0x%02X", data[functionIndex], functionCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	byteCount := int(data[byteCountIndex])                  /* 更新 byteCount 的值。 */
	if byteCount <= 0 || dataOffset+byteCount > len(data) { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("modbus coil byte count %d exceeds payload", byteCount) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	startAddress := configInt(config, "startAddress", 0) /* 更新 startAddress 的值。 */
	properties := make(map[string]any, len(fields))      /* 更新 properties 的值。 */
	for _, field := range fields {                       /* 循环处理当前数据。 */
		address := modbusCoilFieldAddress(field) /* 更新 address 的值。 */
		bitOffset := address - startAddress      /* 更新 bitOffset 的值。 */
		if bitOffset < 0 {                       /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("field %q address %d is before startAddress %d", fieldName(field), address, startAddress) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		byteOffset := bitOffset / 8  /* 更新 byteOffset 的值。 */
		if byteOffset >= byteCount { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("field %q address %d is outside the response", fieldName(field), address) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		properties[fieldName(field)] = data[dataOffset+byteOffset]&(1<<uint(bitOffset%8)) != 0 /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	messageType := model.MessageType(strings.ToUpper(firstConfig(config, "messageType"))) /* 更新 messageType 的值。 */
	if messageType == "" {                                                                /* 判断条件并选择处理分支。 */
		messageType = model.PropertyReport /* 更新 messageType 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &model.StandardMessage{ /* 返回当前处理结果。 */
		MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, /* 执行当前语句并推进处理流程。 */
		TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: messageType, /* 执行当前语句并推进处理流程。 */
		Timestamp: raw.ReceivedAt, Properties: properties, Event: map[string]any{}, Tags: map[string]string{}, /* 执行当前语句并推进处理流程。 */
		Raw: map[string]any{"payloadFormat": "hex", "payload": strings.ToUpper(strings.Trim(string(raw.Payload), `"`)), "mappingConfig": config}, /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// ValidateModbusCoilConfig is used by the HTTP layer before a package is
// saved. It does not need a sample payload, so a user can save an address map
// first and attach/verify a real response later.
func ValidateModbusCoilConfig(config map[string]any) error { /* 定义 ValidateModbusCoilConfig 函数。 */
	_, err := validateModbusCoilConfig(config) /* 更新 err 的值。 */
	return err                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validateModbusCoilConfig(config map[string]any) ([]map[string]any, error) { /* 定义 validateModbusCoilConfig 函数。 */
	if config == nil { /* 判断条件并选择处理分支。 */
		return nil, errors.New("modbus coil parser configuration is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	messageType := model.MessageType(strings.ToUpper(firstConfig(config, "messageType"))) /* 更新 messageType 的值。 */
	if messageType != "" && !validModbusMessageType(messageType) {                        /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("unsupported messageType %q", messageType) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frame := strings.ToLower(strings.TrimSpace(firstConfig(config, "frame"))) /* 更新 frame 的值。 */
	if frame != "" && frame != "auto" && frame != "tcp" && frame != "rtu" {   /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("unsupported Modbus frame %q", frame) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	startAddress := configInt(config, "startAddress", 0) /* 更新 startAddress 的值。 */
	if startAddress < 0 {                                /* 判断条件并选择处理分支。 */
		return nil, errors.New("modbus coil parser startAddress must not be negative") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	fields, err := modbusCoilFields(config["fields"]) /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	seen := map[string]struct{}{}      /* 更新 seen 的值。 */
	for index, field := range fields { /* 循环处理当前数据。 */
		name := fieldName(field) /* 更新 name 的值。 */
		if name == "" {          /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("fields[%d] name is required", index) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, exists := seen[name]; exists { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("duplicate Modbus coil field %q", name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[name] = struct{}{}                /* 更新 seen[name] 的值。 */
		address, ok := parseCoilAddress(field) /* 更新 ok 的值。 */
		if !ok || address < startAddress {     /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("fields[%d] coilAddress is invalid", index) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return fields, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func modbusCoilFields(value any) ([]map[string]any, error) { /* 定义 modbusCoilFields 函数。 */
	switch values := value.(type) { /* 根据条件选择处理路径。 */
	case []any: /* 处理当前分支。 */
		fields := make([]map[string]any, 0, len(values)) /* 更新 fields 的值。 */
		for index, value := range values {               /* 循环处理当前数据。 */
			field, ok := value.(map[string]any) /* 更新 ok 的值。 */
			if !ok {                            /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("fields[%d] must be an object", index) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			fields = append(fields, field) /* 更新 fields 的值。 */
		} /* 结束当前表达式或代码块。 */
		return fields, nil /* 返回当前处理结果。 */
	case []map[string]any: /* 处理当前分支。 */
		return values, nil /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return nil, errors.New("modbus coil parser requires a fields array") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func modbusCoilFieldAddress(field map[string]any) int { /* 定义 modbusCoilFieldAddress 函数。 */
	address, _ := parseCoilAddress(field) /* 更新 _ 的值。 */
	return address                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var coilAddressPattern = regexp.MustCompile(`(?i)^[A-Z]*\s*(\d+)$`) /* 声明 coilAddressPattern。 */

func parseCoilAddress(field map[string]any) (int, bool) { /* 定义 parseCoilAddress 函数。 */
	for _, key := range []string{"coilAddress", "address", "modbusAddress"} { /* 循环处理当前数据。 */
		value, exists := field[key]  /* 更新 exists 的值。 */
		if !exists || value == nil { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if number, ok := value.(float64); ok { /* 判断条件并选择处理分支。 */
			return int(number), number >= 0 && number == float64(int(number)) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if number, ok := value.(int); ok { /* 判断条件并选择处理分支。 */
			return number, number >= 0 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		text := strings.TrimSpace(fmt.Sprint(value))         /* 更新 text 的值。 */
		match := coilAddressPattern.FindStringSubmatch(text) /* 更新 match 的值。 */
		if len(match) == 2 {                                 /* 判断条件并选择处理分支。 */
			number, err := strconv.Atoi(match[1])    /* 更新 err 的值。 */
			return number, err == nil && number >= 0 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func fieldName(field map[string]any) string { /* 定义 fieldName 函数。 */
	return strings.TrimSpace(fmt.Sprint(field["name"])) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validModbusMessageType(value model.MessageType) bool { /* 定义 validModbusMessageType 函数。 */
	switch value { /* 根据条件选择处理路径。 */
	case model.PropertyReport, model.EventReport, model.StateChange, model.AlarmReport, model.CommandReply, model.LogReport: /* 处理当前分支。 */
		return true /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func decodeHexPayload(payload json.RawMessage) ([]byte, error) { /* 定义 decodeHexPayload 函数。 */
	var text string                                        /* 声明 text。 */
	if err := json.Unmarshal(payload, &text); err != nil { /* 判断条件并选择处理分支。 */
		text = strings.Trim(string(payload), `"`) /* 更新 text 的值。 */
	} /* 结束当前表达式或代码块。 */
	cleaned := strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(strings.TrimSpace(text)) /* 更新 cleaned 的值。 */
	data, err := hex.DecodeString(cleaned)                                                                 /* 更新 err 的值。 */
	if err != nil {                                                                                        /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid hex payload: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return data, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func modbusCoilFrame(data []byte, frame string) (dataOffset, byteCountIndex, functionIndex int, err error) { /* 定义 modbusCoilFrame 函数。 */
	if frame == "auto" { /* 判断条件并选择处理分支。 */
		if len(data) >= 9 && data[2] == 0 && data[3] == 0 && data[7] != 0 { /* 判断条件并选择处理分支。 */
			frame = "tcp" /* 更新 frame 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			frame = "rtu" /* 更新 frame 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if frame == "tcp" { /* 判断条件并选择处理分支。 */
		if len(data) < 9 { /* 判断条件并选择处理分支。 */
			return 0, 0, 0, errors.New("Modbus TCP coil response requires at least 9 bytes") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return 9, 8, 7, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if frame == "rtu" { /* 判断条件并选择处理分支。 */
		if len(data) < 4 { /* 判断条件并选择处理分支。 */
			return 0, 0, 0, errors.New("Modbus RTU coil response requires at least 4 bytes") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return 3, 2, 1, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0, 0, 0, fmt.Errorf("unsupported Modbus frame %q", frame) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
