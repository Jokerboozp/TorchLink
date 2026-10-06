package repositorytest

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
)

type ruleDeleteRepository interface {
	SaveRule(context.Context, model.AlarmRule) error
	DeleteRule(context.Context, string, string) error
	SaveRulePending(context.Context, string, string, string, int64) error
	GetRulePending(context.Context, string, string, string) (int64, bool, error)
}

// RuleDeleteRemovesDurationTimers checks that deleting a rule removes its
// pending duration timers with it, so no orphan timer survives the rule.
func RuleDeleteRemovesDurationTimers(t *testing.T, repo ruleDeleteRepository) {
	t.Helper()
	ctx := context.Background()
	rule := model.AlarmRule{ID: "rule-delete-timers", TenantID: "rule-delete", ProductID: "p", Name: "持续高温", AlarmType: "TEMP", Level: "HIGH", Enabled: true, DurationSeconds: 30, Conditions: []model.RuleCondition{{Field: "t", Operator: ">", Value: 1}}}
	if err := repo.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRulePending(ctx, rule.TenantID, rule.ID, "device-1", 1000); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteRule(ctx, rule.TenantID, rule.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.GetRulePending(ctx, rule.TenantID, rule.ID, "device-1"); err != nil || found {
		t.Fatalf("timer survived its rule: found=%v err=%v", found, err)
	}
	if err := repo.DeleteRule(ctx, rule.TenantID, rule.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleting a missing rule: %v", err)
	}
}
