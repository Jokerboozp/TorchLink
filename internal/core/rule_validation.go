package core

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"iot-platform/internal/model"
)

var ruleFieldPattern = regexp.MustCompile(`^(properties\.|tags\.|event\.)?[A-Za-z_][A-Za-z0-9_.-]*$`)

func (e *Engine) ValidateRuleDraft(ctx context.Context, rule model.AlarmRule) ([]string, []string, error) {
	if strings.TrimSpace(rule.Name) == "" || strings.TrimSpace(rule.AlarmType) == "" {
		return nil, nil, fmt.Errorf("rule schema: name and alarmType are required")
	}
	if !map[string]bool{"CRITICAL": true, "HIGH": true, "MEDIUM": true, "LOW": true, "INFO": true}[strings.ToUpper(rule.Level)] {
		return nil, nil, fmt.Errorf("rule schema: invalid level %q", rule.Level)
	}
	if rule.Match != "" && !strings.EqualFold(rule.Match, "all") && !strings.EqualFold(rule.Match, "any") {
		return nil, nil, fmt.Errorf("rule schema: match must be all or any")
	}
	if rule.DurationSeconds < 0 {
		return nil, nil, fmt.Errorf("rule schema: durationSeconds cannot be negative")
	}
	if len(rule.Actions) > 4 {
		return nil, nil, fmt.Errorf("rule actions cannot exceed 4")
	}
	allowedPages := map[string]bool{
		"dashboard":         true,
		"devices":           true,
		"products":          true,
		"protocols":         true,
		"protocolassistant": true,
		"integration":       true,
		"cameras":           true,
		"alarms":            true,
		"inspection":        true,
		"raw":               true,
		"rules":             true,
		"knowledge":         true,
		"ai":                true,
	}
	for _, action := range rule.Actions {
		switch strings.ToUpper(strings.TrimSpace(action.Type)) {
		case "OPEN_CAMERA":
			cameraID := strings.TrimSpace(action.CameraID)
			if cameraID == "" {
				return nil, nil, fmt.Errorf("OPEN_CAMERA action requires cameraId")
			}
			camera, err := e.Repo.GetVideoCameraMapping(ctx, rule.TenantID, cameraID)
			if err != nil || !camera.Enabled {
				return nil, nil, fmt.Errorf("camera %q is not available for metadata lookup", cameraID)
			}
		case "OPEN_PAGE":
			if !allowedPages[strings.ToLower(strings.TrimSpace(action.Page))] {
				return nil, nil, fmt.Errorf("OPEN_PAGE action page %q is not allowed", action.Page)
			}
		default:
			return nil, nil, fmt.Errorf("rule action type %q is not allowed", action.Type)
		}
	}
	if len(rule.Conditions) == 0 && strings.TrimSpace(rule.Expression) == "" {
		return nil, nil, fmt.Errorf("rule schema: conditions or expression is required")
	}
	allowedOps := map[string]bool{"eq": true, "==": true, "ne": true, "!=": true, ">": true, "gt": true, ">=": true, "gte": true, "<": true, "lt": true, "<=": true, "lte": true, "contains": true, "in": true, "exists": true}
	for _, c := range append(append([]model.RuleCondition{}, rule.Conditions...), rule.Recovery...) {
		if !ruleFieldPattern.MatchString(c.Field) {
			return nil, nil, fmt.Errorf("thing-model field %q is invalid", c.Field)
		}
		if !allowedOps[strings.ToLower(c.Operator)] {
			return nil, nil, fmt.Errorf("operator %q is not allowed", c.Operator)
		}
	}
	if rule.Expression != "" {
		if err := ValidateGengineExpression(rule.Expression); err != nil {
			return nil, nil, err
		}
	}
	warnings := []string{}
	if rule.ProductID != "" {
		product, err := e.Repo.GetProduct(ctx, rule.TenantID, rule.ProductID)
		if err != nil {
			return nil, nil, fmt.Errorf("thing-model product %q does not exist", rule.ProductID)
		}
		fields := productFields(product)
		if len(fields) > 0 {
			for _, c := range rule.Conditions {
				field := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(c.Field, "properties."), "tags."), "event.")
				if !fields[field] {
					return nil, nil, fmt.Errorf("thing-model field %q is not declared by product %s", field, product.ID)
				}
			}
		} else {
			warnings = append(warnings, "产品未声明物模型字段，已完成字段语法校验，启用前需人工核对")
		}
	}
	existing, err := e.Repo.ListRules(ctx, rule.TenantID)
	if err != nil {
		return nil, nil, err
	}
	conflicts := []string{}
	for _, other := range existing {
		if other.ID == rule.ID || other.ProductID != rule.ProductID {
			continue
		}
		if other.Expression == rule.Expression && rule.Expression != "" || reflect.DeepEqual(other.Conditions, rule.Conditions) {
			conflicts = append(conflicts, fmt.Sprintf("与规则 %s(%s) 的触发条件重复", other.Name, other.ID))
		}
		if other.AlarmType == rule.AlarmType && other.Level != rule.Level && reflect.DeepEqual(other.Conditions, rule.Conditions) {
			conflicts = append(conflicts, fmt.Sprintf("与规则 %s(%s) 的等级配置冲突", other.Name, other.ID))
		}
	}
	return warnings, conflicts, nil
}

func productFields(product model.Product) map[string]bool {
	out := map[string]bool{}
	for _, key := range []string{"properties", "fields", "telemetry"} {
		switch v := product.Metadata[key].(type) {
		case []any:
			for _, item := range v {
				switch x := item.(type) {
				case string:
					out[x] = true
				case map[string]any:
					if id, ok := x["id"].(string); ok {
						out[id] = true
					}
					if code, ok := x["code"].(string); ok {
						out[code] = true
					}
				}
			}
		case map[string]any:
			for field := range v {
				out[field] = true
			}
		}
	}
	return out
}
