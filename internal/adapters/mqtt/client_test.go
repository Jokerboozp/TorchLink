package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestTopicIdentityOverridesPayloadTenant(t *testing.T) { /* 定义 TestTopicIdentityOverridesPayloadTenant 函数。 */
	raw := model.RawMessage{TenantID: "spoofed", ProductID: "spoofed", DeviceID: "spoofed"}          /* 更新 raw 的值。 */
	if err := applyRawTopicIdentity("/external/raw/tenant-a/product-a/device-a", &raw); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if raw.TenantID != "tenant-a" || raw.ProductID != "product-a" || raw.DeviceID != "device-a" { /* 判断条件并选择处理分支。 */
		t.Fatalf("topic identity was not authoritative: %#v", raw) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	state := model.DeviceState{TenantID: "spoofed", ProductID: "spoofed", DeviceID: "spoofed"}               /* 更新 state 的值。 */
	if err := applyStateTopicIdentity("/iot/device/state/tenant-a/product-a/device-a", &state); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if state.TenantID != "tenant-a" || state.ProductID != "product-a" || state.DeviceID != "device-a" { /* 判断条件并选择处理分支。 */
		t.Fatalf("state topic identity was not authoritative: %#v", state) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	v := model.VideoAlarmEvent{TenantID: "spoofed", CameraID: "spoofed"}                           /* 更新 v 的值。 */
	if err := applyVideoTopicIdentity("/external/video/alarm/tenant-a/camera-a", &v); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if v.TenantID != "tenant-a" || v.CameraID != "camera-a" { /* 判断条件并选择处理分支。 */
		t.Fatalf("video topic identity was not authoritative: %#v", v) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestTopicIdentityRejectsMalformedTopics(t *testing.T) { /* 定义 TestTopicIdentityRejectsMalformedTopics 函数。 */
	if err := applyStateTopicIdentity("/iot/device/state/tenant-a/product-a", &model.DeviceState{}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected malformed state topic to be rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := applyVideoTopicIdentity("/external/video/alarm/tenant-a", &model.VideoAlarmEvent{}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected malformed video topic to be rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
