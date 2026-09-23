package repositorytest /* 声明 repositorytest 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"sync"                        /* 执行当前语句并推进处理流程。 */
	"sync/atomic"                 /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func ProtocolRegistration(t *testing.T, r ports.Repository) { /* 定义 ProtocolRegistration 函数。 */
	t.Helper()                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                                                                                                                                 /* 更新 ctx 的值。 */
	const tenant = "protocol-registration-tenant"                                                                                                               /* 声明 tenant。 */
	product := model.Product{TenantID: tenant, ID: "product", Status: "ENABLED"}                                                                                /* 更新 product 的值。 */
	p := model.DeviceAccessProfile{TenantID: tenant, ID: "profile", ProductID: product.ID, Mode: "listener", Network: "tcp", AutoRegister: true, Enabled: true} /* 更新 p 的值。 */
	for _, err := range []error{r.SaveProduct(ctx, product), r.SaveDeviceAccessProfile(ctx, p)} {                                                               /* 循环处理当前数据。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	var wg sync.WaitGroup     /* 声明 wg。 */
	var winners atomic.Int32  /* 声明 winners。 */
	for i := 0; i < 16; i++ { /* 循环处理当前数据。 */
		wg.Add(1)   /* 执行当前语句并推进处理流程。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()                                                                 /* 安排函数结束时执行清理。 */
			d, created, err := r.RegisterProtocolDevice(ctx, p, "first", "Protocol device") /* 更新 err 的值。 */
			if err != nil {                                                                 /* 判断条件并选择处理分支。 */
				t.Error(err) /* 验证实际结果符合预期。 */
				return       /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if created { /* 判断条件并选择处理分支。 */
				winners.Add(1) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if d.AccessKey == "" || d.SecretHash != "" || !d.AutoRegistered { /* 判断条件并选择处理分支。 */
				t.Error("invalid registered inventory") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wg.Wait()                /* 执行当前语句并推进处理流程。 */
	if winners.Load() != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("registration created more than once", winners.Load()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first, err := r.GetManagedDevice(ctx, tenant, "first") /* 更新 err 的值。 */
	if err != nil {                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second, created, err := r.RegisterProtocolDevice(ctx, p, "second", "Second") /* 更新 err 的值。 */
	if err != nil || !created || first.AccessKey == second.AccessKey {           /* 判断条件并选择处理分支。 */
		t.Fatal("second identity key collision", second, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first.Name, first.AccessKey, first.SecretHash = "Operator edited", "existing-access", "existing-secret-hash" /* 更新 first.SecretHash 的值。 */
	if err = r.SaveManagedDevice(ctx, first); err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	unchanged, created, err := r.RegisterProtocolDevice(ctx, p, "first", "Overwrite attempt")                                                        /* 更新 err 的值。 */
	if err != nil || created || unchanged.Name != first.Name || unchanged.AccessKey != first.AccessKey || unchanged.SecretHash != first.SecretHash { /* 判断条件并选择处理分支。 */
		t.Fatal("repeat registration overwrote credentials/name", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first.Status = "DISABLED"                              /* 更新 first.Status 的值。 */
	if err = r.SaveManagedDevice(ctx, first); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "first", "Restore attempt"); !errors.Is(err, model.ErrProtocolRegistration) { /* 判断条件并选择处理分支。 */
		t.Fatal("disabled device revived", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second.ProductID = "other-product"                      /* 更新 second.ProductID 的值。 */
	if err = r.SaveManagedDevice(ctx, second); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "second", "Move attempt"); !errors.Is(err, model.ErrProtocolRegistration) { /* 判断条件并选择处理分支。 */
		t.Fatal("foreign product claimed", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	current := p                                                   /* 更新 current 的值。 */
	current.AutoRegister = false                                   /* 更新 current.AutoRegister 的值。 */
	if err = r.SaveDeviceAccessProfile(ctx, current); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "stale", ""); !errors.Is(err, model.ErrProtocolRegistration) { /* 判断条件并选择处理分支。 */
		t.Fatal("stale configuration registered device", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = r.SaveDeviceAccessProfile(ctx, p); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	p.EdgeNodeID = "removed-node"                            /* 更新 p.EdgeNodeID 的值。 */
	if err = r.SaveDeviceAccessProfile(ctx, p); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "legacy-node", ""); !errors.Is(err, model.ErrProtocolRegistration) { /* 判断条件并选择处理分支。 */
		t.Fatal("legacy edge assignment registered device", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

} /* 结束当前表达式或代码块。 */
