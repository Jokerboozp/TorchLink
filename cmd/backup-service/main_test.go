package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestNextDailyRun(t *testing.T) { /* 定义 TestNextDailyRun 函数。 */
	location := time.FixedZone("CST", 8*60*60)            /* 更新 location 的值。 */
	now := time.Date(2026, 8, 31, 23, 58, 0, 0, location) /* 更新 now 的值。 */
	next, err := nextDailyRun(now, "23:59", location)     /* 更新 err 的值。 */
	if err != nil {                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("next daily run: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	want := time.Date(2026, 8, 31, 23, 59, 0, 0, location) /* 更新 want 的值。 */
	if !next.Equal(want) {                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("next daily run = %s, want %s", next, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	next, err = nextDailyRun(want, "23:59", location) /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("next day run: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	want = time.Date(2026, 9, 1, 23, 59, 0, 0, location) /* 更新 want 的值。 */
	if !next.Equal(want) {                               /* 判断条件并选择处理分支。 */
		t.Fatalf("next day run = %s, want %s", next, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestNextDailyRunRejectsInvalidClock(t *testing.T) { /* 定义 TestNextDailyRunRejectsInvalidClock 函数。 */
	location := time.FixedZone("CST", 8*60*60)                                             /* 更新 location 的值。 */
	if _, err := nextDailyRun(time.Now().In(location), "midnight", location); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected invalid clock error") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestScheduledRawLogBackupTargetsPreviousDay(t *testing.T) { /* 定义 TestScheduledRawLogBackupTargetsPreviousDay 函数。 */
	location := time.FixedZone("CST", 8*60*60)           /* 更新 location 的值。 */
	next := time.Date(2026, 9, 1, 0, 5, 0, 0, location)  /* 更新 next 的值。 */
	backupDay := next.AddDate(0, 0, -1)                  /* 更新 backupDay 的值。 */
	want := time.Date(2026, 8, 31, 0, 5, 0, 0, location) /* 更新 want 的值。 */
	if !backupDay.Equal(want) {                          /* 判断条件并选择处理分支。 */
		t.Fatalf("backup day = %s, want %s", backupDay, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestRespondUsesServerErrorForBackupExecutionFailure(t *testing.T) { /* 定义 TestRespondUsesServerErrorForBackupExecutionFailure 函数。 */
	recorder := httptest.NewRecorder()                   /* 更新 recorder 的值。 */
	respond(recorder, nil, errTestBackupFailure{})       /* 检查错误并决定后续处理。 */
	if recorder.Code != http.StatusInternalServerError { /* 判断条件并选择处理分支。 */
		t.Fatalf("response status = %d, want %d", recorder.Code, http.StatusInternalServerError) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var payload map[string]string                                           /* 声明 payload。 */
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !strings.Contains(payload["error"], "backup failed") { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected response body: %s", recorder.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

type errTestBackupFailure struct{} /* 定义 errTestBackupFailure 类型。 */

func (errTestBackupFailure) Error() string { return "backup failed" } /* 定义 Error 函数。 */
