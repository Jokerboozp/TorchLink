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

func TestResponseAIHTTPFixedEvaluationHistoryAndExecutionBinding(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "d1", TenantID: "t", ProductID: "p", Name: "device"}); err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "response-ai-httptest-secret-32-characters"}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := api.analysis.Store
	source, err := store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: "execution-v1", TenantID: "t", Kind: "RESPONSE_EXECUTION", ResourceID: "exec-one", Creator: "admin", Scope: "SHARED", DeviceIDs: []string{"d1"}, Body: json.RawMessage(`{"status":"ENDED","startedAt":1000,"endedAt":2000}`)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	makeRun := func(id string) model.AnalysisRun {
		parameters, _ := json.Marshal(map[string]any{"executionRevisionId": source.ID, "cutoff": 2000})
		run, err := store.CreateAnalysisRun(ctx, model.AnalysisRun{ID: id, TenantID: "t", Kind: analytics.KindResponse, Creator: "admin", DeviceIDs: []string{"d1"}, Start: 1000, End: 2000, ConfigurationVersion: source.Hash, AlgorithmVersion: "v1", Parameters: parameters, IdempotencyKey: id}, 100)
		if err != nil {
			t.Fatal(err)
		}
		run, err = store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{analytics.KindResponse})
		if err != nil {
			t.Fatal(err)
		}
		run, err = store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "completed", Status: model.AnalysisSucceeded, Snapshot: &model.AnalysisSnapshot{ID: id + "-snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"requiredSteps":1,"completedRequiredSteps":0}`)}})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	run := makeRun("evaluation-one")
	path := "/api/v1/response-runs/exec-one/ai-jobs"
	job := requestJSON(t, server.Client(), "POST", server.URL+path+"?runId="+run.ID, token, map[string]any{"expectedVersion": run.Version, "idempotencyKey": "first"}, 202)
	if job["runId"] != run.ID || job["workflowId"] != analytics.WorkflowResponse {
		t.Fatal(job)
	}
	// A later fixed evaluation must not rebind old job details or stop requests.
	newRun := makeRun("evaluation-two")
	_ = requestJSON(t, server.Client(), "GET", server.URL+path+"?runId="+newRun.ID, token, nil, 200)
	read := requestJSON(t, server.Client(), "GET", server.URL+path+"/"+job["id"].(string), token, nil, 200)
	if read["runId"] != run.ID || read["snapshotId"] != run.SnapshotID {
		t.Fatal("historical job rebound to later evaluation", read)
	}
	other, err := store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: "other-execution", TenantID: "t", Kind: "RESPONSE_EXECUTION", ResourceID: "other", Creator: "admin", Scope: "SHARED", DeviceIDs: []string{"d1"}, Body: json.RawMessage(`{}`)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	_ = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/response-runs/other/ai-jobs/"+job["id"].(string), token, nil, 404)
	stopped := requestJSON(t, server.Client(), "POST", server.URL+path+"/"+job["id"].(string)+"/stop", token, map[string]any{"expectedVersion": job["version"]}, 200)
	if stopped["status"] != model.AnalysisCancelled {
		t.Fatal(stopped)
	}
	if facts, err := store.GetAnalysisRun(ctx, "t", run.ID); err != nil || facts.Status != model.AnalysisSucceeded {
		t.Fatal("stopping AI changed facts", facts, err)
	}
}
