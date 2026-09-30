package eval

import "iot-platform/internal/model"

// TransitionRevision preserves v1 predicates, stage clocks and lifecycle
// priority. Its explicit v2 policy uses a revision-scoped pending supplied by
// the executor and the producing revision's recovery. Administrative disable
// or deletion retains open alarms; a disabled tombstone can still recover them.
// A legacy alarm without a producing revision must be marked UNKNOWN by the
// caller; it is never silently attributed to the current rule revision.
func TransitionRevision(revision model.AlarmRuleRevision, msg model.StandardMessage, state RuleState, recovery *model.AlarmRuleRevision, times StageTimes) (Decision, error) {
	rule := revision.Rule
	if recovery != nil {
		rule.Recovery = recovery.Rule.Recovery
		if !Covers(rule, msg) && Covers(recovery.Rule, msg) {
			rule.Enabled = false
			rule.ProductID = recovery.Rule.ProductID
		}
	}
	decision, err := Transition(rule, msg, state, times)
	decision.SemanticsVersion = RevisionV2
	if err == nil && decision.WriteAlarm && decision.Matched {
		if decision.Created {
			decision.Alarm.CreatedRuleRevision = revision.ID
		}
		decision.Alarm.TriggerRuleRevision = revision.ID
	}
	return decision, err
}
