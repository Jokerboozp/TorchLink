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

func TestAnalysisAIAPIExplicitStartStopAndWholeBodyRevocation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	cfg := config.Config{AdminUser: "admin", AdminPassword: "test", AdminTenants: []string{"t", "other"}, JWTSecret: "analysis-ai-test-secret-at-least-32-characters", DevMode: true}
	permissions := []string{"menu:devices", "menu:dataQuality", "POST /api/v1/data-quality/runs/:id/ai-jobs", "POST /api/v1/data-quality/runs/:id/ai-jobs/:jobId/stop"}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "reader", Enabled: true, SessionVersion: 1, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a", "b"}}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	store := api.analysis.Store
	run, err := store.CreateAnalysisRun(ctx, model.AnalysisRun{ID: "facts", TenantID: "t", Kind: analytics.KindDataQuality, Creator: "reader", DeviceIDs: []string{"a", "b"}, PermissionsVersion: "version", Start: 1000, End: 2000, ConfigurationVersion: "1", AlgorithmVersion: "1", IdempotencyKey: "facts"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	run, err = store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{run.Kind})
	if err != nil {
		t.Fatal(err)
	}
	run, err = store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"unknown":2}`), Limitations: []string{"起点未知"}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.IssueUser("reader", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/data-quality/runs/" + run.ID + "/ai-jobs"
	q := map[string]any{"expectedVersion": run.Version, "idempotencyKey": "click", "useKnowledge": false}
	job := requestJSON(t, server.Client(), "POST", server.URL+path, token, q, 202)
	id := job["id"].(string)
	again := requestJSON(t, server.Client(), "POST", server.URL+path, token, q, 202)
	if again["id"] != id {
		t.Fatal("repeat click created another job")
	}
	requestJSON(t, server.Client(), "GET", server.URL+path+"/"+id, token, nil, 200)
	q["useKnowledge"] = true
	q["idempotencyKey"] = "knowledge"
	requestJSON(t, server.Client(), "POST", server.URL+path, token, q, 403)
	q["useKnowledge"] = false
	aistore := store.(ports.AnalysisAIStore)
	claimed, err := aistore.ClaimAnalysisAIRevision(ctx, "worker", time.Minute, 4*time.Minute, []string{analytics.WorkflowDataQuality})
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+path+"/"+id+"/stop", token, map[string]any{"expectedVersion": claimed.Version}, 200)
	if _, err = aistore.FinishAnalysisAIRevision(ctx, "t", id, claimed.LeaseToken, model.AnalysisAIResult{}, "", "late"); err != model.ErrAnalysisLeaseLost {
		t.Fatal("stopped worker wrote", err)
	}
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = []string{"a"}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+path+"/"+id, token, nil, 403)
	requestJSON(t, server.Client(), "GET", server.URL+path, token, nil, 403)
	other, err := api.auth.Issue("admin", "other", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+path+"/"+id, other, nil, 404)
}
