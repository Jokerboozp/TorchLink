package model /* 声明 model 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestObservedCommandTimeoutPreservesTerminalReply(t *testing.T) { /* 定义 TestObservedCommandTimeoutPreservesTerminalReply 函数。 */
	for _, status := range []string{"SENT", "DISPATCHING"} { /* 循环处理当前数据。 */
		c := DeviceCommand{Status: status, CreatedAt: 1000}                                            /* 更新 c 的值。 */
		if c.ObservedOutcome(30999).Status != status || c.ObservedOutcome(31000).Status != "UNKNOWN" { /* 判断条件并选择处理分支。 */
			t.Fatal("incorrect wait boundary") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if c.Status != status { /* 判断条件并选择处理分支。 */
			t.Fatal("query mutated stored outcome") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, status := range []string{"SUCCEEDED", "FAILED"} { /* 循环处理当前数据。 */
		if (DeviceCommand{Status: status, CreatedAt: 1000}).ObservedOutcome(40000).Status != status { /* 判断条件并选择处理分支。 */
			t.Fatal("terminal outcome lost") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestLegacyQueuedCommandIsUnknownAndHidesExecutionToken(t *testing.T) { /* 定义 TestLegacyQueuedCommandIsUnknownAndHidesExecutionToken 函数。 */
	var c DeviceCommand                                                                                                                                    /* 声明 c。 */
	if err := json.Unmarshal([]byte(`{"id":"old-command","status":"QUEUED","execution":{"nodeId":"old-node","token":"legacy-secret"}}`), &c); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	observed := c.ObservedOutcome(100).Public()                                           /* 更新 observed 的值。 */
	if observed.Status != "UNKNOWN" || observed.LastError == "" || c.Status != "QUEUED" { /* 判断条件并选择处理分支。 */
		t.Fatal("legacy queue claimed execution or mutated history", observed) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	encoded, err := json.Marshal(observed)                                                                                  /* 更新 err 的值。 */
	if err != nil || strings.Contains(string(encoded), "legacy-secret") || strings.Contains(string(encoded), "execution") { /* 判断条件并选择处理分支。 */
		t.Fatal("legacy execution metadata leaked", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
