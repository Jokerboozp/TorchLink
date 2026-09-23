package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip" /* 执行当前语句并推进处理流程。 */
	"bytes"       /* 执行当前语句并推进处理流程。 */
	"testing"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestProtocolAssistantSpreadsheetDraftUsesGoMapping(t *testing.T) { /* 定义 TestProtocolAssistantSpreadsheetDraftUsesGoMapping 函数。 */
	draft, err := BuildProtocolAssistantSpreadsheetDraft(ProtocolAssistantInput{ /* 更新 err 的值。 */
		Name:             "库卡火花探测器",                              /* 执行当前语句并推进处理流程。 */
		Transport:        "MODBUS_TCP",                           /* 执行当前语句并推进处理流程。 */
		PayloadFormat:    "hex",                                  /* 执行当前语句并推进处理流程。 */
		DocumentFilename: "变量地址表.xlsx",                           /* 执行当前语句并推进处理流程。 */
		DocumentData:     protocolAssistantSpreadsheetFixture(t), /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if draft.ParserType != parser.ModbusCoilParserName || draft.MessageType != "PROPERTY_REPORT" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected parser/message type: %#v", draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(draft.Fields) != 2 || draft.Fields[0].CoilAddress != 100 || draft.Fields[1].CoilAddress != 3001 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected fields: %#v", draft.Fields) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if draft.Source != "" { /* 判断条件并选择处理分支。 */
		t.Fatalf("spreadsheet draft must not generate source code: %#v", draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(draft.Warnings) == 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("expected missing sample/serial context warning") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func protocolAssistantSpreadsheetFixture(t *testing.T) []byte { /* 定义 protocolAssistantSpreadsheetFixture 函数。 */
	t.Helper()                                       /* 执行当前语句并推进处理流程。 */
	var data bytes.Buffer                            /* 声明 data。 */
	zw := zip.NewWriter(&data)                       /* 更新 zw 的值。 */
	shared, err := zw.Create("xl/sharedStrings.xml") /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = shared.Write([]byte(`<sst><si><t>序号</t></si><si><t>变量名称</t></si><si><t>PLC 线圈地址</t></si><si><t>Modbus地址（十进制）</t></si><si><t>数据类型</t></si><si><t>无报出状态</t></si><si><t>报出状态</t></si><si><t>备 注</t></si><si><t>通讯心跳测试</t></si><si><t>M100</t></si><si><t>BOOL</t></si><si><t>通断循环</t></si><si><t>火花探测组1报警</t></si><si><t>M3001</t></si><si><t>持续2秒</t></si></sst>`)) /* 更新 _ 的值。 */
	sheet, err := zw.Create("xl/worksheets/sheet1.xml")                                                                                                                                                                                                                                                                                                                      /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c><c r="G1" t="s"><v>6</v></c><c r="H1" t="s"><v>7</v></c></row><row r="2"><c r="A2"><v>1</v></c><c r="B2" t="s"><v>8</v></c><c r="C2" t="s"><v>9</v></c><c r="D2"><v>100</v></c><c r="E2" t="s"><v>10</v></c><c r="F2"><v>0</v></c><c r="G2"><v>1</v></c><c r="H2" t="s"><v>11</v></c></row><row r="3"><c r="A3"><v>2</v></c><c r="B3" t="s"><v>12</v></c><c r="C3" t="s"><v>13</v></c><c r="D3"><v>3001</v></c><c r="E3" t="s"><v>10</v></c><c r="F3"><v>0</v></c><c r="G3"><v>1</v></c><c r="H3" t="s"><v>14</v></c></row></sheetData></worksheet>`)) /* 更新 _ 的值。 */
	if err := zw.Close(); err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return data.Bytes() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
