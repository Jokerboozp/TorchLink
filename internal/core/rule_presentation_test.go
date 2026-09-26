package core

import (
	"encoding/json"
	"strings"
	"testing"

	"iot-platform/internal/model"
)

func TestPresentRuleKeepsJSONExecutableAndGengineCommentedByDefault(t *testing.T) {
	rule := model.AlarmRule{
		Name:        "高温烟雾",
		Description: "温度过高且烟雾信号出现时告警。",
		AlarmType:   "FIRE_RISK",
		Level:       "HIGH",
		Match:       "all",
		Conditions: []model.RuleCondition{
			{Field: "temperature", Operator: ">", Value: 80},
			{Field: "smoke", Operator: "eq", Value: true},
		},
		Enabled: false,
	}
	presentation, err := PresentRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	var executable map[string]any
	if err = json.Unmarshal([]byte(presentation.JSON), &executable); err != nil {
		t.Fatal(err)
	}
	if executable["description"] != rule.Description || strings.Contains(presentation.JSON, "_comment") {
		t.Fatalf("unexpected JSON presentation: %s", presentation.JSON)
	}
	if presentation.Gengine != `Properties["temperature"] > 80 && Properties["smoke"] == true` {
		t.Fatalf("unexpected Gengine: %q", presentation.Gengine)
	}
	if !strings.HasPrefix(presentation.GenginePlaceholder, "//") || !strings.Contains(presentation.GenginePlaceholder, presentation.Gengine) {
		t.Fatalf("Gengine is not commented in placeholder: %q", presentation.GenginePlaceholder)
	}
	for _, required := range []string{"conditions", "recovery[].field", "recovery[].operator", "recovery[].value", "actions[].type", "actions[].cameraId", "actions[].page"} {
		found := false
		for _, description := range presentation.FieldDescriptions {
			if description.Field == required && strings.TrimSpace(description.Meaning) != "" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing field description for %q", required)
		}
	}
}

func TestGeneratedGengineSpecialOperatorsValidateAndEvaluate(t *testing.T) {
	rule := model.AlarmRule{Conditions: []model.RuleCondition{
		{Field: "smokeText", Operator: "contains", Value: "smoke"},
		{Field: "temperature", Operator: "in", Value: []any{70, 80}},
		{Field: "properties.temperature", Operator: "exists"},
	}}
	// Validate each generated condition separately so the test also documents
	// the supported expression snippets exposed by the rule editor.
	for _, condition := range rule.Conditions {
		expression := RenderGengine(model.AlarmRule{Conditions: []model.RuleCondition{condition}})
		if err := ValidateGengineExpression(expression); err != nil {
			t.Fatalf("generated expression %q did not compile: %v", expression, err)
		}
	}
	if matched, err := EvaluateGengineExpression(`Contains(Properties["smokeText"], "smoke")`, model.StandardMessage{Properties: map[string]any{"smokeText": "smoke detected"}}); err != nil || !matched {
		t.Fatalf("contains expression matched=%v err=%v", matched, err)
	}
	if matched, err := EvaluateGengineExpression(`Exists("properties.temperature")`, model.StandardMessage{Properties: map[string]any{"temperature": 80}}); err != nil || !matched {
		t.Fatalf("exists expression matched=%v err=%v", matched, err)
	}
}
