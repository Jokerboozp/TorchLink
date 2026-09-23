package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"reflect" /* 执行当前语句并推进处理流程。 */
	"regexp"  /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var ruleFieldPattern = regexp.MustCompile(`^(properties\.|tags\.|event\.)?[A-Za-z_][A-Za-z0-9_.-]*$`) /* 声明 ruleFieldPattern。 */

func (e *Engine) ValidateRuleDraft(ctx context.Context, rule model.AlarmRule) ([]string, []string, error) { /* 定义 ValidateRuleDraft 函数。 */
	if strings.TrimSpace(rule.Name) == "" || strings.TrimSpace(rule.AlarmType) == "" { /* 判断条件并选择处理分支。 */
		return nil, nil, fmt.Errorf("rule schema: name and alarmType are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !map[string]bool{"CRITICAL": true, "HIGH": true, "MEDIUM": true, "LOW": true, "INFO": true}[strings.ToUpper(rule.Level)] { /* 判断条件并选择处理分支。 */
		return nil, nil, fmt.Errorf("rule schema: invalid level %q", rule.Level) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if rule.Match != "" && !strings.EqualFold(rule.Match, "all") && !strings.EqualFold(rule.Match, "any") { /* 判断条件并选择处理分支。 */
		return nil, nil, fmt.Errorf("rule schema: match must be all or any") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if rule.DurationSeconds < 0 { /* 判断条件并选择处理分支。 */
		return nil, nil, fmt.Errorf("rule schema: durationSeconds cannot be negative") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(rule.Actions) > 4 { /* 判断条件并选择处理分支。 */
		return nil, nil, fmt.Errorf("rule actions cannot exceed 4") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	allowedPages := map[string]bool{ /* 更新 allowedPages 的值。 */
		"dashboard":         true, /* 执行当前语句并推进处理流程。 */
		"devices":           true, /* 执行当前语句并推进处理流程。 */
		"products":          true, /* 执行当前语句并推进处理流程。 */
		"protocols":         true, /* 执行当前语句并推进处理流程。 */
		"protocolassistant": true, /* 执行当前语句并推进处理流程。 */
		"integration":       true, /* 执行当前语句并推进处理流程。 */
		"cameras":           true, /* 执行当前语句并推进处理流程。 */
		"alarms":            true, /* 执行当前语句并推进处理流程。 */
		"inspection":        true, /* 执行当前语句并推进处理流程。 */
		"raw":               true, /* 执行当前语句并推进处理流程。 */
		"rules":             true, /* 执行当前语句并推进处理流程。 */
		"knowledge":         true, /* 执行当前语句并推进处理流程。 */
		"ai":                true, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, action := range rule.Actions { /* 循环处理当前数据。 */
		switch strings.ToUpper(strings.TrimSpace(action.Type)) { /* 根据条件选择处理路径。 */
		case "OPEN_CAMERA": /* 处理当前分支。 */
			cameraID := strings.TrimSpace(action.CameraID) /* 更新 cameraID 的值。 */
			if cameraID == "" {                            /* 判断条件并选择处理分支。 */
				return nil, nil, fmt.Errorf("OPEN_CAMERA action requires cameraId") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			camera, err := e.Repo.GetVideoCameraMapping(ctx, rule.TenantID, cameraID) /* 更新 err 的值。 */
			if err != nil || !camera.Enabled {                                        /* 判断条件并选择处理分支。 */
				return nil, nil, fmt.Errorf("camera %q is not available for metadata lookup", cameraID) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		case "OPEN_PAGE": /* 处理当前分支。 */
			if !allowedPages[strings.ToLower(strings.TrimSpace(action.Page))] { /* 判断条件并选择处理分支。 */
				return nil, nil, fmt.Errorf("OPEN_PAGE action page %q is not allowed", action.Page) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		default: /* 处理当前分支。 */
			return nil, nil, fmt.Errorf("rule action type %q is not allowed", action.Type) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(rule.Conditions) == 0 && strings.TrimSpace(rule.Expression) == "" { /* 判断条件并选择处理分支。 */
		return nil, nil, fmt.Errorf("rule schema: conditions or expression is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	allowedOps := map[string]bool{"eq": true, "==": true, "ne": true, "!=": true, ">": true, "gt": true, ">=": true, "gte": true, "<": true, "lt": true, "<=": true, "lte": true, "contains": true, "in": true, "exists": true} /* 更新 allowedOps 的值。 */
	for _, c := range append(append([]model.RuleCondition{}, rule.Conditions...), rule.Recovery...) {                                                                                                                           /* 循环处理当前数据。 */
		if !ruleFieldPattern.MatchString(c.Field) { /* 判断条件并选择处理分支。 */
			return nil, nil, fmt.Errorf("thing-model field %q is invalid", c.Field) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !allowedOps[strings.ToLower(c.Operator)] { /* 判断条件并选择处理分支。 */
			return nil, nil, fmt.Errorf("operator %q is not allowed", c.Operator) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if rule.Expression != "" { /* 判断条件并选择处理分支。 */
		if err := ValidateGengineExpression(rule.Expression); err != nil { /* 判断条件并选择处理分支。 */
			return nil, nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	warnings := []string{}    /* 更新 warnings 的值。 */
	if rule.ProductID != "" { /* 判断条件并选择处理分支。 */
		product, err := e.Repo.GetProduct(ctx, rule.TenantID, rule.ProductID) /* 更新 err 的值。 */
		if err != nil {                                                       /* 判断条件并选择处理分支。 */
			return nil, nil, fmt.Errorf("thing-model product %q does not exist", rule.ProductID) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		fields := productFields(product) /* 更新 fields 的值。 */
		if len(fields) > 0 {             /* 判断条件并选择处理分支。 */
			for _, c := range rule.Conditions { /* 循环处理当前数据。 */
				field := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(c.Field, "properties."), "tags."), "event.") /* 更新 field 的值。 */
				if !fields[field] {                                                                                            /* 判断条件并选择处理分支。 */
					return nil, nil, fmt.Errorf("thing-model field %q is not declared by product %s", field, product.ID) /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} else { /* 结束当前表达式或代码块。 */
			warnings = append(warnings, "产品未声明物模型字段，已完成字段语法校验，启用前需人工核对") /* 更新 warnings 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	existing, err := e.Repo.ListRules(ctx, rule.TenantID) /* 更新 err 的值。 */
	if err != nil {                                       /* 判断条件并选择处理分支。 */
		return nil, nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	conflicts := []string{}          /* 更新 conflicts 的值。 */
	for _, other := range existing { /* 循环处理当前数据。 */
		if other.ID == rule.ID || other.ProductID != rule.ProductID { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if other.Expression == rule.Expression && rule.Expression != "" || reflect.DeepEqual(other.Conditions, rule.Conditions) { /* 判断条件并选择处理分支。 */
			conflicts = append(conflicts, fmt.Sprintf("与规则 %s(%s) 的触发条件重复", other.Name, other.ID)) /* 更新 conflicts 的值。 */
		} /* 结束当前表达式或代码块。 */
		if other.AlarmType == rule.AlarmType && other.Level != rule.Level && reflect.DeepEqual(other.Conditions, rule.Conditions) { /* 判断条件并选择处理分支。 */
			conflicts = append(conflicts, fmt.Sprintf("与规则 %s(%s) 的等级配置冲突", other.Name, other.ID)) /* 更新 conflicts 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return warnings, conflicts, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func productFields(product model.Product) map[string]bool { /* 定义 productFields 函数。 */
	out := map[string]bool{}                                            /* 更新 out 的值。 */
	for _, key := range []string{"properties", "fields", "telemetry"} { /* 循环处理当前数据。 */
		switch v := product.Metadata[key].(type) { /* 根据条件选择处理路径。 */
		case []any: /* 处理当前分支。 */
			for _, item := range v { /* 循环处理当前数据。 */
				switch x := item.(type) { /* 根据条件选择处理路径。 */
				case string: /* 处理当前分支。 */
					out[x] = true /* 更新 out[x] 的值。 */
				case map[string]any: /* 处理当前分支。 */
					if id, ok := x["id"].(string); ok { /* 判断条件并选择处理分支。 */
						out[id] = true /* 更新 out[id] 的值。 */
					} /* 结束当前表达式或代码块。 */
					if code, ok := x["code"].(string); ok { /* 判断条件并选择处理分支。 */
						out[code] = true /* 更新 out[code] 的值。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		case map[string]any: /* 处理当前分支。 */
			for field := range v { /* 循环处理当前数据。 */
				out[field] = true /* 更新 out[field] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
