package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary"                   /* 执行当前语句并推进处理流程。 */
	"encoding/hex"                      /* 执行当前语句并推进处理流程。 */
	"encoding/json"                     /* 执行当前语句并推进处理流程。 */
	"errors"                            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/modbusframe" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ModbusRTUParserName = "modbus_rtu_parser_v2" /* 声明 ModbusRTUParserName。 */

type ModbusRTUParser struct{} /* 定义 ModbusRTUParser 类型。 */

func (ModbusRTUParser) Name() string    { return ModbusRTUParserName }    /* 定义 Name 函数。 */
func (ModbusRTUParser) Version() string { return ModbusTCPParserVersion } /* 定义 Version 函数。 */
func (ModbusRTUParser) Match(Meta) bool { return false }                  /* 定义 Match 函数。 */
func (ModbusRTUParser) Parse(model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return nil, errors.New("Modbus RTU requires a versioned point table") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (ModbusRTUParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	frame, err := decodeHexPayload(raw.Payload) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = modbusframe.Validate(frame); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Reuse point decoding for the common PDU. The archive and returned raw
	// evidence remain the original RTU frame, including its CRC.
	header := make([]byte, 6)                                                                      /* 更新 header 的值。 */
	binary.BigEndian.PutUint16(header[4:], uint16(len(frame)-2))                                   /* 执行当前语句并推进处理流程。 */
	copyRaw := raw                                                                                 /* 更新 copyRaw 的值。 */
	copyRaw.Payload, _ = json.Marshal(hex.EncodeToString(append(header, frame[:len(frame)-2]...))) /* 更新 _ 的值。 */
	message, err := (ModbusTCPParser{}).ParseWithConfig(copyRaw, config)                           /* 更新 err 的值。 */
	if err == nil {                                                                                /* 判断条件并选择处理分支。 */
		message.Raw["payload"] = string(raw.Payload) /* 执行当前语句并推进处理流程。 */
		message.Raw["transport"] = "MODBUS_RTU"      /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return message, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
