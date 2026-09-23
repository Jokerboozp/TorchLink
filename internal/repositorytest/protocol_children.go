package repositorytest /* 声明 repositorytest 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"sync"                        /* 执行当前语句并推进处理流程。 */
	"sync/atomic"                 /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func ProtocolChildren(t *testing.T, repo ports.Repository) { /* 定义 ProtocolChildren 函数。 */
	t.Helper()                                      /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                     /* 更新 ctx 的值。 */
	tenant := "children-test"                       /* 更新 tenant 的值。 */
	for _, id := range []string{"main", "sensor"} { /* 循环处理当前数据。 */
		if e := repo.SaveProduct(ctx, model.Product{ID: id, TenantID: tenant, Status: "ENABLED"}); e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if e := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: tenant, ProtocolID: "sensor", Version: "1", Status: "PUBLISHED", PayloadFormat: "hex"}); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: tenant, ProductID: "sensor", ProtocolID: "sensor", Version: "1"}); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	p := model.DeviceAccessProfile{ID: "profile", TenantID: tenant, ProductID: "main", Mode: "listener", Network: "tcp", Enabled: true, ChildProducts: []model.ChildProductBinding{{Type: "smoke", ProductID: "sensor"}}} /* 更新 p 的值。 */
	if e := repo.SaveDeviceAccessProfile(ctx, p); e != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, id := range []string{"main-1", "main-2"} { /* 循环处理当前数据。 */
		if e := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: tenant, ProductID: "main", DeviceRole: "DIRECT", Status: "ENABLED", AccessKey: "children-" + id, SecretHash: "keep"}); e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	identity := model.ChildIdentity{Address: "01", Type: "smoke", Name: "探测器"} /* 更新 identity 的值。 */
	var created atomic.Int32                                                   /* 声明 created。 */
	var wg sync.WaitGroup                                                      /* 声明 wg。 */
	for i := 0; i < 12; i++ {                                                  /* 循环处理当前数据。 */
		wg.Add(1)   /* 执行当前语句并推进处理流程。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()                                                       /* 安排函数结束时执行清理。 */
			_, yes, err := repo.RegisterProtocolChild(ctx, p, "main-1", identity) /* 更新 err 的值。 */
			if err != nil {                                                       /* 判断条件并选择处理分支。 */
				t.Error(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if yes { /* 判断条件并选择处理分支。 */
				created.Add(1) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wg.Wait()                /* 执行当前语句并推进处理流程。 */
	if created.Load() != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("non atomic registration", created.Load()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	items, total, err := repo.ListManagedDeviceChildren(ctx, tenant, "main-1", 20, 0) /* 更新 err 的值。 */
	if err != nil || total != 1 || len(items) != 1 {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(items, total, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	child := items[0]                                                                         /* 更新 child 的值。 */
	if child.ProductID != "sensor" || child.GatewayID != "main-1" || child.SecretHash != "" { /* 判断条件并选择处理分支。 */
		t.Fatal(child) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second, _, err := repo.RegisterProtocolChild(ctx, p, "main-2", identity) /* 更新 err 的值。 */
	if err != nil || second.ID == child.ID {                                 /* 判断条件并选择处理分支。 */
		t.Fatal("parent address collision", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	parent, err := repo.GetManagedDevice(ctx, tenant, "main-1")                      /* 更新 err 的值。 */
	if err != nil || parent.DeviceRole != "GATEWAY" || parent.SecretHash != "keep" { /* 判断条件并选择处理分支。 */
		t.Fatal("parent credentials or role changed", parent, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	identity.Name = "更新名称"                                                      /* 更新 identity.Name 的值。 */
	updated, yes, err := repo.RegisterProtocolChild(ctx, p, "main-1", identity) /* 更新 err 的值。 */
	if err != nil || yes || updated.Name != identity.Name {                     /* 判断条件并选择处理分支。 */
		t.Fatal(updated, yes, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	identity.Type = "unconfigured"                                                      /* 更新 identity.Type 的值。 */
	if _, _, err = repo.RegisterProtocolChild(ctx, p, "main-1", identity); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("unknown child type accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	identity.Type = "smoke"                                                                 /* 更新 identity.Type 的值。 */
	stale := p                                                                              /* 更新 stale 的值。 */
	stale.Enabled = false                                                                   /* 更新 stale.Enabled 的值。 */
	if _, _, err = repo.RegisterProtocolChild(ctx, stale, "main-1", identity); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("stale config accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = repo.RegisterProtocolChild(ctx, p, child.ID, identity); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("child became parent") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if list, n, e := repo.ListManagedDeviceChildren(ctx, "other", parent.ID, 20, 0); e != nil || n != 0 || len(list) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("cross tenant child leaked") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	child.Status = "DISABLED"                                                           /* 更新 child.Status 的值。 */
	repo.SaveManagedDevice(ctx, child)                                                  /* 执行当前语句并推进处理流程。 */
	if _, _, err = repo.RegisterProtocolChild(ctx, p, "main-1", identity); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("disabled child reenabled") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
