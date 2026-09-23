package repositorytest /* 声明 repositorytest 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"fmt"                         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"sync"                        /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func ExecutionLease(t *testing.T, repo ports.Repository) { /* 定义 ExecutionLease 函数。 */
	t.Helper()                                     /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                    /* 更新 ctx 的值。 */
	var wg sync.WaitGroup                          /* 声明 wg。 */
	results := make(chan model.ExecutionLease, 12) /* 更新 results 的值。 */
	for i := 0; i < 12; i++ {                      /* 循环处理当前数据。 */
		wg.Add(1)        /* 执行当前语句并推进处理流程。 */
		go func(i int) { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()                                                                                                                       /* 安排函数结束时执行清理。 */
			v, ok, err := repo.AcquireExecutionLease(ctx, "lease-test", "profile/one", fmt.Sprintf("owner-%d", i), "http://gateway", time.Second) /* 更新 err 的值。 */
			if err != nil {                                                                                                                       /* 判断条件并选择处理分支。 */
				t.Error(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if ok { /* 判断条件并选择处理分支。 */
				results <- v /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		}(i) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wg.Wait()                      /* 执行当前语句并推进处理流程。 */
	close(results)                 /* 执行当前语句并推进处理流程。 */
	var first model.ExecutionLease /* 声明 first。 */
	count := 0                     /* 更新 count 的值。 */
	for v := range results {       /* 循环处理当前数据。 */
		count++   /* 执行当前语句并推进处理流程。 */
		first = v /* 更新 first 的值。 */
	} /* 结束当前表达式或代码块。 */
	if count != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("%d owners acquired one resource", count) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	renewed, ok, err := repo.AcquireExecutionLease(ctx, first.TenantID, first.Resource, first.Owner, first.Endpoint, time.Second) /* 更新 err 的值。 */
	if err != nil || !ok || renewed.Token != first.Token {                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal("renewal changed token", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, ok, err := repo.AcquireExecutionLease(ctx, "another-tenant", first.Resource, "different", first.Endpoint, time.Second); err != nil || !ok { /* 判断条件并选择处理分支。 */
		t.Fatal("tenant namespace collided", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.ReleaseExecutionLease(ctx, first); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second, ok, err := repo.AcquireExecutionLease(ctx, first.TenantID, first.Resource, "next", first.Endpoint, time.Second) /* 更新 err 的值。 */
	if err != nil || !ok || second.Token <= first.Token {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal("takeover lacks fencing", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.ReleaseExecutionLease(ctx, first); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	current, err := repo.GetExecutionLease(ctx, first.TenantID, first.Resource) /* 更新 err 的值。 */
	if err != nil || current.Owner != second.Owner {                            /* 判断条件并选择处理分支。 */
		t.Fatal("stale owner released successor", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	time.Sleep(1100 * time.Millisecond)                                                    /* 执行当前语句并推进处理流程。 */
	if _, err := repo.GetExecutionLease(ctx, first.TenantID, first.Resource); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expired ownership remained valid") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	third, ok, err := repo.AcquireExecutionLease(ctx, first.TenantID, first.Resource, "third", first.Endpoint, time.Second) /* 更新 err 的值。 */
	if err != nil || !ok || third.Token <= second.Token {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal("expired lease not fenced", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
