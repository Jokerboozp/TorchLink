package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

// RuleFieldDescription is deliberately kept outside the executable JSON.
// JSON has no standard comment syntax, so the UI receives these annotations as
// a separate, human-readable contract instead of storing _comment keys that
// the runtime might accidentally treat as rule data.
type RuleFieldDescription struct {
	Field   string `json:"field"`
	Meaning string `json:"meaning"`
	Example string `json:"example,omitempty"`
}

type RulePresentation struct {
	JSON               string                 `json:"json"`
	Gengine            string                 `json:"gengine"`
	GenginePlaceholder string                 `json:"genginePlaceholder"`
	FieldDescriptions  []RuleFieldDescription `json:"fieldDescriptions"`
}

func PresentRule(rule model.AlarmRule) (RulePresentation, error) {
	jsonText, err := json.MarshalIndent(ruleJSON(rule), "", "  ")
	if err != nil {
		return RulePresentation{}, fmt.Errorf("render rule JSON: %w", err)
	}
	gengine := RenderGengine(rule)
	return RulePresentation{
		JSON:               string(jsonText),
		Gengine:            gengine,
		GenginePlaceholder: commentedGengine(gengine),
		FieldDescriptions:  RuleFieldDescriptions(),
	}, nil
}

func ruleJSON(rule model.AlarmRule) map[string]any {
	value := map[string]any{
		"name":            rule.Name,
		"description":     rule.Description,
		"alarmType":       rule.AlarmType,
		"level":           rule.Level,
		"match":           rule.Match,
		"conditions":      rule.Conditions,
		"durationSeconds": rule.DurationSeconds,
		"recovery":        rule.Recovery,
		"actions":         rule.Actions,
		"enabled":         rule.Enabled,
	}
	if rule.ProductID != "" {
		value["productId"] = rule.ProductID
	}
	if strings.TrimSpace(rule.Expression) != "" {
		value["expression"] = rule.Expression
	}
	return value
}

// RenderGengine creates the alternative expression from the JSON condition
// list. The JSON condition form stays authoritative for an AI draft; callers
// must explicitly copy this expression into AlarmRule.Expression to enable it.
func RenderGengine(rule model.AlarmRule) string {
	if expression := strings.TrimSpace(rule.Expression); expression != "" {
		return expression
	}
	conditions := rule.Conditions
	if len(conditions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		parts = append(parts, renderCondition(condition))
	}
	separator := " && "
	if strings.EqualFold(rule.Match, "any") {
		separator = " || "
	}
	return strings.Join(parts, separator)
}

func renderCondition(condition model.RuleCondition) string {
	field := gengineField(condition.Field)
	operator := strings.ToLower(strings.TrimSpace(condition.Operator))
	if operator == "eq" {
		operator = "=="
	}
	if operator == "ne" {
		operator = "!="
	}
	if operator == "gt" {
		operator = ">"
	}
	if operator == "gte" {
		operator = ">="
	}
	if operator == "lt" {
		operator = "<"
	}
	if operator == "lte" {
		operator = "<="
	}
	if operator == "contains" {
		encoded, _ := json.Marshal(condition.Value)
		return fmt.Sprintf("Contains(%s, %s)", field, encoded)
	}
	if operator == "in" {
		return renderInCondition(field, condition.Value)
	}
	if operator == "exists" {
		return fmt.Sprintf("Exists(%s)", strconv.Quote(strings.TrimSpace(condition.Field)))
	}
	encoded, err := json.Marshal(condition.Value)
	if err != nil {
		encoded = []byte(strconv.Quote(fmt.Sprint(condition.Value)))
	}
	return fmt.Sprintf("%s %s %s", field, operator, encoded)
}

func renderInCondition(field string, value any) string {
	rv := reflect.ValueOf(value)
	if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
		if rv.Len() == 0 {
			return "false"
		}
		parts := make([]string, 0, rv.Len())
		for index := 0; index < rv.Len(); index++ {
			encoded, err := json.Marshal(rv.Index(index).Interface())
			if err != nil {
				encoded = []byte(strconv.Quote(fmt.Sprint(rv.Index(index).Interface())))
			}
			parts = append(parts, fmt.Sprintf("%s == %s", field, encoded))
		}
		return "(" + strings.Join(parts, " || ") + ")"
	}
	encoded, _ := json.Marshal(value)
	return fmt.Sprintf("%s == %s", field, encoded)
}

func gengineField(field string) string {
	field = strings.TrimSpace(field)
	for _, prefix := range []struct {
		name string
		ref  string
	}{
		{"properties.", "Properties"},
		{"tags.", "Tags"},
		{"event.", "Event"},
	} {
		if strings.HasPrefix(strings.ToLower(field), prefix.name) {
			field = field[len(prefix.name):]
			return fmt.Sprintf(`%s[%s]`, prefix.ref, strconv.Quote(field))
		}
	}
	return fmt.Sprintf(`Properties[%s]`, strconv.Quote(field))
}

func commentedGengine(expression string) string {
	if strings.TrimSpace(expression) == "" {
		return "// 暂无可转换的 Gengine 表达式；当前使用 JSON 条件。"
	}
	lines := []string{"// Gengine 表达式（默认不启用，仅作为可选替代）"}
	for _, line := range strings.Split(expression, "\n") {
		lines = append(lines, "// "+line)
	}
	return strings.Join(lines, "\n")
}

func RuleFieldDescriptions() []RuleFieldDescription {
	return []RuleFieldDescription{
		{Field: "name", Meaning: "规则名称，供运维人员识别，不参与条件计算。", Example: "高温烟雾复合告警"},
		{Field: "description", Meaning: "规则用途和现场含义说明；JSON 本身不能写注释，因此使用该字段和本说明表解释。", Example: "温度超过阈值且烟雾信号同时出现"},
		{Field: "productId", Meaning: "可选的物模型产品 ID；填写后只对该产品的设备计算。", Example: "smoke-detector-v1"},
		{Field: "alarmType", Meaning: "命中后生成的告警类型。", Example: "FIRE_RISK"},
		{Field: "level", Meaning: "告警等级：CRITICAL、HIGH、MEDIUM、LOW 或 INFO。", Example: "HIGH"},
		{Field: "match", Meaning: "条件关系：all 表示全部满足，any 表示任一满足。", Example: "all"},
		{Field: "conditions", Meaning: "触发条件数组；按 match 字段组合，至少需要一个条件或人工填写 Gengine 表达式。", Example: "[{\"field\":\"temperature\",\"operator\":\">\",\"value\":80}]"},
		{Field: "conditions[].field", Meaning: "标准消息字段；不加前缀时优先读取 properties，也可使用 properties.、tags. 或 event.。", Example: "temperature"},
		{Field: "conditions[].operator", Meaning: "比较方式：eq、ne、gt、gte、lt、lte、contains、in、exists。", Example: ">"},
		{Field: "conditions[].value", Meaning: "与设备上报值比较的目标值，类型要和物模型一致。", Example: "80"},
		{Field: "durationSeconds", Meaning: "条件连续满足多少秒后才产生告警；0 表示立即触发。", Example: "30"},
		{Field: "recovery", Meaning: "恢复条件数组；满足后关闭或恢复该规则告警。", Example: "[{\"field\":\"temperature\",\"operator\":\"lt\",\"value\":70}]"},
		{Field: "recovery[].field", Meaning: "恢复判断读取的标准消息字段，字段路径规则与 conditions[].field 相同。", Example: "temperature"},
		{Field: "recovery[].operator", Meaning: "恢复判断使用的比较方式，支持与触发条件相同的运算符。", Example: "lt"},
		{Field: "recovery[].value", Meaning: "恢复判断的目标值，类型应与设备上报值一致。", Example: "70"},
		{Field: "actions", Meaning: "告警后的前端联动数组；只允许打开已登记摄像头或平台页面，不执行设备控制。", Example: "[{\"type\":\"OPEN_CAMERA\",\"cameraId\":\"camera-001\"}]"},
		{Field: "actions[].type", Meaning: "联动类型：OPEN_CAMERA 打开登记摄像头，OPEN_PAGE 打开平台页面。", Example: "OPEN_CAMERA"},
		{Field: "actions[].cameraId", Meaning: "OPEN_CAMERA 要打开的摄像头 ID；服务端会校验摄像头已登记且属于当前租户。", Example: "camera-001"},
		{Field: "actions[].page", Meaning: "OPEN_PAGE 要打开的平台页面代码，例如 alarms；不能填写外部 URL。", Example: "alarms"},
		{Field: "expression", Meaning: "可选 Gengine 表达式。填写后运行时优先使用它；AI 草稿默认留空，避免未经人工复核切换执行方式。", Example: `Properties["temperature"] > 80`},
		{Field: "enabled", Meaning: "是否参与运行时告警计算。AI 生成草稿默认 false，必须人工确认后启用。", Example: "false"},
	}
}
