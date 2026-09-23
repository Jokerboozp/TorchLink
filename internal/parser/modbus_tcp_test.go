package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestModbusTCPParserWithVersionedPoints(t *testing.T) { /* 定义 TestModbusTCPParserWithVersionedPoints 函数。 */
	payload, _ := json.Marshal("00010000000701030400FA0001") /* 更新 _ 的值。 */
	bit := 0                                                 /* 更新 bit 的值。 */
	points := []model.ModbusPoint{                           /* 更新 points 的值。 */
		{Identifier: "temperature", FunctionCode: 3, Address: 100, DataType: "int16", RegisterCount: 1, ByteOrder: "big", WordOrder: "ABCD", Scale: 0.1},       /* 执行当前语句并推进处理流程。 */
		{Identifier: "running", FunctionCode: 3, Address: 101, DataType: "uint16", RegisterCount: 1, ByteOrder: "big", WordOrder: "ABCD", Scale: 1, Bit: &bit}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	message, err := (ModbusTCPParser{}).ParseWithConfig(model.RawMessage{MessageID: "raw_1", TenantID: "t", ProductID: "p", DeviceID: "d", ReceivedAt: 1, ProtocolID: "modbus", ProtocolVersion: "2.0.0", PointTableVersion: "2.0.0", Payload: payload, Metadata: map[string]any{"startAddress": 100}}, map[string]any{"points": points}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.Properties["temperature"] != float64(25) || message.Properties["running"] != true { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected properties: %#v", message.Properties) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.Tags["protocolVersion"] != "2.0.0" { /* 判断条件并选择处理分支。 */
		t.Fatalf("release trace was lost: %#v", message.Tags) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestModbusTCPParserRejectsException(t *testing.T) { /* 定义 TestModbusTCPParserRejectsException 函数。 */
	payload, _ := json.Marshal("000100000003018302")                                                                                                                                                     /* 更新 _ 的值。 */
	_, err := (ModbusTCPParser{}).ParseWithConfig(model.RawMessage{Payload: payload}, map[string]any{"points": []model.ModbusPoint{{Identifier: "x", FunctionCode: 3, Address: 0, DataType: "uint16"}}}) /* 更新 err 的值。 */
	if err == nil {                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal("expected Modbus exception to fail parsing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
