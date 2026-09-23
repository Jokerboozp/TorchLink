package modbusframe /* 声明 modbusframe 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// CRC is the Modbus RTU CRC16; the wire order is low byte first.
func CRC(data []byte) uint16 { /* 定义 CRC 函数。 */
	v := uint16(0xffff)      /* 更新 v 的值。 */
	for _, b := range data { /* 循环处理当前数据。 */
		v ^= uint16(b)           /* 执行当前语句并推进处理流程。 */
		for i := 0; i < 8; i++ { /* 循环处理当前数据。 */
			if v&1 != 0 { /* 判断条件并选择处理分支。 */
				v = v>>1 ^ 0xa001 /* 更新 v 的值。 */
			} else { /* 结束当前表达式或代码块。 */
				v >>= 1 /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func AppendCRC(data []byte) []byte { /* 定义 AppendCRC 函数。 */
	v := CRC(data)                           /* 更新 v 的值。 */
	return append(data, byte(v), byte(v>>8)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func Validate(data []byte) error { /* 定义 Validate 函数。 */
	if len(data) < 5 || len(data) > 256 { /* 判断条件并选择处理分支。 */
		return errors.New("invalid Modbus RTU frame length") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if CRC(data[:len(data)-2]) != binary.LittleEndian.Uint16(data[len(data)-2:]) { /* 判断条件并选择处理分支。 */
		return errors.New("Modbus RTU CRC mismatch") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
