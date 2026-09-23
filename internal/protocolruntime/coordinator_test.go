package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestCoordinatorLossCancelsOldExecution(t *testing.T) { /* 定义 TestCoordinatorLossCancelsOldExecution 函数。 */
	repo := memory.NewRepository()                                                   /* 更新 repo 的值。 */
	ctx, cancel := context.WithCancel(context.Background())                          /* 更新 cancel 的值。 */
	defer cancel()                                                                   /* 安排函数结束时执行清理。 */
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", Enabled: true} /* 更新 p 的值。 */
	if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first := NewCoordinator(repo, "first", "http://first")    /* 更新 first 的值。 */
	second := NewCoordinator(repo, "second", "http://second") /* 更新 second 的值。 */
	done := make(chan struct{})                               /* 更新 done 的值。 */
	go func() { first.Run(ctx); close(done) }()               /* 执行当前语句并推进处理流程。 */
	execution, ok := first.Claim(ctx, p)                      /* 更新 ok 的值。 */
	if !ok {                                                  /* 判断条件并选择处理分支。 */
		t.Fatal("first owner rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, ok := second.Claim(ctx, p); ok { /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate executor") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := first.Validate(execution, p); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	lease, err := repo.GetExecutionLease(ctx, p.TenantID, "profile/"+p.ID) /* 更新 err 的值。 */
	if err != nil {                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.ReleaseExecutionLease(ctx, lease); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	next, ok := second.Claim(ctx, p) /* 更新 ok 的值。 */
	if !ok {                         /* 判断条件并选择处理分支。 */
		t.Fatal("takeover rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case <-execution.Done(): /* 处理当前分支。 */
	case <-time.After(2 * time.Second): /* 处理当前分支。 */
		t.Fatal("old execution survived lease loss") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := first.Validate(execution, p); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("old execution may ingest") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := second.Validate(next, p); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("new execution rejected", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	p.Enabled = false                                            /* 更新 p.Enabled 的值。 */
	if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := second.Validate(next, p); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("disabled profile may ingest") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cancel()                        /* 执行当前语句并推进处理流程。 */
	<-done                          /* 执行当前语句并推进处理流程。 */
	second.mu.Lock()                /* 执行当前语句并推进处理流程。 */
	for _, h := range second.held { /* 循环处理当前数据。 */
		h.timer.Stop() /* 执行当前语句并推进处理流程。 */
		h.cancel()     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	second.mu.Unlock() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
