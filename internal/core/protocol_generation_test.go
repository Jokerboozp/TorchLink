package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"
	"testing" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestGenerateMessageWithoutAI(t *testing.T) { /* 定义 TestGenerateMessageWithoutAI 函数。 */
	engine := &Engine{}                                                                                                                                                                                                                                                                      /* 更新 engine 的值。 */
	draft, err := engine.GenerateProtocolAssistant(aitest.Context(context.Background()), "tenant", ProtocolAssistantInput{InputKind: "sample", DocumentText: `{"messageType":"ALARM_REPORT","data":{"smoke":true},"event":{"alarmType":"FIRE"}}`, Transport: "HTTP", PayloadFormat: "json"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if draft.Preview.MessageType != model.AlarmReport || draft.Preview.Event["alarmType"] != "FIRE" { /* 判断条件并选择处理分支。 */
		t.Fatal(draft.Preview) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	next, err := PreviewProtocolAssistant(draft, "tenant", `{"messageType":"PROPERTY_REPORT","data":{"smoke":false}}`) /* 更新 err 的值。 */
	if err != nil || next.Properties["smoke"] != false || next.MessageType != model.PropertyReport {                   /* 判断条件并选择处理分支。 */
		t.Fatalf("mapping hardcodes the sample: %v %v", next, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, err = engine.GenerateProtocolAssistant(context.Background(), "tenant", ProtocolAssistantInput{InputKind: "sample", SamplePayload: `{invalid`, PayloadFormat: "json"}) /* 更新 err 的值。 */
	if !errors.Is(err, ErrProtocolInput) {                                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestGeneratedModbusPreservesExplicitZeroBasedAddress(t *testing.T) { /* 定义 TestGeneratedModbusPreservesExplicitZeroBasedAddress 函数。 */
	in := ProtocolAssistantInput{InputKind: "point-table", Transport: "MODBUS_TCP", DocumentFilename: "points.csv", DocumentData: []byte("identifier,name,functionCode,address,addressNotation,dataType\nx,X,3,40001,zero_based,uint16\n")} /* 更新 in 的值。 */
	draft, _, err := buildUploadedProtocol(in)                                                                                                                                                                                              /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	points := draft.Config["points"].([]model.ModbusPoint) /* 更新 points 的值。 */
	if points[0].Address != 40001 {                        /* 判断条件并选择处理分支。 */
		t.Fatal(points) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = NormalizeGeneratedModbusConfig(draft.Config); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if draft.Config["points"].([]model.ModbusPoint)[0].Address != 40001 { /* 判断条件并选择处理分支。 */
		t.Fatal("normalization changed address") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	points[0].RegisterCount = 1                                         /* 更新 points[0].RegisterCount 的值。 */
	points[0].DataType = "uint64"                                       /* 更新 points[0].DataType 的值。 */
	draft.Config["points"] = points                                     /* 执行当前语句并推进处理流程。 */
	if err = NormalizeGeneratedModbusConfig(draft.Config); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("unsafe field width accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGeneratedCabinetBitPointPreview(t *testing.T) { /* 定义 TestGeneratedCabinetBitPointPreview 函数。 */
	in := ProtocolAssistantInput{InputKind: "point-table", Transport: "MODBUS_RTU", DocumentFilename: "cabinet.csv", DocumentData: []byte("identifier,name,functionCode,address,addressNotation,dataType,bit\ninput4,输入4,3,8195,zero_based,bits,3\n")} /* 更新 in 的值。 */
	draft, _, err := buildUploadedProtocol(in)                                                                                                                                                                                                         /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = NormalizeGeneratedModbusConfig(draft.Config); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	point := draft.Config["points"].([]model.ModbusPoint)[0]                                      /* 更新 point 的值。 */
	if point.DataType != "bits" || point.Bit == nil || *point.Bit != 3 || point.Address != 8195 { /* 判断条件并选择处理分支。 */
		t.Fatal("point import changed bit definition") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	draft.Config["startAddress"] = 8195 /* 执行当前语句并推进处理流程。 */
	// Unit 1, function 3, one register 0x0008, CRC 0x82B9 (low byte first).
	preview, err := PreviewProtocolAssistant(draft, "tenant", "01 03 02 00 08 B9 82") /* 更新 err 的值。 */
	if err != nil {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if preview.Properties["input4"] != true { /* 判断条件并选择处理分支。 */
		t.Fatalf("input4 = %v", preview.Properties["input4"]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

type hexMappingAI struct{ protocolAssistantAI } /* 定义 hexMappingAI 类型。 */

func (hexMappingAI) GenerateJSON(context.Context, string, string, string) (string, error) { /* 定义 GenerateJSON 函数。 */
	return `{"name":"HEX temperature","protocol":"temp","transport":"MQTT","payloadFormat":"hex","parserType":"configurable_hex_parser","messageType":"PROPERTY_REPORT","config":{"startHex":"AA","fields":[{"name":"temperature","offset":1,"length":2,"type":"uint16","endian":"big","scale":0.1}]},"fields":[{"name":"temperature"}]}`, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func TestGenerateFixedHexMappingAndPreview(t *testing.T) { /* 定义 TestGenerateFixedHexMappingAndPreview 函数。 */
	engine := &Engine{AIWorkflows: &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) {
		return hexMappingAI{}.GenerateJSON(context.Background(), "", "", "")
	}}, HarnessTokens: aitest.Tokens(), Repo: memory.NewRepository()}
	draft, err := engine.GenerateProtocolAssistant(aitest.Context(context.Background()), "tenant", ProtocolAssistantInput{InputKind: "sample", PayloadFormat: "hex", SamplePayload: "AA 00 FA", PointTable: "temperature starts at byte 1, uint16 big endian, scale 0.1"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if draft.Preview == nil || draft.Preview.Properties["temperature"] != float64(25) { /* 判断条件并选择处理分支。 */
		t.Fatal(draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	next, err := PreviewProtocolAssistant(draft, "tenant", "AA 01 2C") /* 更新 err 的值。 */
	if err != nil || next.Properties["temperature"] != float64(30) {   /* 判断条件并选择处理分支。 */
		t.Fatalf("%v %v", next, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
