package core

import (
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
)

func ruleCovers(rule model.AlarmRule, msg model.StandardMessage) bool { return eval.Covers(rule, msg) }
func MatchRule(rule model.AlarmRule, msg model.StandardMessage) bool {
	matched, err := eval.Match(rule, msg)
	return err == nil && matched
}
func MatchConditions(conditions []model.RuleCondition, msg model.StandardMessage) bool {
	return eval.MatchConditions(conditions, msg)
}
func ValidateGengineExpression(expression string) error { return eval.ValidateExpression(expression) }
func EvaluateGengineExpression(expression string, msg model.StandardMessage) (bool, error) {
	return eval.EvaluateExpression(expression, msg)
}
