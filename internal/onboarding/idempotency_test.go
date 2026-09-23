package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"sync"                        /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestConcurrentCreationRecoversWithoutCredentialRotation(t *testing.T) { /* 定义 TestConcurrentCreationRecoversWithoutCredentialRotation 函数。 */
	s, repo, q := fixture(t)            /* 更新 q 的值。 */
	q.ProductID = "new-product"         /* 更新 q.ProductID 的值。 */
	q.ProductName = "new product"       /* 更新 q.ProductName 的值。 */
	q = tested(t, s, q)                 /* 更新 q 的值。 */
	const count = 12                    /* 声明 count。 */
	var wg sync.WaitGroup               /* 声明 wg。 */
	results := make(chan Result, count) /* 更新 results 的值。 */
	failures := make(chan error, count) /* 更新 failures 的值。 */
	for i := 0; i < count; i++ {        /* 循环处理当前数据。 */
		wg.Add(1)   /* 执行当前语句并推进处理流程。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()                                     /* 安排函数结束时执行清理。 */
			r, e := s.Create(context.Background(), "tenant", q) /* 更新 e 的值。 */
			results <- r                                        /* 执行当前语句并推进处理流程。 */
			failures <- e                                       /* 执行当前语句并推进处理流程。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wg.Wait()                   /* 执行当前语句并推进处理流程。 */
	close(results)              /* 执行当前语句并推进处理流程。 */
	close(failures)             /* 执行当前语句并推进处理流程。 */
	for err := range failures { /* 循环处理当前数据。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	secrets := 0             /* 更新 secrets 的值。 */
	for r := range results { /* 循环处理当前数据。 */
		if r.Credential.Secret != "" { /* 判断条件并选择处理分支。 */
			secrets++     /* 执行当前语句并推进处理流程。 */
			if r.Reused { /* 判断条件并选择处理分支。 */
				t.Fatal("replay disclosed secret") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if secrets != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("generated %d secrets", secrets) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	devices, _ := repo.ListManagedDevices(context.Background(), "tenant") /* 更新 _ 的值。 */
	if len(devices) != 1 {                                                /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate devices") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.TestToken = "expired"                                                         /* 更新 q.TestToken 的值。 */
	if r, e := s.Create(context.Background(), "tenant", q); e != nil || !r.Reused { /* 判断条件并选择处理分支。 */
		t.Fatal("lost response could not recover", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.Name = "different"                                               /* 更新 q.Name 的值。 */
	if _, e := s.Create(context.Background(), "tenant", q); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("different request did not conflict") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

type failingOnboardingRepository struct{ ports.Repository } /* 定义 failingOnboardingRepository 类型。 */

func (r failingOnboardingRepository) SaveOnboarding(context.Context, model.OnboardingBundle) error { /* 定义 SaveOnboarding 函数。 */
	return errors.New("injected storage failure") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func TestFailedSaveLeavesNoResourcesAndCanRetry(t *testing.T) { /* 定义 TestFailedSaveLeavesNoResourcesAndCanRetry 函数。 */
	s, repo, q := fixture(t)                                           /* 更新 q 的值。 */
	q.ProductID = "new-product"                                        /* 更新 q.ProductID 的值。 */
	q.ProductName = "new product"                                      /* 更新 q.ProductName 的值。 */
	q = tested(t, s, q)                                                /* 更新 q 的值。 */
	s.Repo = failingOnboardingRepository{repo}                         /* 更新 s.Repo 的值。 */
	if _, e := s.Create(context.Background(), "tenant", q); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("failed storage reported success") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e := repo.GetProduct(context.Background(), "tenant", q.ProductID); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("failed request leaked product") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e := repo.GetManagedDevice(context.Background(), "tenant", q.DeviceID); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("failed request leaked device") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	s.Repo = repo                                                                                               /* 更新 s.Repo 的值。 */
	if r, e := s.Create(context.Background(), "tenant", q); e != nil || r.Reused || r.Credential.Secret == "" { /* 判断条件并选择处理分支。 */
		t.Fatal("retry failed", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
