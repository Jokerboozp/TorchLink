package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector" /* 执行当前语句并推进处理流程。 */
	"testing"                         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestProbeLimitAndCancellation(t *testing.T) { /* 定义 TestProbeLimitAndCancellation 函数。 */
	s, _, q := fixture(t)                 /* 更新 q 的值。 */
	for i := 0; i < cap(testSlots); i++ { /* 循环处理当前数据。 */
		testSlots <- struct{}{} /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	r, e := s.Test(context.Background(), "tenant", q) /* 更新 e 的值。 */
	for i := 0; i < cap(testSlots); i++ {             /* 循环处理当前数据。 */
		<-testSlots /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if e != nil || r.Success || r.ErrorCode != "BUSY" { /* 判断条件并选择处理分支。 */
		t.Fatal(r, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	cancel()                                                /* 执行当前语句并推进处理流程。 */
	r, e = s.Test(ctx, "tenant", q)                         /* 更新 e 的值。 */
	if e == nil || r.ErrorCode != "CANCELED" {              /* 判断条件并选择处理分支。 */
		t.Fatal(r, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.Type = connector.Edge                          /* 更新 q.Type 的值。 */
	r, e = s.Test(context.Background(), "tenant", q) /* 更新 e 的值。 */
	if e == nil || r.ErrorCode != "UNSUPPORTED" {    /* 判断条件并选择处理分支。 */
		t.Fatal(r, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
