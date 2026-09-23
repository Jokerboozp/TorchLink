package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestMatchRuleAll(t *testing.T) { /* 定义 TestMatchRuleAll 函数。 */
	rule := model.AlarmRule{TenantID: "t", Enabled: true, Match: "all", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}, {Field: "smoke", Operator: "eq", Value: true}}} /* 更新 rule 的值。 */
	msg := model.StandardMessage{TenantID: "t", Properties: map[string]any{"temperature": 81.2, "smoke": true}}                                                                                             /* 更新 msg 的值。 */
	if !MatchRule(rule, msg) {                                                                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal("expected match") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	msg.Properties["smoke"] = false /* 执行当前语句并推进处理流程。 */
	if MatchRule(rule, msg) {       /* 判断条件并选择处理分支。 */
		t.Fatal("unexpected match") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestMatchRuleEventPath(t *testing.T) { /* 定义 TestMatchRuleEventPath 函数。 */
	rule := model.AlarmRule{TenantID: "t", Enabled: true, Conditions: []model.RuleCondition{{Field: "event.smoke", Operator: "eq", Value: true}}} /* 更新 rule 的值。 */
	msg := model.StandardMessage{TenantID: "t", Event: map[string]any{"smoke": true}}                                                             /* 更新 msg 的值。 */
	if !MatchRule(rule, msg) {                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal("event-prefixed condition should match the event field") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestAlarmTopicSanitizesSegments(t *testing.T) { /* 定义 TestAlarmTopicSanitizesSegments 函数。 */
	a := model.Alarm{CityCode: "city", DistrictCode: "district/escape", BuildingID: "A", DeviceType: "smoke", DeviceID: "d"} /* 更新 a 的值。 */
	if got := a.MQTTTopic("raised"); got != "/iot/alarm/raised/city/district_escape/A/smoke/d" {                             /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected topic %s", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
