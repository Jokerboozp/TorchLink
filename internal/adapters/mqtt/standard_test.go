package mqttadapter /* 声明 mqttadapter 包。 */

import "testing" /* 引入当前代码需要的依赖。 */

func TestStandardTopicIdentity(t *testing.T) { /* 定义 TestStandardTopicIdentity 函数。 */
	tenant, product, device, kind, err := StandardTopic("/iot/up/t/p/d/property")             /* 更新 err 的值。 */
	if err != nil || tenant != "t" || product != "p" || device != "d" || kind != "property" { /* 判断条件并选择处理分支。 */
		t.Fatal(tenant, product, device, kind, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, topic := range []string{"/iot/up/t/p/d/shadow-get", "/iot/down/t/p/d/shadow", "/iot/up/t/p/d/command", "iot/up/t/p/d/property", "/iot/up/t/p/d/property/extra", "/external/raw/t/p/d"} { /* 循环处理当前数据。 */
		if _, _, _, _, err := StandardTopic(topic); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("accepted %q", topic) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
