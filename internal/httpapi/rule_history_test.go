package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestRuleHistoryAPITenantScopeAndStaleSaveCAS(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if e := repo.SaveProduct(ctx, model.Product{ID: "p", TenantID: "t", Name: "产品", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "flow", DataType: "number"}}}}); e != nil {
		t.Fatal(e)
	}
	baseline := model.AlarmRule{ID: "rule", TenantID: "t", ProductID: "p", Name: "流量规则", AlarmType: "FIRE", Level: "MEDIUM", Enabled: true, Match: "all", Conditions: []model.RuleCondition{{Field: "flow", Operator: "gt", Value: 50}}}
	if e := repo.SaveRule(ctx, baseline); e != nil {
		t.Fatal(e)
	}
	rules, e := repo.ListRules(ctx, "t")
	if e != nil {
		t.Fatal(e)
	}
	baseline = rules[0]
	state := model.AccessState{Users: []model.PlatformUser{{Username: "limited", Enabled: true, SessionVersion: 1, Permissions: []string{"menu:rules", "menu:devices", "POST /api/v1/rules/:id/publish"}, DeviceScope: "selected", DeviceIDs: []string{"d1"}}}}
	if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
		t.Fatal(ok, e)
	}
	cfg := config.Config{AdminUser: "admin", AdminTenants: []string{"t", "other"}, JWTSecret: "rule-history-test-secret-at-least-32-characters", DevMode: true}
	api := New(cfg, core.New(repo, nil, nil, nil, nil, nil), metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	admin, e := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	other, e := api.auth.Issue("admin", "other", "admin", nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	limited, e := api.auth.IssueUser("limited", "t", 1, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"revisions", "activations"} {
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rules/rule/"+suffix, admin, nil, 200)
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rules/rule/"+suffix, other, nil, 404)
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rules/rule/"+suffix, limited, nil, 403)
	}
	updated := baseline
	updated.Name = "新版本"
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/rules/rule", admin, updated, 200)
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/rules/rule", admin, baseline, 409)
	history, n, e := repo.ListRuleRevisions(ctx, "t", "rule", 100, 0)
	if e != nil || n != 2 || history[1].Rule.Name != baseline.Name || history[0].Version != baseline.Version+1 {
		t.Fatal(history, n, e)
	}
	for _, q := range []map[string]any{{"rule": updated, "reason": "人工核实"}, {"rule": updated, "experimentId": "unknown", "reason": "人工核实"}, {"rule": updated, "expectedBaselineVersion": history[0].Version, "experimentId": "unknown", "reason": ""}} {
		requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rules/rule/publish", admin, q, 422)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rules/rule/publish", limited, map[string]any{"rule": updated, "expectedBaselineVersion": history[0].Version, "experimentId": "unknown", "reason": "人工核实"}, 403)
}

func TestRulePolicyAIAPIExperimentBindingHistoryAndRevocation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	permission := []string{"menu:devices", "menu:ruleLab", analytics.AIStartOperation(analytics.KindRuleLab), analytics.AIStopOperation(analytics.KindRuleLab)}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "reader", Enabled: true, SessionVersion: 1, Permissions: permission, DeviceScope: "selected", DeviceIDs: []string{"a", "b"}}}}
	if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
		t.Fatal(ok, e)
	}
	cfg := config.Config{AdminUser: "admin", AdminTenants: []string{"t", "other"}, JWTSecret: "rule-ai-test-secret-at-least-32-characters", DevMode: true}
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	store := api.analysis.Store
	for _, id := range []string{"experiment", "another-experiment"} {
		if _, e := store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: id, TenantID: "t", Kind: model.RuleLabExperimentKind, ResourceID: id, Scope: "SHARED", Creator: "reader", DeviceIDs: []string{"a", "b"}, Body: json.RawMessage(`{"candidateEnabled":false}`)}, 0); e != nil {
			t.Fatal(e)
		}
	}
	run, e := store.CreateAnalysisRun(ctx, model.AnalysisRun{ID: "facts", TenantID: "t", Kind: analytics.KindRuleLab, Creator: "reader", DeviceIDs: []string{"a", "b"}, PermissionsVersion: "fixed", Start: 1000, End: 2000, ConfigurationVersion: "v1", AlgorithmVersion: "v1", IdempotencyKey: "facts", Parameters: json.RawMessage(`{"phase":"EXPERIMENT","experimentRevisionId":"experiment","datasetId":"dataset"}`)}, 100)
	if e != nil {
		t.Fatal(e)
	}
	run, e = store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{run.Kind})
	if e != nil {
		t.Fatal(e)
	}
	run, e = store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "done", Status: model.AnalysisPartial, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"unknown":2}`), Limitations: []string{"初态未知"}}})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, e := api.auth.IssueUser("reader", "t", 1, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	p := "/api/v1/rule-lab/experiments/experiment/ai-jobs"
	q := map[string]any{"expectedVersion": run.Version, "idempotencyKey": "click"}
	job := requestJSON(t, server.Client(), "POST", server.URL+p+"?runId=facts", token, q, 202)
	if job["workflowId"] != core.WorkflowRulePolicy {
		t.Fatal(job)
	}
	id := job["id"].(string)
	requestJSON(t, server.Client(), "GET", server.URL+p+"?runId=facts", token, nil, 200)
	requestJSON(t, server.Client(), "GET", server.URL+p+"/"+id, token, nil, 200)
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rule-lab/experiments/another-experiment/ai-jobs/"+id, token, nil, 404)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rule-lab/experiments/another-experiment/ai-jobs?runId=facts", token, q, 404)
	claimed, e := store.(ports.AnalysisAIStore).ClaimAnalysisAIRevision(ctx, "ai", time.Minute, 4*time.Minute, []string{core.WorkflowRulePolicy})
	if e != nil {
		t.Fatal(e)
	}
	requestJSON(t, server.Client(), "POST", server.URL+p+"/"+id+"/stop", token, map[string]any{"expectedVersion": claimed.Version}, 200)
	state, e = repo.LoadAccessState(ctx, "t")
	if e != nil {
		t.Fatal(e)
	}
	state.Users[0].DeviceIDs = []string{"a"}
	if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
		t.Fatal(ok, e)
	}
	requestJSON(t, server.Client(), "GET", server.URL+p+"/"+id, token, nil, 403)
}
