package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestBatchLookupsKeepTenantAndRequestedIDs(t *testing.T) { /* 定义 TestBatchLookupsKeepTenantAndRequestedIDs 函数。 */
	ctx := context.Background()                           /* 更新 ctx 的值。 */
	repo := NewRepository()                               /* 更新 repo 的值。 */
	for _, tenant := range []string{"allowed", "other"} { /* 循环处理当前数据。 */
		if err := repo.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "p", Name: tenant}); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: "d", BusinessStatus: tenant}); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if err := repo.SaveStandardMessage(ctx, model.StandardMessage{TenantID: tenant, MessageID: "m", RawMessageID: "raw", DeviceID: "d", Parser: tenant}); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if err := repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: tenant, CameraID: "camera", DeviceID: "d", CameraName: tenant}); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	products, err := repo.GetProductsByIDs(ctx, "allowed", []string{"p", "missing"}) /* 更新 err 的值。 */
	if err != nil || len(products) != 1 || products["p"].Name != "allowed" {         /* 判断条件并选择处理分支。 */
		t.Fatalf("products: %+v, %v", products, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	states, err := repo.GetDeviceStatesByIDs(ctx, "allowed", []string{"d", "missing"}) /* 更新 err 的值。 */
	if err != nil || len(states) != 1 || states["d"].BusinessStatus != "allowed" {     /* 判断条件并选择处理分支。 */
		t.Fatalf("states: %+v, %v", states, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	messages, err := repo.GetStandardMessagesByRawIDs(ctx, "allowed", []string{"raw", "missing"}) /* 更新 err 的值。 */
	if err != nil || len(messages) != 1 || messages["raw"].Parser != "allowed" {                  /* 判断条件并选择处理分支。 */
		t.Fatalf("messages: %+v, %v", messages, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cameras, err := repo.ListVideoCameraMappingsByDeviceIDs(ctx, "allowed", []string{"d", "missing"})         /* 更新 err 的值。 */
	if err != nil || len(cameras) != 1 || len(cameras["d"]) != 1 || cameras["d"][0].CameraName != "allowed" { /* 判断条件并选择处理分支。 */
		t.Fatalf("cameras: %+v, %v", cameras, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
