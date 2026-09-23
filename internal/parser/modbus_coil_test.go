package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestModbusCoilParserTCPResponse(t *testing.T) { /* 定义 TestModbusCoilParserTCPResponse 函数。 */
	message, err := (ModbusCoilParser{}).ParseWithConfig(model.RawMessage{ /* 更新 err 的值。 */
		MessageID: "raw_coil", TenantID: "tenant", ProductID: "product", DeviceID: "device", ReceivedAt: 123, /* 执行当前语句并推进处理流程。 */
		Protocol: "modbus", PayloadFormat: "hex", Payload: json.RawMessage(`"00 01 00 00 00 05 01 01 02 03 01"`), /* 执行当前语句并推进处理流程。 */
	}, map[string]any{ /* 结束当前表达式或代码块。 */
		"frame": "tcp", "startAddress": 0, "messageType": "PROPERTY_REPORT", /* 执行当前语句并推进处理流程。 */
		"fields": []any{ /* 执行当前语句并推进处理流程。 */
			map[string]any{"name": "coil_0", "coilAddress": 0}, /* 执行当前语句并推进处理流程。 */
			map[string]any{"name": "coil_1", "coilAddress": 1}, /* 执行当前语句并推进处理流程。 */
			map[string]any{"name": "coil_8", "coilAddress": 8}, /* 执行当前语句并推进处理流程。 */
			map[string]any{"name": "coil_9", "coilAddress": 9}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.Properties["coil_0"] != true || message.Properties["coil_1"] != true || message.Properties["coil_8"] != true || message.Properties["coil_9"] != false { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected coil properties: %#v", message.Properties) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.Parser != "" || message.MessageType != model.PropertyReport { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected standard message: %#v", message) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestModbusCoilParserRejectsMissingMapping(t *testing.T) { /* 定义 TestModbusCoilParserRejectsMissingMapping 函数。 */
	if err := ValidateModbusCoilConfig(map[string]any{}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected fields validation error") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestModbusCoilParserRTUResponse(t *testing.T) { /* 定义 TestModbusCoilParserRTUResponse 函数。 */
	message, err := (ModbusCoilParser{}).ParseWithConfig(model.RawMessage{ /* 更新 err 的值。 */
		MessageID: "raw_rtu", Protocol: "modbus", PayloadFormat: "hex", Payload: json.RawMessage(`"01 01 01 01"`), /* 执行当前语句并推进处理流程。 */
	}, map[string]any{"frame": "rtu", "startAddress": 0, "fields": []any{map[string]any{"name": "coil_0", "coilAddress": 0}}}) /* 结束当前表达式或代码块。 */
	if err != nil || message.Properties["coil_0"] != true { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected RTU response message=%#v err=%v", message, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
