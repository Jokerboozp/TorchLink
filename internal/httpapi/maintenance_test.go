package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/maintenance"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestMaintenanceAPICompletedWorkCanCreateIndependentDutyFollowUp(t *testing.T) {
	f := newDutyHTTPFixture(t, "maintenance_duty", "maintenance-duty-test-only")
	f.api.SetAnalysisStorage(analytics.NewMemoryStore(), nil)
	write := func(path string, body any) map[string]any {
		return f.req("POST", path, f.admin, body, 201)
	}
	envelope := func(id string, body any) map[string]any {
		return map[string]any{"resourceId": id, "idempotencyKey": id, "expectedVersion": 0, "deviceIds": []string{"duty-device"}, "body": body}
	}
	now := time.Now().UnixMilli()
	asset := write("/api/v1/assets", envelope("asset", model.AssetInstance{Name: "独立实物", DeviceID: "duty-device", PhysicalID: "actual-serial", EffectiveStart: now - 60000, BoundaryStatus: "UNKNOWN"}))
	work := write("/api/v1/maintenance-records", envelope("work", model.MaintenanceIntervention{Name: "工作结束后继续验收", AssetRevisionID: asset["id"].(string), Type: "REPAIR", Reason: "实际检修"}))
	work = write("/api/v1/maintenance-records/work/actions", map[string]any{"expectedVersion": work["version"], "idempotencyKey": "start", "action": "START", "at": now - 1000})
	work = write("/api/v1/maintenance-records/work/actions", map[string]any{"expectedVersion": work["version"], "idempotencyKey": "complete", "action": "COMPLETE", "at": now, "reason": "检修结束，功能仍需独立验收"})
	linkBody := map[string]any{"expectedVersion": work["version"], "runId": f.dayRun["id"], "ownerId": "day", "nextAction": "登记功能验收，跟进事项独立处理"}
	link := write("/api/v1/maintenance-records/work/duty-links", linkBody)
	retry := write("/api/v1/maintenance-records/work/duty-links", linkBody)
	if link["id"] != retry["id"] {
		t.Fatal("repeated completed-work link created another duty item")
	}
	links := f.req("GET", "/api/v1/maintenance-records/work/duty-links", f.admin, nil, 200)
	if links["sourceStatus"] != "COMPLETED" || len(links["items"].([]any)) != 1 {
		t.Fatal(links)
	}
	current := f.req("GET", "/api/v1/maintenance-records/work", f.admin, nil, 200)
	verifications, _ := current["body"].(map[string]any)["verifications"].([]any)
	if current["version"] != work["version"] || len(verifications) != 0 {
		t.Fatal("duty association changed work or approved functional verification", current)
	}
	cancelled := write("/api/v1/maintenance-records", envelope("cancelled", model.MaintenanceIntervention{Name: "已取消工作", AssetRevisionID: asset["id"].(string), Type: "REPAIR", Reason: "无需继续"}))
	cancelled = write("/api/v1/maintenance-records/cancelled/actions", map[string]any{"expectedVersion": cancelled["version"], "idempotencyKey": "cancel", "action": "CANCEL", "reason": "取消"})
	linkBody["expectedVersion"] = cancelled["version"]
	f.req("POST", "/api/v1/maintenance-records/cancelled/duty-links", f.admin, linkBody, 409)
}

func TestMaintenanceAPIPhysicalScopeFixedUnknownReportsAndMoneyRevocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	for _, id := range []string{"d1", "hidden"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, ProductID: "p", Name: id, AccessKey: id}); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	api := New(config.Config{AdminUser: "admin", AdminPassword: "fixture-only", AdminTenants: []string{"t"}, JWTSecret: "maintenance-api-test-secret-32-chars", DevMode: true, Analytics: config.AnalyticsConfig{Poll: time.Millisecond, Workers: 1}}, engine, metrics.New(), log)
	api.SetAnalysisStorage(analytics.NewMemoryStore(), nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := func(method, path string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	body := func(id, key string, version any, b any) map[string]any {
		return map[string]any{"resourceId": id, "idempotencyKey": key, "expectedVersion": version, "deviceIds": []string{"d1"}, "body": b}
	}
	now := time.Now().UnixMilli()
	evidence := []model.ResponseEvidenceReference{{Kind: "HUMAN_CONFIRMATION", DeviceID: "d1", Description: "隔离HTTP实物确认"}}
	asset := req("POST", "/api/v1/assets", body("asset", "asset", 0, model.AssetInstance{Name: "实物", PhysicalID: "serial", DeviceID: "d1", EffectiveStart: now - 3600000, BoundaryStatus: "CONFIRMED", Evidence: evidence}), 201)
	if _, ok := asset["body"].(map[string]any)["requests"]; ok {
		t.Fatal("public aggregate leaked receipt")
	}
	if _, ok := asset["body"].(map[string]any)["commissionedAt"]; ok {
		t.Fatal("registration time became physical date")
	}
	work := req("POST", "/api/v1/maintenance-records", body("repair", "repair", 0, model.MaintenanceIntervention{Name: "维修", AssetRevisionID: asset["id"].(string), Type: "REPAIR", Reason: "实际检修"}), 201)
	work = req("POST", "/api/v1/maintenance-records/repair/actions", map[string]any{"expectedVersion": work["version"], "idempotencyKey": "start", "action": "START", "at": now - 9*60000}, 201)
	work = req("POST", "/api/v1/maintenance-records/repair/actions", map[string]any{"expectedVersion": work["version"], "idempotencyKey": "end", "action": "COMPLETE", "at": now - 8*60000, "reason": "实际工作结束"}, 201)
	workBody := work["body"].(map[string]any)
	if workBody["status"] != "COMPLETED" {
		t.Fatal(workBody)
	}
	if v, ok := workBody["verifications"].([]any); ok && len(v) > 0 {
		t.Fatal("completion auto-verified")
	}
	q := model.MaintenanceObservationRequest{ExpectedVersion: int64(work["version"].(float64)), IdempotencyKey: "observe", Parameters: model.MaintenanceObservationParameters{BeforeAssetRevisionID: asset["id"].(string), AfterAssetRevisionID: asset["id"].(string), ComparisonType: "SAME_INSTANCE_REPAIR", Before: model.FactRange{Start: now - 20*60000, End: now - 10*60000}, After: model.FactRange{Start: now - 7*60000, End: now - 60000}}}
	run := req("POST", "/api/v1/maintenance-records/repair/observations", q, 202)
	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { api.RunAnalysisWorkers(workerCtx); close(workerDone) }()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		run = req("GET", "/api/v1/maintenance-observations/"+run["id"].(string), nil, 200)
		if run["status"] == model.AnalysisPartial {
			break
		}
		if run["status"] == model.AnalysisFailed {
			t.Fatal(run)
		}
		time.Sleep(5 * time.Millisecond)
	}
	workerCancel()
	<-workerDone
	if run["status"] != model.AnalysisPartial {
		t.Fatal("missing facts became complete", run)
	}
	snapshot := req("GET", "/api/v1/maintenance-observations/"+run["id"].(string)+"/snapshot", nil, 200)
	stats := snapshot["statistics"].(map[string]any)
	before := stats["before"].(map[string]any)
	if before["stateUnknownMs"] != float64(10*60000) || before["faultsPer1000Hours"] != nil || before["fullWindowOfflineRatio"] != nil {
		t.Fatal("unknown denominator inferred", stats)
	}
	req("GET", "/api/v1/maintenance-observations/"+run["id"].(string)+"/observations", nil, 200)
	req("GET", "/api/v1/maintenance-records/repair/observations", nil, 200)
	review := map[string]any{"expectedVersion": run["version"], "snapshotVersion": snapshot["version"], "factsHash": snapshot["factsHash"], "result": "OBSERVE", "explanation": "来源不足，继续收集", "idempotencyKey": "review"}
	req("POST", "/api/v1/maintenance-observations/"+run["id"].(string)+"/reviews", review, 201)
	req("GET", "/api/v1/maintenance-observations/"+run["id"].(string)+"/reviews", nil, 200)
	fee := "1.25"
	quote := req("POST", "/api/v1/maintenance-costs", body("quote", "quote", 0, model.MaintenanceCost{Type: "ESTIMATE", SourceKind: maintenance.AssetKind, SourceID: "asset", Currency: "CNY", Material: &fee, OccurredAt: now, PlanningStart: now, PlanningEnd: now + 60000, Basis: "固定报价"}), 201)
	scenario := req("POST", "/api/v1/investment-scenarios", body("scenario", "scenario", 0, model.InvestmentScenario{UseFinance: true, Name: "投入清单", Currency: "CNY", Budget: &fee, PlanningStart: now, PlanningEnd: now + 60000, PolicyVersion: maintenance.DefaultInvestmentPolicy, Candidates: []model.InvestmentCandidate{{ID: "inspect", AssetRevisionID: asset["id"].(string), Action: "INSPECT", RequiredTier: 1, TierBasis: "人工必需项", QuoteRevisionID: quote["id"].(string)}}}), 201)
	// Route middleware remains administrative here, while the analytics current
	// actor is narrowed to exercise server-side scope and derivative inheritance.
	permissions := []string{"menu:devices", "menu:maintenance"}
	api.analysis.Resolve = func(_ context.Context, a analytics.Actor) (analytics.Actor, error) {
		a.Permissions = slices.Clone(permissions)
		a.AllDevices = false
		a.DeviceIDs = []string{"d1"}
		return a, nil
	}
	req("GET", "/api/v1/investment-scenarios/scenario", nil, 403)
	list := req("GET", "/api/v1/investment-scenarios", nil, 200)
	if list["total"] != float64(0) {
		t.Fatal("hidden fund count leaked", list)
	}
	req("GET", "/api/v1/maintenance-revisions/"+scenario["id"].(string)+"?kind="+maintenance.ScenarioKind, nil, 403)
	req("GET", "/api/v1/maintenance-observations/"+run["id"].(string)+"/snapshot", nil, 200)
	// A visible ordinary snapshot cannot expand to the hidden device.
	permissions = append(permissions, "POST /api/v1/assets")
	hidden := body("hidden", "hidden", 0, model.AssetInstance{Name: "隐藏", DeviceID: "hidden", PhysicalID: "hidden", EffectiveStart: now - 1000, BoundaryStatus: "UNKNOWN"})
	hidden["deviceIds"] = []string{"hidden"}
	req("POST", "/api/v1/assets", hidden, 403)
}
