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
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func TestAnalysisAPIDurableIdentityScopeAndWholeResult(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	cfg := config.Config{AdminUser: "admin", AdminPassword: "test-password", AdminTenants: []string{"t", "other"}, JWTSecret: "analysis-test-secret-at-least-32-characters", DevMode: true}
	for _, id := range []string{"a", "b"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: "t", ProductID: "p", AccessKey: "test-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	permissions := []string{"menu:devices", "menu:dataQuality", "POST /api/v1/data-quality/runs", "POST /api/v1/data-quality/runs/:id/stop"}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "reader", Enabled: true, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a", "b"}, SessionVersion: 1}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := api.AnalysisService().Register(analytics.KindDataQuality, func(context.Context, *analytics.Execution) error { return nil }); err != nil {
		t.Fatal(err)
	}
	profileBody, err := json.Marshal(model.QualityProfile{AttributeID: "pressure", Mode: "periodic", EffectiveFrom: 1, ScheduleAnchor: 1, PeriodMs: 1000, ValueType: "number", MinimumSamples: 30})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := api.analysis.Store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: "profile-fixture", TenantID: "t", Kind: model.DataQualityProfileKind, ResourceID: "pressure", Creator: "reader", Scope: "PERSONAL", DeviceIDs: []string{"a", "b"}, Body: profileBody}, 0)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.IssueUser("reader", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := func(method, path string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	q := map[string]any{"deviceIds": []string{"a", "b"}, "start": 1000, "end": 10000, "idempotencyKey": "request", "parameters": model.QualityRunParameters{AttributeIDs: []string{"pressure"}, ProfileRevisionIDs: []string{profile.ID}}}
	run := req("POST", "/api/v1/data-quality/runs", q, 202)
	id := run["id"].(string)
	if repeat := req("POST", "/api/v1/data-quality/runs", q, 202); repeat["id"] != id {
		t.Fatal("duplicate request changed task")
	}
	q["tenantId"] = "other"
	req("POST", "/api/v1/data-quality/runs", q, 400)
	delete(q, "tenantId")
	q["deviceIds"] = []string{"hidden"}
	q["idempotencyKey"] = "hidden"
	req("POST", "/api/v1/data-quality/runs", q, 403)
	claimed, err := api.analysis.Store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{analytics.KindDataQuality})
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.analysis.Store.CommitAnalysisBatch(ctx, "t", id, claimed.LeaseToken, model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Stage: "complete", Processed: 2, Outputs: []model.AnalysisOutput{{ID: "metric", Kind: "metrics", DeviceID: "a", Body: json.RawMessage(`{"unknown":true}`)}}, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 9000, InputHashes: []string{"fixed-source-input"}, Statistics: json.RawMessage(`{"unknown":true}`), MissingSources: []string{"availableAt"}, UncomputableMetrics: []string{"continuity"}}})
	if err != nil {
		t.Fatal(err)
	}
	req("GET", "/api/v1/data-quality/runs/"+id+"/snapshot", nil, 200)
	page := req("GET", "/api/v1/data-quality/runs/"+id+"/metrics?limit=1000", nil, 200)
	if page["total"] != float64(1) {
		t.Fatal(page)
	}
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = []string{"a"}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, suffix := range []string{"", "/snapshot", "/metrics", "/findings", "/evidence"} {
		req("GET", "/api/v1/data-quality/runs/"+id+suffix, nil, 403)
	}
	page = req("GET", "/api/v1/data-quality/runs", nil, 200)
	if page["total"] != float64(0) {
		t.Fatal("hidden count disclosed", page)
	}
	other, err := api.auth.Issue("admin", "other", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/data-quality/runs/"+id, other, nil, 404)
}

func TestAnalysisStorageSurvivesRepositoryDecoratorsAndServerRestart(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "a", TenantID: "t", ProductID: "p", AccessKey: "test-a"}); err != nil {
		t.Fatal(err)
	}
	// Redis decorates only the Repository contract; it cannot implicitly expose
	// the optional durable AnalysisStore. Startup must wire that port explicitly.
	decorated := redisadapter.New(repo, nil)
	store := analytics.NewMemoryStore()
	cfg := config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "analysis-restart-test-secret-32-characters", DevMode: true}
	newAPI := func() *Server {
		api := New(cfg, &core.Engine{Repo: decorated}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		api.SetAnalysisStorage(store, nil)
		if err := api.AnalysisService().Register(analytics.KindDataQuality, func(context.Context, *analytics.Execution) error { return nil }); err != nil {
			t.Fatal(err)
		}
		return api
	}
	first := newAPI()
	a := analytics.Actor{TenantID: "t", Username: "admin"}
	run, err := first.AnalysisService().Create(ctx, a, analytics.KindDataQuality, "v1", analytics.CreateRequest{DeviceIDs: []string{"a"}, Start: 1000, End: 10000, ConfigurationVersion: "profile-v1", IdempotencyKey: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	second := newAPI()
	got, err := second.AnalysisService().Get(ctx, a, analytics.KindDataQuality, run.ID)
	if err != nil || got.ID != run.ID || got.Status != model.AnalysisQueued {
		t.Fatal("decorated storage lost task across server instances", got, err)
	}
}

func TestAnalysisAttachmentDownloadIsAssignableAndProtected(t *testing.T) {
	api := New(config.Config{DevMode: true}, &core.Engine{Repo: memory.NewRepository()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	const path = "/api/v1/data-quality/calibrations/attachments/:id"
	found := false
	for _, item := range api.permissionCatalog() {
		if item.ID == "GET "+path {
			found = item.Menu == "dataQuality" && item.Name == "下载校准附件"
		}
	}
	if !found {
		t.Fatal("attachment download cannot be assigned from the real permission catalog")
	}
	permissions := map[string]bool{"menu:dataQuality": true, "menu:devices": true}
	if allowsRoute(permissions, "GET", path) {
		t.Fatal("ordinary page read granted attachment download")
	}
	permissions["GET "+path] = true
	if !allowsRoute(permissions, "GET", path) {
		t.Fatal("explicit attachment permission did not grant its route")
	}
}
