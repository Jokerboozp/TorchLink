package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
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

func TestGovernanceExactFactHTTPReadsOldSnapshotAndFreshPermissions(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, id := range []string{"a", "b"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id}); err != nil {
			t.Fatal(err)
		}
	}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "viewer", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"a", "b"}, Permissions: []string{"menu:devices", "menu:alarms", "menu:alarmGovernance"}}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(err)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "exact-fact-http-secret-longer-than-32-characters", DevMode: true}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.IssueUser("viewer", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	input := model.AnalysisRun{ID: "fixed-fact-run", TenantID: "t", Kind: analytics.KindRecurring, Creator: "admin", DeviceIDs: []string{"a", "b"}, Start: 1000, End: 2000, Parameters: json.RawMessage(`{}`), ConfigurationVersion: "fixed-v1", AlgorithmVersion: "fixed-v1", IdempotencyKey: "fixed-fact-run"}
	if _, err := api.analysis.Store.CreateAnalysisRun(ctx, input, 10); err != nil {
		t.Fatal(err)
	}
	run, err := api.analysis.Store.ClaimAnalysisRun(ctx, "test", time.Minute, []string{analytics.KindRecurring})
	if err != nil {
		t.Fatal(err)
	}
	fixed := model.GovernanceDocument{ID: "verification-source", Kind: model.GovernanceVerificationKind, DeviceIDs: []string{"a", "b"}, Body: json.RawMessage(`{"fieldResult":"UNABLE_TO_DETERMINE","basis":"旧快照依据"}`)}
	if err := repo.GovernanceTransaction(ctx, "t", func(tx ports.AlarmGovernanceTx) error { var err error; fixed, err = tx.Put(fixed, 0); return err }); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(fixed)
	run, err = api.analysis.Store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Outputs: []model.AnalysisOutput{
		{ID: run.ID + ":input-manifest", Kind: "input-manifest", Body: json.RawMessage(`{"resources":[]}`)},
		{ID: "exact-verification-fact", Kind: "verifications", Body: body},
	}, Snapshot: &model.AnalysisSnapshot{ID: "exact-fact-snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"reportCount":1}`), Limitations: []string{"历史覆盖未知"}}})
	if err != nil {
		t.Fatal(err)
	}
	base := server.URL + "/api/v1/alarm-governance/runs/" + run.ID + "/facts/"
	read := func(id string, status int) map[string]any {
		return requestJSON(t, server.Client(), "GET", base+url.PathEscape(id), token, nil, status)
	}
	fact := read("exact-verification-fact", 200)
	if fact["snapshotId"] != run.SnapshotID || fact["factsHash"] == "" {
		t.Fatal(fact)
	}
	// Update the real source after the snapshot. Exact facts must still show
	// their original typed document rather than resolving a mutable source ID.
	fixed.Body = json.RawMessage(`{"fieldResult":"DISPUTED","basis":"后续更正"}`)
	if err := repo.GovernanceTransaction(ctx, "t", func(tx ports.AlarmGovernanceTx) error { _, err := tx.Put(fixed, fixed.Version); return err }); err != nil {
		t.Fatal(err)
	}
	still := read("exact-verification-fact", 200)
	item := still["body"].(map[string]any)["body"].(map[string]any)
	if item["basis"] != "旧快照依据" || still["factsHash"] != fact["factsHash"] {
		t.Fatal("latest source replaced frozen fact", still)
	}
	summary := read(run.SnapshotID+"/summary", 200)
	if summary["summary"].(map[string]any)["statistics"].(map[string]any)["reportCount"] != float64(1) {
		t.Fatal(summary)
	}
	read("verification-source", 404)
	read(run.ID+":input-manifest", 404)
	read("missing", 404)
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = []string{"a"}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(err)
	}
	read("exact-verification-fact", 403)
	read(run.SnapshotID+"/summary", 403)
}
