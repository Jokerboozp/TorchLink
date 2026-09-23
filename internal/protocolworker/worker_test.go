package protocolworker /* 声明 protocolworker 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestIngressResponseCannotConsumeOrAcknowledgeIncompleteFrames(t *testing.T) { /* 定义 TestIngressResponseCannotConsumeOrAcknowledgeIncompleteFrames 函数。 */
	for _, r := range []Response{ /* 循环处理当前数据。 */
		{NeedMore: true, Consumed: 1}, {NeedMore: true, Reply: "AA"}, {NeedMore: true, DeviceID: "device"}, /* 执行当前语句并推进处理流程。 */
		{Consumed: -1, DeviceID: "device"}, {Consumed: 3, DeviceID: "device"}, {Consumed: 2, DeviceID: "../other"}, /* 执行当前语句并推进处理流程。 */
		{Consumed: 2, DeviceID: "device", Reply: "XY"}, {Consumed: 2, DeviceID: "device", Reply: strings.Repeat("00", MaxFrameBytes+1)}, /* 执行当前语句并推进处理流程。 */
		{Consumed: 2, DeviceID: "device", State: []byte(strings.Repeat(" ", MaxStateBytes+1))}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if err := validateResponse("ingress", 2, r); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("unsafe result accepted: consumed=%d needMore=%v device=%q", r.Consumed, r.NeedMore, r.DeviceID) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, r := range []Response{{NeedMore: true}, {Consumed: 1, DeviceID: "device", Reply: "AA"}} { /* 循环处理当前数据。 */
		if err := validateResponse("ingress", 2, r); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateResponse("encode", 0, Response{}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("empty downlink accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateResponse("decode", 0, Response{}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("empty standard message accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDeviceIdentityIsSafeAndStable(t *testing.T) { /* 定义 TestDeviceIdentityIsSafeAndStable 函数。 */
	for _, id := range []string{"", " device", "device ", "a/b", "a\\b", "a\x00b", "a\nb", strings.Repeat("a", 129)} { /* 循环处理当前数据。 */
		if ValidDeviceID(id) { /* 判断条件并选择处理分支。 */
			t.Fatalf("accepted %q", id) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !ValidDeviceID("gb26875_123456789012") { /* 判断条件并选择处理分支。 */
		t.Fatal("valid address rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
