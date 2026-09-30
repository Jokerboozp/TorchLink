package ports

import (
	"context"

	"iot-platform/internal/model"
)

// History and production evaluation mutations live in the same repository as
// alarms and claims. The callback is pure apart from observing actual stage
// clocks; it cannot call this repository recursively while rows are locked.
type RuleHistoryRepository interface {
	PublishRule(context.Context, model.RulePublishRequest) (model.AlarmRuleRevision, error)
	GetRuleRevision(context.Context, string, string) (model.AlarmRuleRevision, error)
	ListRuleRevisions(context.Context, string, string, int, int) ([]model.AlarmRuleRevision, int, error)
	ListRuleActivations(context.Context, string, string, int, int) ([]model.AlarmRuleActivation, int, error)
	RuleEvaluationRules(context.Context, string, string) ([]model.AlarmRuleRevision, error)
	BeginRuleEvaluationTrace(context.Context, model.RuleEvaluationTrace) error
	CommitRuleEvaluationStep(context.Context, model.RuleTraceBinding, int, func(model.RuleEvaluationState) (model.RuleEvaluationStep, error)) (model.RuleEvaluationStep, error)
	RecordRuleRoutingTrace(context.Context, model.RuleTraceBinding, model.RuleRoutingTrace) error
	ListRuleEvaluationTraces(context.Context, string, model.RuleTraceFilter) ([]model.RuleEvaluationTrace, int, error)
}
