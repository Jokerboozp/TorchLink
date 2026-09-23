package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"io"       /* 执行当前语句并推进处理流程。 */
	"log/slog" /* 执行当前语句并推进处理流程。 */
	"testing"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestBoundedIngressRejectsOverload(t *testing.T) { /* 定义 TestBoundedIngressRejectsOverload 函数。 */
	c := &Client{jobs: make(chan func(), 2), stop: make(chan struct{}), log: slog.New(slog.NewTextHandler(io.Discard, nil))} /* 更新 c 的值。 */
	ran := 0                                                                                                                 /* 更新 ran 的值。 */
	for i := 0; i < 2; i++ {                                                                                                 /* 循环处理当前数据。 */
		if !c.enqueue("test", func() { ran++ }) { /* 判断条件并选择处理分支。 */
			t.Fatal("early rejection") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if c.enqueue("test", func() { t.Fatal("overload executed") }) { /* 判断条件并选择处理分支。 */
		t.Fatal("unbounded queue") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if ran != 0 || len(c.jobs) != 2 { /* 判断条件并选择处理分支。 */
		t.Fatal("callback executed inline or queue changed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	(<-c.jobs)()  /* 执行当前语句并推进处理流程。 */
	(<-c.jobs)()  /* 执行当前语句并推进处理流程。 */
	if ran != 2 { /* 判断条件并选择处理分支。 */
		t.Fatal("accepted jobs lost") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	close(c.stop)                     /* 执行当前语句并推进处理流程。 */
	if c.enqueue("test", func() {}) { /* 判断条件并选择处理分支。 */
		t.Fatal("enqueue after shutdown") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
