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
)

func TestInvestmentAIHTTPResourceRevisionHistoryAndFinanceRevocation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "d1", TenantID: "t", ProductID: "p", Name: "device"}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, nil, nil, nil, nil, log)
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "investment-ai-httptest-secret-32-characters"}, engine, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := api.analysis.Store
	source, err := store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: "scenario-v1", TenantID: "t", Kind: "INVESTMENT_SCENARIO", ResourceID: "scenario", Creator: "admin", Scope: "SHARED", DeviceIDs: []string{"d1"}, Body: json.RawMessage(`{"useFinance":true,"name":"fixed","currency":"CNY","budget":"500"}`)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]any{"scenarioRevisionId": source.ID, "useFinance": true})
	run, err := store.CreateAnalysisRun(ctx, model.AnalysisRun{ID: "investment-evaluation", TenantID: "t", Kind: analytics.KindInvestment, Creator: "admin", DeviceIDs: []string{"d1"}, Start: 1000, End: 2000, ConfigurationVersion: source.Hash, AlgorithmVersion: "v1", Parameters: params, RequiredPermissions: []string{analytics.FinanceReadOperation}, IdempotencyKey: "evaluate"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	run, err = store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{analytics.KindInvestment})
	if err != nil {
		t.Fatal(err)
	}
	run, err = store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "complete", Status: model.AnalysisPartial, Snapshot: &model.AnalysisSnapshot{ID: "investment-snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"usesFinance":true,"amount":"500"}`), Limitations: []string{"quotes incomplete"}}})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/investment-scenarios/scenario/ai-jobs"
	job := requestJSON(t, server.Client(), "POST", server.URL+path, token, map[string]any{"expectedVersion": run.Version, "idempotencyKey": "first"}, 202)
	if job["workflowId"] != analytics.WorkflowInvestment || job["runId"] != run.ID {
		t.Fatal(job)
	}
	source.ID = "scenario-v2"
	source.Body = json.RawMessage(`{"useFinance":true,"budget":"600"}`)
	if _, err = store.PutAnalysisConfig(ctx, source, source.Version); err != nil {
		t.Fatal(err)
	}
	_ = requestJSON(t, server.Client(), "GET", server.URL+path, token, nil, 404) // no completed run of the new scenario revision
	read := requestJSON(t, server.Client(), "GET", server.URL+path+"/"+job["id"].(string), token, nil, 200)
	if read["runId"] != run.ID {
		t.Fatal("fixed history rebound", read)
	}
	api.analysis.Resolve = func(context.Context, analytics.Actor) (analytics.Actor, error) {
		return analytics.Actor{TenantID: "t", Username: "admin", AllDevices: true, Permissions: []string{"menu:devices", "menu:maintenance", analytics.AIStartOperation(analytics.KindInvestment)}}, nil
	}
	_ = requestJSON(t, server.Client(), "GET", server.URL+path+"/"+job["id"].(string), token, nil, 403)
	_ = requestJSON(t, server.Client(), "POST", server.URL+path+"/"+job["id"].(string)+"/stop", token, map[string]any{"expectedVersion": job["version"]}, 403)
	if facts, err := store.GetAnalysisRun(ctx, "t", run.ID); err != nil || facts.Status != model.AnalysisPartial {
		t.Fatal(facts, err)
	}
}
