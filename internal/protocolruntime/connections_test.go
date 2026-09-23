package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"reflect"                     /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestConnectionCountsAcrossListeners(t *testing.T) { /* 定义 TestConnectionCountsAcrossListeners 函数。 */
	r := &Listeners{}                                                                                                 /* 更新 r 的值。 */
	var got []bool                                                                                                    /* 声明 got。 */
	r.SetConnectionReporter(func(_ context.Context, tenant, product, device string, connected bool, at int64) error { /* 执行当前语句并推进处理流程。 */
		got = append(got, connected) /* 更新 got 的值。 */
		return nil                   /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	p := model.DeviceAccessProfile{TenantID: "t", ProductID: "p"} /* 更新 p 的值。 */
	r.reportConnection(p, "d", true)                              /* 执行当前语句并推进处理流程。 */
	r.reportConnection(p, "d", true)                              /* 执行当前语句并推进处理流程。 */
	r.reportConnection(p, "d", false)                             /* 执行当前语句并推进处理流程。 */
	if !reflect.DeepEqual(got, []bool{true}) {                    /* 判断条件并选择处理分支。 */
		t.Fatal(got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	r.reportConnection(p, "d", false)                 /* 执行当前语句并推进处理流程。 */
	r.reportConnection(p, "d", false)                 /* 执行当前语句并推进处理流程。 */
	if !reflect.DeepEqual(got, []bool{true, false}) { /* 判断条件并选择处理分支。 */
		t.Fatal(got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
