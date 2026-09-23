package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestGeneratedHexRejectsInvalidBounds(t *testing.T) { /* 定义 TestGeneratedHexRejectsInvalidBounds 函数。 */
	raw := model.RawMessage{Payload: json.RawMessage(`"01 02 03"`)} /* 更新 raw 的值。 */
	for _, config := range []map[string]any{                        /* 循环处理当前数据。 */
		{"checksum": "sum8", "checksumStartOffset": -1},                                                                    /* 执行当前语句并推进处理流程。 */
		{"checksum": "sum8", "checksumStartOffset": 9},                                                                     /* 执行当前语句并推进处理流程。 */
		{"fields": []any{map[string]any{"name": "overflow", "offset": int(^uint(0) >> 1), "length": 2, "type": "uint16"}}}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if _, err := (ConfigurableHexParser{}).ParseWithConfig(raw, config); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatal("invalid bounds accepted") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGeneratedHexRejectsUnknownChecksum(t *testing.T) { /* 定义 TestGeneratedHexRejectsUnknownChecksum 函数。 */
	// The pressure-history document requires CRC16. A mapping must not
	// silently skip that integrity requirement when the parser lacks it.
	raw := model.RawMessage{Payload: json.RawMessage(`"0146001900050a66da8098000000000023f60b"`)} /* 更新 raw 的值。 */
	for _, checksum := range []string{"crc16", "crc16-modbus", "sum16", "typo"} {                 /* 循环处理当前数据。 */
		t.Run(checksum, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			config := map[string]any{"checksum": checksum, "fields": []any{map[string]any{"name": "pressure", "offset": 13, "length": 4, "type": "int32", "endian": "big", "scale": 0.01}}} /* 更新 config 的值。 */
			if _, err := (ConfigurableHexParser{}).ParseWithConfig(raw, config); err == nil {                                                                                               /* 判断条件并选择处理分支。 */
				t.Fatal("unsupported checksum was silently ignored") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
