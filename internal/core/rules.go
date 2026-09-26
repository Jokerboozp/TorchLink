package core

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/bilibili/gengine/builder"
	gcontext "github.com/bilibili/gengine/context"
	"github.com/bilibili/gengine/engine"
	"iot-platform/internal/model"
)

func MatchRule(rule model.AlarmRule, msg model.StandardMessage) bool {
	if !rule.Enabled || rule.TenantID != "" && rule.TenantID != msg.TenantID || rule.ProductID != "" && rule.ProductID != msg.ProductID {
		return false
	}
	if strings.TrimSpace(rule.Expression) != "" {
		matched, err := EvaluateGengineExpression(rule.Expression, msg)
		return err == nil && matched
	}
	if len(rule.Conditions) == 0 {
		return false
	}
	all := !strings.EqualFold(rule.Match, "any")
	for _, c := range rule.Conditions {
		v, ok := fieldValue(msg, c.Field)
		matched := ok && compare(v, c.Operator, c.Value)
		if all && !matched {
			return false
		}
		if !all && matched {
			return true
		}
	}
	return all
}

func ValidateGengineExpression(expression string) error {
	_, err := evaluateGengine(expression, model.StandardMessage{Properties: map[string]any{"temperature": 0.0}, Tags: map[string]string{}, Event: map[string]any{}}, false)
	return err
}

func EvaluateGengineExpression(expression string, msg model.StandardMessage) (bool, error) {
	return evaluateGengine(expression, msg, true)
}

func evaluateGengine(expression string, msg model.StandardMessage, execute bool) (bool, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return false, fmt.Errorf("expression is empty")
	}
	lower := strings.ToLower(expression)
	for _, forbidden := range []string{"rule ", "begin", "end", "{", "}", ";", "import", "exec", "system"} {
		if strings.Contains(lower, forbidden) {
			return false, fmt.Errorf("expression contains forbidden token %q", forbidden)
		}
	}
	if len(expression) > 4096 {
		return false, fmt.Errorf("expression exceeds 4096 bytes")
	}
	matched := false
	dc := gcontext.NewDataContext()
	expression = bindExpressionFields(expression, dc, msg)
	dc.Add("Message", msg)
	dc.Add("MarkMatched", func() { matched = true })
	dc.Add("Contains", func(value, target any) bool {
		return strings.Contains(fmt.Sprint(value), fmt.Sprint(target))
	})
	dc.Add("Exists", func(field string) bool {
		_, ok := fieldValue(msg, field)
		return ok
	})
	rb := builder.NewRuleBuilder(dc)
	ruleText := "rule \"iot_expression\" \"controlled expression\"\nbegin\nif " + expression + " { MarkMatched() }\nend"
	if err := rb.BuildRuleFromString(ruleText); err != nil {
		return false, fmt.Errorf("compile gengine expression: %w", err)
	}
	if execute {
		if err := engine.NewGengine().Execute(rb, true); err != nil {
			return false, fmt.Errorf("execute gengine expression: %w", err)
		}
	}
	return matched, nil
}

var expressionField = regexp.MustCompile(`(Properties|Tags|Event)\[(?:"([A-Za-z0-9_.-]+)"|'([A-Za-z0-9_.-]+)')\]`)

func bindExpressionFields(expression string, dc *gcontext.DataContext, msg model.StandardMessage) string {
	bound := map[string]bool{}
	return expressionField.ReplaceAllStringFunc(expression, func(match string) string {
		parts := expressionField.FindStringSubmatch(match)
		field := parts[2]
		if field == "" {
			field = parts[3]
		}
		name := parts[1][:1] + "_" + strings.NewReplacer(".", "_", "-", "_").Replace(field)
		if !bound[name] {
			var value any = float64(0)
			switch parts[1] {
			case "Properties":
				if v, ok := msg.Properties[field]; ok {
					value = v
				}
			case "Tags":
				if v, ok := msg.Tags[field]; ok {
					value = v
				}
			case "Event":
				if v, ok := msg.Event[field]; ok {
					value = v
				}
			}
			dc.Add(name, value)
			bound[name] = true
		}
		return name
	})
}
func MatchConditions(conditions []model.RuleCondition, msg model.StandardMessage) bool {
	if len(conditions) == 0 {
		return false
	}
	for _, c := range conditions {
		v, ok := fieldValue(msg, c.Field)
		if !ok || !compare(v, c.Operator, c.Value) {
			return false
		}
	}
	return true
}
func fieldValue(msg model.StandardMessage, path string) (any, bool) {
	if strings.HasPrefix(path, "properties.") {
		v, ok := msg.Properties[strings.TrimPrefix(path, "properties.")]
		return v, ok
	}
	if strings.HasPrefix(path, "tags.") {
		v, ok := msg.Tags[strings.TrimPrefix(path, "tags.")]
		return v, ok
	}
	if strings.HasPrefix(path, "event.") {
		v, ok := msg.Event[strings.TrimPrefix(path, "event.")]
		return v, ok
	}
	if v, ok := msg.Properties[path]; ok {
		return v, true
	}
	if v, ok := msg.Tags[path]; ok {
		return v, true
	}
	if v, ok := msg.Event[path]; ok {
		return v, true
	}
	return nil, false
}
func compare(a any, op string, b any) bool {
	op = strings.ToLower(strings.TrimSpace(op))
	if op == "exists" {
		return a != nil
	}
	if op == "contains" {
		return strings.Contains(fmt.Sprint(a), fmt.Sprint(b))
	}
	if op == "in" {
		rv := reflect.ValueOf(b)
		if rv.Kind() == reflect.Slice {
			for i := 0; i < rv.Len(); i++ {
				if compare(a, "eq", rv.Index(i).Interface()) {
					return true
				}
			}
		}
		return false
	}
	af, aok := asFloat(a)
	bf, bok := asFloat(b)
	if aok && bok {
		switch op {
		case ">", "gt":
			return af > bf
		case ">=", "gte":
			return af >= bf
		case "<", "lt":
			return af < bf
		case "<=", "lte":
			return af <= bf
		case "!=", "ne":
			return af != bf
		default:
			return af == bf
		}
	}
	as := strings.ToLower(fmt.Sprint(a))
	bs := strings.ToLower(fmt.Sprint(b))
	switch op {
	case "!=", "ne":
		return as != bs
	default:
		return as == bs
	}
}
func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		n, e := strconv.ParseFloat(x, 64)
		return n, e == nil
	}
	return 0, false
}
