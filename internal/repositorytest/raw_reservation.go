package repositorytest /* 声明 repositorytest 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"sync"                        /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func RawReservation(t *testing.T, repo ports.Repository) { /* 定义 RawReservation 函数。 */
	t.Helper()                                 /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                /* 更新 ctx 的值。 */
	var wg sync.WaitGroup                      /* 声明 wg。 */
	results := make(chan model.RawMessage, 16) /* 更新 results 的值。 */
	conflicts := make(chan bool, 16)           /* 更新 conflicts 的值。 */
	for i := 0; i < 16; i++ {                  /* 循环处理当前数据。 */
		wg.Add(1)        /* 执行当前语句并推进处理流程。 */
		go func(i int) { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()                                                                                                                                                                /* 安排函数结束时执行清理。 */
			v := model.RawMessage{TenantID: "reservation-test", ProductID: "product", DeviceID: "device", MessageID: "one", ProtocolVersion: "first", Payload: json.RawMessage(`{"x":1}`)} /* 更新 v 的值。 */
			if i%2 == 1 {                                                                                                                                                                  /* 判断条件并选择处理分支。 */
				v.Payload = json.RawMessage(`{"x":2}`) /* 更新 v.Payload 的值。 */
				v.ProtocolVersion = "other"            /* 更新 v.ProtocolVersion 的值。 */
			} /* 结束当前表达式或代码块。 */
			canonical, err := repo.ReserveRawMessage(ctx, v) /* 更新 err 的值。 */
			if err != nil {                                  /* 判断条件并选择处理分支。 */
				if !errors.Is(err, model.ErrRawConflict) { /* 判断条件并选择处理分支。 */
					t.Error(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				conflicts <- true /* 执行当前语句并推进处理流程。 */
				return            /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			results <- canonical /* 执行当前语句并推进处理流程。 */
		}(i) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wg.Wait()                  /* 执行当前语句并推进处理流程。 */
	close(results)             /* 执行当前语句并推进处理流程。 */
	close(conflicts)           /* 执行当前语句并推进处理流程。 */
	var first model.RawMessage /* 声明 first。 */
	count := 0                 /* 更新 count 的值。 */
	for v := range results {   /* 循环处理当前数据。 */
		if count == 0 { /* 判断条件并选择处理分支。 */
			first = v /* 更新 first 的值。 */
		} else if v.PayloadHash() != first.PayloadHash() || v.ProtocolVersion != first.ProtocolVersion { /* 结束当前表达式或代码块。 */
			t.Error("conflicting routing snapshots accepted") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		count++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if count != 8 || len(conflicts) != 8 { /* 判断条件并选择处理分支。 */
		t.Fatalf("accepted %d conflicting %d", count, len(conflicts)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	retry := first                                                                                                    /* 更新 retry 的值。 */
	retry.ProtocolVersion = "new-version"                                                                             /* 更新 retry.ProtocolVersion 的值。 */
	retry.ReceivedAt = 9999                                                                                           /* 更新 retry.ReceivedAt 的值。 */
	canonical, err := repo.ReserveRawMessage(ctx, retry)                                                              /* 更新 err 的值。 */
	if err != nil || canonical.ProtocolVersion != first.ProtocolVersion || canonical.ReceivedAt != first.ReceivedAt { /* 判断条件并选择处理分支。 */
		t.Fatal("retry changed reserved parser snapshot", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
