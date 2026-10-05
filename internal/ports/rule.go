package ports

import (
	"context"

	"iot-platform/internal/model"
)

// RuleStore keeps alarm rules and the pending duration timers of their
// conditions.
type RuleStore interface {
	SaveRule(context.Context, model.AlarmRule) error
	ListRules(context.Context, string) ([]model.AlarmRule, error)
	ListRulesPage(context.Context, string, int, int) ([]model.AlarmRule, int, error)
	DeleteRule(context.Context, string, string) error
	SaveRulePending(context.Context, string, string, string, int64) error
	GetRulePending(context.Context, string, string, string) (int64, bool, error)
	DeleteRulePending(context.Context, string, string, string) error
	DeleteRulePendings(context.Context, string, string) error
}
