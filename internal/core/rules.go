package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"reflect" /* 执行当前语句并推进处理流程。 */
	"regexp"  /* 执行当前语句并推进处理流程。 */
	"strconv" /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */

	"github.com/bilibili/gengine/builder"          /* 执行当前语句并推进处理流程。 */
	gcontext "github.com/bilibili/gengine/context" /* 执行当前语句并推进处理流程。 */
	"github.com/bilibili/gengine/engine"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func MatchRule(rule model.AlarmRule, msg model.StandardMessage) bool { /* 定义 MatchRule 函数。 */
	if !rule.Enabled || rule.TenantID != "" && rule.TenantID != msg.TenantID || rule.ProductID != "" && rule.ProductID != msg.ProductID { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(rule.Expression) != "" { /* 判断条件并选择处理分支。 */
		matched, err := EvaluateGengineExpression(rule.Expression, msg) /* 更新 err 的值。 */
		return err == nil && matched                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(rule.Conditions) == 0 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	all := !strings.EqualFold(rule.Match, "any") /* 更新 all 的值。 */
	for _, c := range rule.Conditions {          /* 循环处理当前数据。 */
		v, ok := fieldValue(msg, c.Field)                /* 更新 ok 的值。 */
		matched := ok && compare(v, c.Operator, c.Value) /* 更新 matched 的值。 */
		if all && !matched {                             /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !all && matched { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return all /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ValidateGengineExpression(expression string) error { /* 定义 ValidateGengineExpression 函数。 */
	_, err := evaluateGengine(expression, model.StandardMessage{Properties: map[string]any{"temperature": 0.0}, Tags: map[string]string{}, Event: map[string]any{}}, false) /* 更新 err 的值。 */
	return err                                                                                                                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func EvaluateGengineExpression(expression string, msg model.StandardMessage) (bool, error) { /* 定义 EvaluateGengineExpression 函数。 */
	return evaluateGengine(expression, msg, true) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func evaluateGengine(expression string, msg model.StandardMessage, execute bool) (bool, error) { /* 定义 evaluateGengine 函数。 */
	expression = strings.TrimSpace(expression) /* 更新 expression 的值。 */
	if expression == "" {                      /* 判断条件并选择处理分支。 */
		return false, fmt.Errorf("expression is empty") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	lower := strings.ToLower(expression)                                                                     /* 更新 lower 的值。 */
	for _, forbidden := range []string{"rule ", "begin", "end", "{", "}", ";", "import", "exec", "system"} { /* 循环处理当前数据。 */
		if strings.Contains(lower, forbidden) { /* 判断条件并选择处理分支。 */
			return false, fmt.Errorf("expression contains forbidden token %q", forbidden) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(expression) > 4096 { /* 判断条件并选择处理分支。 */
		return false, fmt.Errorf("expression exceeds 4096 bytes") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	matched := false                                       /* 更新 matched 的值。 */
	dc := gcontext.NewDataContext()                        /* 更新 dc 的值。 */
	expression = bindExpressionFields(expression, dc, msg) /* 更新 expression 的值。 */
	dc.Add("Message", msg)                                 /* 执行当前语句并推进处理流程。 */
	dc.Add("MarkMatched", func() { matched = true })       /* 执行当前语句并推进处理流程。 */
	dc.Add("Contains", func(value, target any) bool {      /* 执行当前语句并推进处理流程。 */
		return strings.Contains(fmt.Sprint(value), fmt.Sprint(target)) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	dc.Add("Exists", func(field string) bool { /* 执行当前语句并推进处理流程。 */
		_, ok := fieldValue(msg, field) /* 更新 ok 的值。 */
		return ok                       /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	rb := builder.NewRuleBuilder(dc)                                                                                     /* 更新 rb 的值。 */
	ruleText := "rule \"iot_expression\" \"controlled expression\"\nbegin\nif " + expression + " { MarkMatched() }\nend" /* 更新 ruleText 的值。 */
	if err := rb.BuildRuleFromString(ruleText); err != nil {                                                             /* 判断条件并选择处理分支。 */
		return false, fmt.Errorf("compile gengine expression: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if execute { /* 判断条件并选择处理分支。 */
		if err := engine.NewGengine().Execute(rb, true); err != nil { /* 判断条件并选择处理分支。 */
			return false, fmt.Errorf("execute gengine expression: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return matched, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var expressionField = regexp.MustCompile(`(Properties|Tags|Event)\[(?:"([A-Za-z0-9_.-]+)"|'([A-Za-z0-9_.-]+)')\]`) /* 声明 expressionField。 */

func bindExpressionFields(expression string, dc *gcontext.DataContext, msg model.StandardMessage) string { /* 定义 bindExpressionFields 函数。 */
	bound := map[string]bool{}                                                          /* 更新 bound 的值。 */
	return expressionField.ReplaceAllStringFunc(expression, func(match string) string { /* 返回当前处理结果。 */
		parts := expressionField.FindStringSubmatch(match) /* 更新 parts 的值。 */
		field := parts[2]                                  /* 更新 field 的值。 */
		if field == "" {                                   /* 判断条件并选择处理分支。 */
			field = parts[3] /* 更新 field 的值。 */
		} /* 结束当前表达式或代码块。 */
		name := parts[1][:1] + "_" + strings.NewReplacer(".", "_", "-", "_").Replace(field) /* 更新 name 的值。 */
		if !bound[name] {                                                                   /* 判断条件并选择处理分支。 */
			var value any = float64(0) /* 声明 value。 */
			switch parts[1] {          /* 根据条件选择处理路径。 */
			case "Properties": /* 处理当前分支。 */
				if v, ok := msg.Properties[field]; ok { /* 判断条件并选择处理分支。 */
					value = v /* 更新 value 的值。 */
				} /* 结束当前表达式或代码块。 */
			case "Tags": /* 处理当前分支。 */
				if v, ok := msg.Tags[field]; ok { /* 判断条件并选择处理分支。 */
					value = v /* 更新 value 的值。 */
				} /* 结束当前表达式或代码块。 */
			case "Event": /* 处理当前分支。 */
				if v, ok := msg.Event[field]; ok { /* 判断条件并选择处理分支。 */
					value = v /* 更新 value 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			dc.Add(name, value) /* 执行当前语句并推进处理流程。 */
			bound[name] = true  /* 更新 bound[name] 的值。 */
		} /* 结束当前表达式或代码块。 */
		return name /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func MatchConditions(conditions []model.RuleCondition, msg model.StandardMessage) bool { /* 定义 MatchConditions 函数。 */
	if len(conditions) == 0 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, c := range conditions { /* 循环处理当前数据。 */
		v, ok := fieldValue(msg, c.Field)            /* 更新 ok 的值。 */
		if !ok || !compare(v, c.Operator, c.Value) { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func fieldValue(msg model.StandardMessage, path string) (any, bool) { /* 定义 fieldValue 函数。 */
	if strings.HasPrefix(path, "properties.") { /* 判断条件并选择处理分支。 */
		v, ok := msg.Properties[strings.TrimPrefix(path, "properties.")] /* 更新 ok 的值。 */
		return v, ok                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasPrefix(path, "tags.") { /* 判断条件并选择处理分支。 */
		v, ok := msg.Tags[strings.TrimPrefix(path, "tags.")] /* 更新 ok 的值。 */
		return v, ok                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasPrefix(path, "event.") { /* 判断条件并选择处理分支。 */
		v, ok := msg.Event[strings.TrimPrefix(path, "event.")] /* 更新 ok 的值。 */
		return v, ok                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v, ok := msg.Properties[path]; ok { /* 判断条件并选择处理分支。 */
		return v, true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v, ok := msg.Tags[path]; ok { /* 判断条件并选择处理分支。 */
		return v, true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v, ok := msg.Event[path]; ok { /* 判断条件并选择处理分支。 */
		return v, true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func compare(a any, op string, b any) bool { /* 定义 compare 函数。 */
	op = strings.ToLower(strings.TrimSpace(op)) /* 更新 op 的值。 */
	if op == "exists" {                         /* 判断条件并选择处理分支。 */
		return a != nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if op == "contains" { /* 判断条件并选择处理分支。 */
		return strings.Contains(fmt.Sprint(a), fmt.Sprint(b)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if op == "in" { /* 判断条件并选择处理分支。 */
		rv := reflect.ValueOf(b)        /* 更新 rv 的值。 */
		if rv.Kind() == reflect.Slice { /* 判断条件并选择处理分支。 */
			for i := 0; i < rv.Len(); i++ { /* 循环处理当前数据。 */
				if compare(a, "eq", rv.Index(i).Interface()) { /* 判断条件并选择处理分支。 */
					return true /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	af, aok := asFloat(a) /* 更新 aok 的值。 */
	bf, bok := asFloat(b) /* 更新 bok 的值。 */
	if aok && bok {       /* 判断条件并选择处理分支。 */
		switch op { /* 根据条件选择处理路径。 */
		case ">", "gt": /* 处理当前分支。 */
			return af > bf /* 返回当前处理结果。 */
		case ">=", "gte": /* 处理当前分支。 */
			return af >= bf /* 返回当前处理结果。 */
		case "<", "lt": /* 处理当前分支。 */
			return af < bf /* 返回当前处理结果。 */
		case "<=", "lte": /* 处理当前分支。 */
			return af <= bf /* 返回当前处理结果。 */
		case "!=", "ne": /* 处理当前分支。 */
			return af != bf /* 返回当前处理结果。 */
		default: /* 处理当前分支。 */
			return af == bf /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	as := strings.ToLower(fmt.Sprint(a)) /* 更新 as 的值。 */
	bs := strings.ToLower(fmt.Sprint(b)) /* 更新 bs 的值。 */
	switch op {                          /* 根据条件选择处理路径。 */
	case "!=", "ne": /* 处理当前分支。 */
		return as != bs /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return as == bs /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func asFloat(v any) (float64, bool) { /* 定义 asFloat 函数。 */
	switch x := v.(type) { /* 根据条件选择处理路径。 */
	case float64: /* 处理当前分支。 */
		return x, true /* 返回当前处理结果。 */
	case float32: /* 处理当前分支。 */
		return float64(x), true /* 返回当前处理结果。 */
	case int: /* 处理当前分支。 */
		return float64(x), true /* 返回当前处理结果。 */
	case int64: /* 处理当前分支。 */
		return float64(x), true /* 返回当前处理结果。 */
	case int32: /* 处理当前分支。 */
		return float64(x), true /* 返回当前处理结果。 */
	case bool: /* 处理当前分支。 */
		if x { /* 判断条件并选择处理分支。 */
			return 1, true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return 0, true /* 返回当前处理结果。 */
	case string: /* 处理当前分支。 */
		n, e := strconv.ParseFloat(x, 64) /* 更新 e 的值。 */
		return n, e == nil                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
