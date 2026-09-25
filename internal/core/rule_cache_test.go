package core

import (
	"context"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

func TestTenantRulesAreCachedUntilChanged(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	e := &Engine{Repo: repo}
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r1", TenantID: "t1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if rules, err := e.tenantRules(ctx, "t1"); err != nil || len(rules) != 1 {
		t.Fatalf("first load: %v %v", rules, err)
	}
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r2", TenantID: "t1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if rules, _ := e.tenantRules(ctx, "t1"); len(rules) != 1 {
		t.Fatalf("rules must come from the cache within the TTL, got %d", len(rules))
	}
	e.RulesChanged("t1")
	if rules, _ := e.tenantRules(ctx, "t1"); len(rules) != 2 {
		t.Fatalf("a change must be visible immediately after RulesChanged, got %d", len(rules))
	}
	if rules, _ := e.tenantRules(ctx, "t2"); len(rules) != 0 {
		t.Fatalf("tenants must not share cached rules: %v", rules)
	}
}
