package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestParseAndCompileModbusPointTable(t *testing.T) { /* 定义 TestParseAndCompileModbusPointTable 函数。 */
	csv := "标识,名称,功能码,地址,数据类型,倍率,轮询周期\n" + /* 更新 csv 的值。 */
		"temperature,温度,03,40001,int16,0.1,10\n" + /* 执行当前语句并推进处理流程。 */
		"pressure,压力,03,40002,uint16,1,10\n" + /* 执行当前语句并推进处理流程。 */
		"smoke,烟感,01,0,bool,1,5\n" /* 执行当前语句并推进处理流程。 */
	table, warnings, err := ParseModbusPointTable("points.csv", []byte(csv), 15) /* 更新 err 的值。 */
	if err != nil {                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(warnings) != 0 || len(table.Points) != 3 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected import result: points=%d warnings=%v", len(table.Points), warnings) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if table.Points[0].Address != 0 || table.Points[0].FunctionCode != 3 || table.Points[0].Scale != 0.1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("first point was not normalized: %+v", table.Points[0]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	blocks, err := CompileModbusReadBlocks(table.Points) /* 更新 err 的值。 */
	if err != nil {                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(blocks) != 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("want two function/interval blocks, got %+v", blocks) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestPointTableRejectsAmbiguousAddress(t *testing.T) { /* 定义 TestPointTableRejectsAmbiguousAddress 函数。 */
	_, _, err := ParseModbusPointTable("points.csv", []byte("名称,地址,数据类型\n温度,100,int16\n"), 10) /* 更新 err 的值。 */
	if err == nil || !strings.Contains(err.Error(), "functionCode is required") {              /* 判断条件并选择处理分支。 */
		t.Fatalf("expected explicit function-code error, got %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
