package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func TestMonitoringHTTPConfirmedObservationVersionScopeAndOrdinaryReads(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "pressure", DataType: "number", Unit: "kPa"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, ProductID: "p", AccessKey: "test-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	permissions := []string{"menu:devices", "menu:monitoringGaps", "POST /api/v1/monitoring-gaps/profiles", "POST /api/v1/monitoring-gaps/observations", "POST /api/v1/monitoring-gaps/observations/:id/confirm", "POST /api/v1/monitoring-gaps/runs", "POST /api/v1/monitoring-gaps/runs/:id/hypotheses"}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "analyst", Enabled: true, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a"}, SessionVersion: 1}, {Username: "other", Enabled: true, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a"}, SessionVersion: 1}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "monitoring-http-test-secret-32-characters", DevMode: true}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.IssueUser("analyst", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	other, err := api.auth.IssueUser("other", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).UnixMilli()
	q := map[string]any{"resourceId": "pressure", "scope": "personal", "deviceIds": []string{"a"}, "body": model.MonitoringProfile{EffectiveFrom: start, Mode: "periodic", Attributes: []model.MonitoringAttribute{{ID: "pressure", ValueType: "number"}}, MessageTypes: []model.MessageType{model.PropertyReport}, Merge: "ALL", PeriodMs: 1000, ToleranceMs: 100}}
	profile := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/profiles", token, q, 201)
	q["deviceIds"] = []string{"b"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/profiles", token, q, 403)
	q["deviceIds"] = []string{"a"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/profiles/publish", token, q, 403)
	page := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/monitoring-gaps/profiles", other, nil, 200)
	if page["total"] != float64(0) {
		t.Fatal("personal monitoring policy leaked", page)
	}
	body := map[string]any{"deviceId": "a", "start": start + 2000, "end": start + 3000, "type": "MAINTENANCE", "basis": "受控观察依据", "reason": "检修观察"}
	observationQ := map[string]any{"resourceId": "maintenance", "scope": "personal", "deviceIds": []string{"a"}, "body": body}
	body["confirmedBy"] = "impersonated"
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/observations", token, observationQ, 422)
	delete(body, "confirmedBy")
	observation := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/observations", token, observationQ, 201)
	parameters := model.MonitoringRunParameters{ProfileRevisionIDs: []string{profile["id"].(string)}, ObservationRevisionIDs: []string{observation["id"].(string)}}
	runQ := map[string]any{"deviceIds": []string{"a"}, "start": start, "end": start + 10000, "parameters": parameters, "idempotencyKey": "monitoring-run"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/runs", token, runQ, 422)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/observations/"+observation["id"].(string)+"/confirm", token, map[string]any{"expectedVersion": 99}, 409)
	confirmed := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/observations/"+observation["id"].(string)+"/confirm", token, map[string]any{"expectedVersion": 1}, 201)
	if confirmed["version"] != float64(2) || confirmed["body"].(map[string]any)["confirmedBy"] != "analyst" {
		t.Fatal("observation not versioned or server-confirmed", confirmed)
	}
	parameters.ObservationRevisionIDs = []string{confirmed["id"].(string)}
	runQ["parameters"] = parameters
	run := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/runs", token, runQ, 202)
	for _, suffix := range []string{"/metrics", "/intervals", "/dependency-groups", "/reviews", "/hypotheses"} {
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/monitoring-gaps/runs/"+run["id"].(string)+suffix, token, nil, 200)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/monitoring-gaps/runs/"+run["id"].(string)+"/export", token, nil, 403)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/monitoring-gaps/runs/"+run["id"].(string)+"/hypotheses", token, map[string]any{"groupId": "invented", "at": start, "expectedRunVersion": run["version"], "idempotencyKey": "not-complete"}, 409)
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = []string{}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/monitoring-gaps/runs/"+run["id"].(string)+"/intervals", token, nil, 403)
}
