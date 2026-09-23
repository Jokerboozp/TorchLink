package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestUpdateVideoEventReplacesPendingRecord(t *testing.T) { /* 定义 TestUpdateVideoEventReplacesPendingRecord 函数。 */
	repo := NewRepository()         /* 更新 repo 的值。 */
	event := model.VideoAlarmEvent{ /* 更新 event 的值。 */
		TenantID: "tenant_001",                                     /* 执行当前语句并推进处理流程。 */
		EventID:  "event_001",                                      /* 执行当前语句并推进处理流程。 */
		Raw:      map[string]any{"mediaTransferStatus": "PENDING"}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	created, err := repo.SaveVideoEvent(context.Background(), event) /* 更新 err 的值。 */
	if err != nil || !created {                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("SaveVideoEvent() created=%v err=%v", created, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	event.Raw["mediaTransferStatus"] = "COMPLETED"                             /* 执行当前语句并推进处理流程。 */
	if err := repo.UpdateVideoEvent(context.Background(), event); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	pending, err := repo.ListPendingVideoEvents(context.Background(), 10) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(pending) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("updated event remained pending: %#v", pending) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
