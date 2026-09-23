package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestVideoCameraRelationsEnforceOneDevicePerCamera(t *testing.T) { /* 定义 TestVideoCameraRelationsEnforceOneDevicePerCamera 函数。 */
	repo := NewRepository()                                                                                                                                                                                  /* 更新 repo 的值。 */
	camera := model.VideoCameraMapping{TenantID: "tenant-001", CameraID: "camera-001", CameraName: "一号摄像头", DeviceID: "device-001", Brand: "大华", CameraPoint: "东侧入口", Building: "A", Floor: "1", Room: "大厅"} /* 更新 camera 的值。 */
	if err := repo.SaveVideoCameraMapping(context.Background(), camera); err != nil {                                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.SaveVideoCameraMapping(context.Background(), model.VideoCameraMapping{TenantID: "tenant-001", CameraID: "camera-002", CameraName: "二号摄像头", DeviceID: "device-001"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	relations, err := repo.ListVideoCameraRelationsByTarget(context.Background(), "tenant-001", "device", "device-001") /* 更新 err 的值。 */
	if err != nil {                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(relations) != 2 || relations[0].CameraID != "camera-001" || relations[1].CameraID != "camera-002" { /* 判断条件并选择处理分支。 */
		t.Fatalf("reverse device lookup = %#v", relations) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	otherDevice, err := repo.ListVideoCameraRelationsByTarget(context.Background(), "tenant-001", "device", "device-002") /* 更新 err 的值。 */
	if err != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(otherDevice) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected second device relation = %#v", otherDevice) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cameraRelations, err := repo.ListVideoCameraRelations(context.Background(), "tenant-001", "camera-001") /* 更新 err 的值。 */
	if err != nil {                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(cameraRelations) != 1 || cameraRelations[0].TargetID != "device-001" { /* 判断条件并选择处理分支。 */
		t.Fatalf("camera relations = %#v", cameraRelations) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
