package httpapi

import (
	"context"
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

func TestGovernanceHistoricalProjectionAPIRequiresExplicitSourceAndCurrentFullScope(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, id := range []string{"a", "b"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id}); err != nil {
			t.Fatal(err)
		}
	}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "reader", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"a"}, Permissions: []string{"menu:devices", "menu:alarms", "menu:alarmGovernance"}}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(err)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "projection-http-secret-longer-than-32-characters", DevMode: true, Analytics: config.AnalyticsConfig{Poll: 5 * time.Millisecond}}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	admin, err := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := api.auth.IssueUser("reader", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/alarm-governance/historical-projections"
	q := map[string]any{"deviceIds": []string{"a"}, "start": 1000, "end": 2000, "idempotencyKey": "api-projection", "parameters": map[string]any{"historicalSources": []string{"POSTGRESQL"}, "timeBasis": "EVENT_AT"}}
	requestJSON(t, server.Client(), "POST", server.URL+base, reader, q, 403)
	q["parameters"] = map[string]any{"timeBasis": "EVENT_AT"}
	requestJSON(t, server.Client(), "POST", server.URL+base, admin, q, 422)
	q["parameters"] = map[string]any{"historicalSources": []string{"POSTGRESQL"}, "unknownProjectionField": true}
	requestJSON(t, server.Client(), "POST", server.URL+base, admin, q, 422)
	q["parameters"] = map[string]any{"historicalSources": []string{"POSTGRESQL"}}
	created := requestJSON(t, server.Client(), "POST", server.URL+base, admin, q, 202)
	again := requestJSON(t, server.Client(), "POST", server.URL+base, admin, q, 202)
	if created["id"] != again["id"] || created["algorithmVersion"] != "historical-observation-projection-v1" {
		t.Fatal(created, again)
	}
	requestJSON(t, server.Client(), "GET", server.URL+base+"/"+created["id"].(string), reader, nil, 200)
	requestJSON(t, server.Client(), "POST", server.URL+base+"/"+created["id"].(string)+"/stop", reader, map[string]any{"expectedVersion": created["version"]}, 403)
	q["deviceIds"], q["idempotencyKey"] = []string{"a", "b"}, "shared-projection"
	shared := requestJSON(t, server.Client(), "POST", server.URL+base, admin, q, 202)
	requestJSON(t, server.Client(), "GET", server.URL+base+"/"+shared["id"].(string), reader, nil, 403)
	page := requestJSON(t, server.Client(), "GET", server.URL+base+"?deviceIds=a&limit=1", reader, nil, 200)
	if page["total"] != float64(1) {
		t.Fatal("shared task count leaked", page)
	}
	stopped := requestJSON(t, server.Client(), "POST", server.URL+base+"/"+created["id"].(string)+"/stop", admin, map[string]any{"expectedVersion": created["version"]}, 200)
	if stopped["status"] != model.AnalysisCancelled {
		t.Fatal(stopped)
	}
	requestJSON(t, server.Client(), "GET", server.URL+base+"/"+created["id"].(string)+"/snapshot", reader, nil, 404)
	outputs := requestJSON(t, server.Client(), "GET", server.URL+base+"/"+created["id"].(string)+"/observations", reader, nil, 200)
	if outputs["total"] != float64(0) {
		t.Fatal(outputs)
	}
	// Re-read the live access state; a previously issued browser token cannot
	// preserve access to a projection after device authorization is removed.
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = nil
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+base+"/"+created["id"].(string), reader, nil, 403)
	page = requestJSON(t, server.Client(), "GET", server.URL+base, reader, nil, 200)
	if page["total"] != float64(0) {
		t.Fatal(page)
	}
	// Projection and ordinary analysis remain the same fenced worker kind.
	if stored, err := api.analysis.Store.GetAnalysisRun(ctx, "t", created["id"].(string)); err != nil || stored.Kind != analytics.KindRecurring {
		t.Fatal(stored, err)
	}
}
