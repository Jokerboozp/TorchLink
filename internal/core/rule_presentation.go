package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"reflect"       /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// RuleFieldDescription is deliberately kept outside the executable JSON.
// JSON has no standard comment syntax, so the UI receives these annotations as
// a separate, human-readable contract instead of storing _comment keys that
// the runtime might accidentally treat as rule data.
type RuleFieldDescription struct { /* 定义 RuleFieldDescription 类型。 */
	Field   string `json:"field"`             /* 执行当前语句并推进处理流程。 */
	Meaning string `json:"meaning"`           /* 执行当前语句并推进处理流程。 */
	Example string `json:"example,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type RulePresentation struct { /* 定义 RulePresentation 类型。 */
	JSON               string                 `json:"json"`               /* 执行当前语句并推进处理流程。 */
	Gengine            string                 `json:"gengine"`            /* 执行当前语句并推进处理流程。 */
	GenginePlaceholder string                 `json:"genginePlaceholder"` /* 执行当前语句并推进处理流程。 */
	FieldDescriptions  []RuleFieldDescription `json:"fieldDescriptions"`  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func PresentRule(rule model.AlarmRule) (RulePresentation, error) { /* 定义 PresentRule 函数。 */
	jsonText, err := json.MarshalIndent(ruleJSON(rule), "", "  ") /* 更新 err 的值。 */
	if err != nil {                                               /* 判断条件并选择处理分支。 */
		return RulePresentation{}, fmt.Errorf("render rule JSON: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	gengine := RenderGengine(rule) /* 更新 gengine 的值。 */
	return RulePresentation{       /* 返回当前处理结果。 */
		JSON:               string(jsonText),          /* 执行当前语句并推进处理流程。 */
		Gengine:            gengine,                   /* 执行当前语句并推进处理流程。 */
		GenginePlaceholder: commentedGengine(gengine), /* 执行当前语句并推进处理流程。 */
		FieldDescriptions:  RuleFieldDescriptions(),   /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func ruleJSON(rule model.AlarmRule) map[string]any { /* 定义 ruleJSON 函数。 */
	value := map[string]any{ /* 更新 value 的值。 */
		"name":            rule.Name,            /* 执行当前语句并推进处理流程。 */
		"description":     rule.Description,     /* 执行当前语句并推进处理流程。 */
		"alarmType":       rule.AlarmType,       /* 执行当前语句并推进处理流程。 */
		"level":           rule.Level,           /* 执行当前语句并推进处理流程。 */
		"match":           rule.Match,           /* 执行当前语句并推进处理流程。 */
		"conditions":      rule.Conditions,      /* 执行当前语句并推进处理流程。 */
		"durationSeconds": rule.DurationSeconds, /* 执行当前语句并推进处理流程。 */
		"recovery":        rule.Recovery,        /* 执行当前语句并推进处理流程。 */
		"actions":         rule.Actions,         /* 执行当前语句并推进处理流程。 */
		"enabled":         rule.Enabled,         /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if rule.ProductID != "" { /* 判断条件并选择处理分支。 */
		value["productId"] = rule.ProductID /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(rule.Expression) != "" { /* 判断条件并选择处理分支。 */
		value["expression"] = rule.Expression /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// RenderGengine creates the alternative expression from the JSON condition
// list. The JSON condition form stays authoritative for an AI draft; callers
// must explicitly copy this expression into AlarmRule.Expression to enable it.
func RenderGengine(rule model.AlarmRule) string { /* 定义 RenderGengine 函数。 */
	if expression := strings.TrimSpace(rule.Expression); expression != "" { /* 判断条件并选择处理分支。 */
		return expression /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	conditions := rule.Conditions /* 更新 conditions 的值。 */
	if len(conditions) == 0 {     /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parts := make([]string, 0, len(conditions)) /* 更新 parts 的值。 */
	for _, condition := range conditions {      /* 循环处理当前数据。 */
		parts = append(parts, renderCondition(condition)) /* 更新 parts 的值。 */
	} /* 结束当前表达式或代码块。 */
	separator := " && "                       /* 更新 separator 的值。 */
	if strings.EqualFold(rule.Match, "any") { /* 判断条件并选择处理分支。 */
		separator = " || " /* 更新 separator 的值。 */
	} /* 结束当前表达式或代码块。 */
	return strings.Join(parts, separator) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func renderCondition(condition model.RuleCondition) string { /* 定义 renderCondition 函数。 */
	field := gengineField(condition.Field)                             /* 更新 field 的值。 */
	operator := strings.ToLower(strings.TrimSpace(condition.Operator)) /* 更新 operator 的值。 */
	if operator == "eq" {                                              /* 判断条件并选择处理分支。 */
		operator = "==" /* 更新 operator 的值。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "ne" { /* 判断条件并选择处理分支。 */
		operator = "!=" /* 更新 operator 的值。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "gt" { /* 判断条件并选择处理分支。 */
		operator = ">" /* 更新 operator 的值。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "gte" { /* 判断条件并选择处理分支。 */
		operator = ">=" /* 更新 operator 的值。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "lt" { /* 判断条件并选择处理分支。 */
		operator = "<" /* 更新 operator 的值。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "lte" { /* 判断条件并选择处理分支。 */
		operator = "<=" /* 更新 operator 的值。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "contains" { /* 判断条件并选择处理分支。 */
		encoded, _ := json.Marshal(condition.Value)            /* 更新 _ 的值。 */
		return fmt.Sprintf("Contains(%s, %s)", field, encoded) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "in" { /* 判断条件并选择处理分支。 */
		return renderInCondition(field, condition.Value) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if operator == "exists" { /* 判断条件并选择处理分支。 */
		return fmt.Sprintf("Exists(%s)", strconv.Quote(strings.TrimSpace(condition.Field))) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	encoded, err := json.Marshal(condition.Value) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		encoded = []byte(strconv.Quote(fmt.Sprint(condition.Value))) /* 更新 encoded 的值。 */
	} /* 结束当前表达式或代码块。 */
	return fmt.Sprintf("%s %s %s", field, operator, encoded) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func renderInCondition(field string, value any) string { /* 定义 renderInCondition 函数。 */
	rv := reflect.ValueOf(value)                                                    /* 更新 rv 的值。 */
	if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) { /* 判断条件并选择处理分支。 */
		if rv.Len() == 0 { /* 判断条件并选择处理分支。 */
			return "false" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		parts := make([]string, 0, rv.Len())        /* 更新 parts 的值。 */
		for index := 0; index < rv.Len(); index++ { /* 循环处理当前数据。 */
			encoded, err := json.Marshal(rv.Index(index).Interface()) /* 更新 err 的值。 */
			if err != nil {                                           /* 判断条件并选择处理分支。 */
				encoded = []byte(strconv.Quote(fmt.Sprint(rv.Index(index).Interface()))) /* 更新 encoded 的值。 */
			} /* 结束当前表达式或代码块。 */
			parts = append(parts, fmt.Sprintf("%s == %s", field, encoded)) /* 更新 parts 的值。 */
		} /* 结束当前表达式或代码块。 */
		return "(" + strings.Join(parts, " || ") + ")" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	encoded, _ := json.Marshal(value)              /* 更新 _ 的值。 */
	return fmt.Sprintf("%s == %s", field, encoded) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func gengineField(field string) string { /* 定义 gengineField 函数。 */
	field = strings.TrimSpace(field)  /* 更新 field 的值。 */
	for _, prefix := range []struct { /* 循环处理当前数据。 */
		name string /* 执行当前语句并推进处理流程。 */
		ref  string /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{"properties.", "Properties"}, /* 执行当前语句并推进处理流程。 */
		{"tags.", "Tags"},             /* 执行当前语句并推进处理流程。 */
		{"event.", "Event"},           /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if strings.HasPrefix(strings.ToLower(field), prefix.name) { /* 判断条件并选择处理分支。 */
			field = field[len(prefix.name):]                               /* 更新 field 的值。 */
			return fmt.Sprintf(`%s[%s]`, prefix.ref, strconv.Quote(field)) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return fmt.Sprintf(`Properties[%s]`, strconv.Quote(field)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func commentedGengine(expression string) string { /* 定义 commentedGengine 函数。 */
	if strings.TrimSpace(expression) == "" { /* 判断条件并选择处理分支。 */
		return "// 暂无可转换的 Gengine 表达式；当前使用 JSON 条件。" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	lines := []string{"// Gengine 表达式（默认不启用，仅作为可选替代）"}     /* 更新 lines 的值。 */
	for _, line := range strings.Split(expression, "\n") { /* 循环处理当前数据。 */
		lines = append(lines, "// "+line) /* 更新 lines 的值。 */
	} /* 结束当前表达式或代码块。 */
	return strings.Join(lines, "\n") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func RuleFieldDescriptions() []RuleFieldDescription { /* 定义 RuleFieldDescriptions 函数。 */
	return []RuleFieldDescription{ /* 返回当前处理结果。 */
		{Field: "name", Meaning: "规则名称，供运维人员识别，不参与条件计算。", Example: "高温烟雾复合告警"},                                                                                     /* 执行当前语句并推进处理流程。 */
		{Field: "description", Meaning: "规则用途和现场含义说明；JSON 本身不能写注释，因此使用该字段和本说明表解释。", Example: "温度超过阈值且烟雾信号同时出现"},                                                    /* 执行当前语句并推进处理流程。 */
		{Field: "productId", Meaning: "可选的物模型产品 ID；填写后只对该产品的设备计算。", Example: "smoke-detector-v1"},                                                                  /* 执行当前语句并推进处理流程。 */
		{Field: "alarmType", Meaning: "命中后生成的告警类型。", Example: "FIRE_RISK"},                                                                                         /* 执行当前语句并推进处理流程。 */
		{Field: "level", Meaning: "告警等级：CRITICAL、HIGH、MEDIUM、LOW 或 INFO。", Example: "HIGH"},                                                                        /* 执行当前语句并推进处理流程。 */
		{Field: "match", Meaning: "条件关系：all 表示全部满足，any 表示任一满足。", Example: "all"},                                                                                   /* 执行当前语句并推进处理流程。 */
		{Field: "conditions", Meaning: "触发条件数组；按 match 字段组合，至少需要一个条件或人工填写 Gengine 表达式。", Example: "[{\"field\":\"temperature\",\"operator\":\">\",\"value\":80}]"}, /* 执行当前语句并推进处理流程。 */
		{Field: "conditions[].field", Meaning: "标准消息字段；不加前缀时优先读取 properties，也可使用 properties.、tags. 或 event.。", Example: "temperature"},                             /* 执行当前语句并推进处理流程。 */
		{Field: "conditions[].operator", Meaning: "比较方式：eq、ne、gt、gte、lt、lte、contains、in、exists。", Example: ">"},                                                    /* 执行当前语句并推进处理流程。 */
		{Field: "conditions[].value", Meaning: "与设备上报值比较的目标值，类型要和物模型一致。", Example: "80"},                                                                           /* 执行当前语句并推进处理流程。 */
		{Field: "durationSeconds", Meaning: "条件连续满足多少秒后才产生告警；0 表示立即触发。", Example: "30"},                                                                            /* 执行当前语句并推进处理流程。 */
		{Field: "recovery", Meaning: "恢复条件数组；满足后关闭或恢复该规则告警。", Example: "[{\"field\":\"temperature\",\"operator\":\"lt\",\"value\":70}]"},                           /* 执行当前语句并推进处理流程。 */
		{Field: "recovery[].field", Meaning: "恢复判断读取的标准消息字段，字段路径规则与 conditions[].field 相同。", Example: "temperature"},                                               /* 执行当前语句并推进处理流程。 */
		{Field: "recovery[].operator", Meaning: "恢复判断使用的比较方式，支持与触发条件相同的运算符。", Example: "lt"},                                                                       /* 执行当前语句并推进处理流程。 */
		{Field: "recovery[].value", Meaning: "恢复判断的目标值，类型应与设备上报值一致。", Example: "70"},                                                                               /* 执行当前语句并推进处理流程。 */
		{Field: "actions", Meaning: "告警后的前端联动数组；只允许打开已登记摄像头或平台页面，不执行设备控制。", Example: "[{\"type\":\"OPEN_CAMERA\",\"cameraId\":\"camera-001\"}]"},                   /* 执行当前语句并推进处理流程。 */
		{Field: "actions[].type", Meaning: "联动类型：OPEN_CAMERA 打开登记摄像头，OPEN_PAGE 打开平台页面。", Example: "OPEN_CAMERA"},                                                   /* 执行当前语句并推进处理流程。 */
		{Field: "actions[].cameraId", Meaning: "OPEN_CAMERA 要打开的摄像头 ID；服务端会校验摄像头已登记且属于当前租户。", Example: "camera-001"},                                               /* 执行当前语句并推进处理流程。 */
		{Field: "actions[].page", Meaning: "OPEN_PAGE 要打开的平台页面代码，例如 alarms；不能填写外部 URL。", Example: "alarms"},                                                        /* 执行当前语句并推进处理流程。 */
		{Field: "expression", Meaning: "可选 Gengine 表达式。填写后运行时优先使用它；AI 草稿默认留空，避免未经人工复核切换执行方式。", Example: `Properties["temperature"] > 80`},                          /* 执行当前语句并推进处理流程。 */
		{Field: "enabled", Meaning: "是否参与运行时告警计算。AI 生成草稿默认 false，必须人工确认后启用。", Example: "false"},                                                                    /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
