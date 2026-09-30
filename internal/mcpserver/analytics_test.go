package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
)

func analysisMCPFixture(t *testing.T) (*core.Engine, *analytics.AIService, auth.Claims) {
	t.Helper()
	ctx := context.Background()
	store := analytics.NewMemoryStore()
	actor := analytics.Actor{TenantID: "t", Username: "alice", Managed: true, SessionVersion: 1, AccessVersion: "scope1", DeviceIDs: []string{"d1", "d2"}, Permissions: []string{"*"}}
	facts := analytics.NewService(store, config.AnalyticsConfig{}, func(_ context.Context, a analytics.Actor) (analytics.Actor, error) {
		a.AccessVersion = actor.AccessVersion
		a.DeviceIDs = actor.DeviceIDs
		a.Permissions = actor.Permissions
		return a, nil
	}, nil)
	run, err := store.CreateAnalysisRun(ctx, model.AnalysisRun{ID: "facts", TenantID: "t", Kind: analytics.KindDataQuality, Creator: "alice", DeviceIDs: actor.DeviceIDs, PermissionsVersion: "scope1", Start: 1000, End: 2000, ConfigurationVersion: "1", AlgorithmVersion: "1", IdempotencyKey: "facts"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	run, err = store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{run.Kind})
	if err != nil {
		t.Fatal(err)
	}
	run, err = store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Outputs: []model.AnalysisOutput{{ID: "fact", Kind: "findings", DeviceID: "d1", Body: json.RawMessage(`{"unknown":true}`)}}, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"unknown":2}`), Limitations: []string{"unknown seed"}}})
	if err != nil {
		t.Fatal(err)
	}
	ai := analytics.NewAIService(facts, nil)
	if _, err = ai.Create(ctx, actor, run.Kind, run.ID, analytics.CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "ai"}); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimAnalysisAIRevision(ctx, "ai", time.Minute, 4*time.Minute, []string{analytics.WorkflowDataQuality})
	if err != nil {
		t.Fatal(err)
	}
	c := auth.Claims{Username: "alice", TenantID: "t", TokenUse: "harness", ManagedUser: true, SessionVersion: 1, RunID: job.HarnessRunID, Workflow: core.WorkflowDataQuality, AnalysisRunID: job.RunID, AnalysisSnapshotID: job.SnapshotID, AnalysisSnapshotVersion: 1, AnalysisJobID: job.ID, AnalysisLeaseToken: job.LeaseToken, AnalysisAccessVersion: "scope1", Scopes: []string{auth.ScopeQueryAnalysisSnapshot}, RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{auth.HarnessAudience}}}
	return &core.Engine{Repo: memory.NewRepository(), AnalysisAI: ai}, ai, c
}
func analysisMCPCall(t *testing.T, engine *core.Engine, c auth.Claims, args map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "query_analysis_snapshot", "arguments": args}})
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithClaims(context.Background(), c))
	out := httptest.NewRecorder()
	NewHarness(engine).ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	return out.Body.String()
}
func TestAnalysisMCPBoundPageAndRejectsOverrides(t *testing.T) {
	engine, _, c := analysisMCPFixture(t)
	out := analysisMCPCall(t, engine, c, map[string]any{"collection": "findings", "limit": 1, "offset": 0})
	if strings.Contains(out, `"isError":true`) || !strings.Contains(out, `\"id\":\"fact\"`) {
		t.Fatal(out)
	}
	for _, args := range []map[string]any{{"runId": "other"}, {"deviceIds": []string{"hidden"}}, {"tenantId": "other"}, {"sql": "select *"}, {"url": "http://private"}, {"limit": 101}, {"offset": .5}, {"collection": "production"}} {
		out = analysisMCPCall(t, engine, c, args)
		if !strings.Contains(out, `"isError":true`) || strings.Contains(out, `\"unknown\":true`) {
			t.Fatal("override accepted", args, out)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	New(engine).ServeHTTP(response, request)
	if strings.Contains(response.Body.String(), `"name":"query_analysis_snapshot"`) {
		t.Fatal("analysis tool exposed outside Harness")
	}
}
func TestAnalysisMCPRejectsChangedClaimGrantAndStop(t *testing.T) {
	for _, change := range []func(*auth.Claims){func(c *auth.Claims) { c.Username = "other" }, func(c *auth.Claims) { c.TenantID = "other" }, func(c *auth.Claims) { c.AnalysisJobID = "other" }, func(c *auth.Claims) { c.AnalysisSnapshotVersion++ }, func(c *auth.Claims) { c.AnalysisLeaseToken++ }, func(c *auth.Claims) { c.RunID = "other" }, func(c *auth.Claims) { c.AnalysisAccessVersion = "other" }, func(c *auth.Claims) { c.SessionVersion++ }, func(c *auth.Claims) { c.Workflow = "ops-assistant" }, func(c *auth.Claims) { c.Scopes = nil }, func(c *auth.Claims) { c.Audience = jwt.ClaimStrings{"browser"} }} {
		engine, _, c := analysisMCPFixture(t)
		change(&c)
		if out := analysisMCPCall(t, engine, c, map[string]any{}); !strings.Contains(out, `"isError":true`) {
			t.Fatal(out)
		}
	}
	engine, ai, c := analysisMCPFixture(t)
	ai.Facts.Resolve = func(context.Context, analytics.Actor) (analytics.Actor, error) {
		return analytics.Actor{TenantID: "t", Username: "alice", Managed: true, SessionVersion: 1, AccessVersion: "revoked", DeviceIDs: []string{"d1"}, Permissions: []string{"*"}}, nil
	}
	if out := analysisMCPCall(t, engine, c, map[string]any{}); !strings.Contains(out, `"isError":true`) || strings.Contains(out, `\"unknown\":2`) {
		t.Fatal("cropped revoked body disclosed", out)
	}
}
