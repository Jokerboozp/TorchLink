package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestPresentRuleKeepsJSONExecutableAndGengineCommentedByDefault(t *testing.T) { /* 定义 TestPresentRuleKeepsJSONExecutableAndGengineCommentedByDefault 函数。 */
	rule := model.AlarmRule{ /* 更新 rule 的值。 */
		Name:        "高温烟雾",            /* 执行当前语句并推进处理流程。 */
		Description: "温度过高且烟雾信号出现时告警。", /* 执行当前语句并推进处理流程。 */
		AlarmType:   "FIRE_RISK",       /* 执行当前语句并推进处理流程。 */
		Level:       "HIGH",            /* 执行当前语句并推进处理流程。 */
		Match:       "all",             /* 执行当前语句并推进处理流程。 */
		Conditions: []model.RuleCondition{ /* 执行当前语句并推进处理流程。 */
			{Field: "temperature", Operator: ">", Value: 80}, /* 执行当前语句并推进处理流程。 */
			{Field: "smoke", Operator: "eq", Value: true},    /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		Enabled: false, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	presentation, err := PresentRule(rule) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var executable map[string]any                                                 /* 声明 executable。 */
	if err = json.Unmarshal([]byte(presentation.JSON), &executable); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if executable["description"] != rule.Description || strings.Contains(presentation.JSON, "_comment") { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected JSON presentation: %s", presentation.JSON) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if presentation.Gengine != `Properties["temperature"] > 80 && Properties["smoke"] == true` { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected Gengine: %q", presentation.Gengine) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !strings.HasPrefix(presentation.GenginePlaceholder, "//") || !strings.Contains(presentation.GenginePlaceholder, presentation.Gengine) { /* 判断条件并选择处理分支。 */
		t.Fatalf("Gengine is not commented in placeholder: %q", presentation.GenginePlaceholder) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, required := range []string{"conditions", "recovery[].field", "recovery[].operator", "recovery[].value", "actions[].type", "actions[].cameraId", "actions[].page"} { /* 循环处理当前数据。 */
		found := false                                               /* 更新 found 的值。 */
		for _, description := range presentation.FieldDescriptions { /* 循环处理当前数据。 */
			if description.Field == required && strings.TrimSpace(description.Meaning) != "" { /* 判断条件并选择处理分支。 */
				found = true /* 更新 found 的值。 */
				break        /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !found { /* 判断条件并选择处理分支。 */
			t.Fatalf("missing field description for %q", required) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGeneratedGengineSpecialOperatorsValidateAndEvaluate(t *testing.T) { /* 定义 TestGeneratedGengineSpecialOperatorsValidateAndEvaluate 函数。 */
	rule := model.AlarmRule{Conditions: []model.RuleCondition{ /* 更新 rule 的值。 */
		{Field: "smokeText", Operator: "contains", Value: "smoke"},   /* 执行当前语句并推进处理流程。 */
		{Field: "temperature", Operator: "in", Value: []any{70, 80}}, /* 执行当前语句并推进处理流程。 */
		{Field: "properties.temperature", Operator: "exists"},        /* 执行当前语句并推进处理流程。 */
	}} /* 结束当前表达式或代码块。 */
	// Validate each generated condition separately so the test also documents
	// the supported expression snippets exposed by the rule editor.
	for _, condition := range rule.Conditions { /* 循环处理当前数据。 */
		expression := RenderGengine(model.AlarmRule{Conditions: []model.RuleCondition{condition}}) /* 更新 expression 的值。 */
		if err := ValidateGengineExpression(expression); err != nil {                              /* 判断条件并选择处理分支。 */
			t.Fatalf("generated expression %q did not compile: %v", expression, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if matched, err := EvaluateGengineExpression(`Contains(Properties["smokeText"], "smoke")`, model.StandardMessage{Properties: map[string]any{"smokeText": "smoke detected"}}); err != nil || !matched { /* 判断条件并选择处理分支。 */
		t.Fatalf("contains expression matched=%v err=%v", matched, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if matched, err := EvaluateGengineExpression(`Exists("properties.temperature")`, model.StandardMessage{Properties: map[string]any{"temperature": 80}}); err != nil || !matched { /* 判断条件并选择处理分支。 */
		t.Fatalf("exists expression matched=%v err=%v", matched, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
