// Package repositorytest contains storage contract checks shared by adapters.
package repositorytest /* 声明 repositorytest 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"reflect"                     /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func AccessStatus(t *testing.T, repo ports.Repository) { /* 定义 AccessStatus 函数。 */
	t.Helper()                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                                                                                                                                                         /* 更新 ctx 的值。 */
	original := model.DeviceAccessProfile{TenantID: "status-test", ID: "profile", ProductID: "product", DeviceID: "device", Host: "127.0.0.1", Port: 502, Enabled: true, UpdatedAt: 10} /* 更新 original 的值。 */
	if err := repo.SaveDeviceAccessProfile(ctx, original); err != nil {                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if ok, err := repo.UpdateDeviceAccessStatus(ctx, original, "ONLINE", "", 20); err != nil || !ok { /* 判断条件并选择处理分支。 */
		t.Fatal("status update rejected", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	current, err := repo.GetDeviceAccessProfile(ctx, original.TenantID, original.ID) /* 更新 err 的值。 */
	if err != nil || current.LastSuccessAt != 20 || current.UpdatedAt != 10 {        /* 判断条件并选择处理分支。 */
		t.Fatal("status changed configuration revision", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, sample := range []struct { /* 循环处理当前数据。 */
		status               string /* 执行当前语句并推进处理流程。 */
		at, success, failure int64  /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{"LISTENING", 0, 20, 0}, {"LISTENING", 25, 25, 0}, {"ERROR", 26, 25, 26}, {"LISTENING", 0, 25, 26}, {"PENDING", 0, 25, 26}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if ok, err := repo.UpdateDeviceAccessStatus(ctx, original, sample.status, "", sample.at); err != nil || !ok { /* 判断条件并选择处理分支。 */
			t.Fatal("listener status update", err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		current, err = repo.GetDeviceAccessProfile(ctx, original.TenantID, original.ID)                                                               /* 更新 err 的值。 */
		if err != nil || current.RuntimeStatus != sample.status || current.LastSuccessAt != sample.success || current.LastErrorAt != sample.failure { /* 判断条件并选择处理分支。 */
			t.Fatalf("listener status timestamps: %+v %v", current, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	current.Enabled, current.Host, current.EdgeNodeID, current.UpdatedAt = false, "192.0.2.1", "remote", 30 /* 更新 current.UpdatedAt 的值。 */
	if err := repo.SaveDeviceAccessProfile(ctx, current); err != nil {                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, status := range []string{"ONLINE", "ERROR", "LISTENING", "PENDING"} { /* 循环处理当前数据。 */
		if ok, err := repo.UpdateDeviceAccessStatus(ctx, original, status, "late observation", 40); err != nil || ok { /* 判断条件并选择处理分支。 */
			t.Fatal("stale status accepted", status, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	actual, err := repo.GetDeviceAccessProfile(ctx, current.TenantID, current.ID) /* 更新 err 的值。 */
	if err != nil || !reflect.DeepEqual(actual, current) {                        /* 判断条件并选择处理分支。 */
		t.Fatal("stale observation replaced user edit", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	other := current                                                                                         /* 更新 other 的值。 */
	other.TenantID = "other-tenant"                                                                          /* 更新 other.TenantID 的值。 */
	if ok, err := repo.UpdateDeviceAccessStatus(ctx, other, "ERROR", "cross tenant", 50); err != nil || ok { /* 判断条件并选择处理分支。 */
		t.Fatal("cross tenant status accepted", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
