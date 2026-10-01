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
	"iot-platform/internal/ports"
	"iot-platform/internal/rulelab/eval"
	"iot-platform/internal/rulelab/lab"
)

type ruleLabHTTPInputs struct{}

func TestRuleLabRuleSourcesReportsConfiguredRecordLimit(t *testing.T) {
	for _, fixture := range []struct {
		name                 string
		configured, expected int
	}{{"default", 0, lab.MaxRecords}, {"smaller configured limit", 3, 3}} {
		t.Run(fixture.name, func(t *testing.T) {
			repo := memory.NewRepository()
			if err := repo.SaveManagedDevice(context.Background(), model.ManagedDevice{TenantID: "t", ID: "a", ProductID: "p", AccessKey: "rule-source-limit-test"}); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "rule-source-limit-test-key-32-chars", DevMode: true, Analytics: config.AnalyticsConfig{RecordLimit: fixture.configured}}
			api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			server := httptest.NewServer(api.Handler())
			defer server.Close()
			token, _ := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
			page := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rule-lab/rule-sources?deviceIds=a", token, nil, 200)
			if page["maxRecords"] != float64(fixture.expected) || page["total"] != float64(0) {
				t.Fatalf("rule sources do not expose the actual normalized record limit: %v", page)
			}
		})
	}
}

func (ruleLabHTTPInputs) RuleLabInputsRead(ctx context.Context, tenant string, fn func(ports.RuleLabInputReader) error) error {
	return fn(ruleLabHTTPInputs{})
}
func (ruleLabHTTPInputs) ListStandardInputs(q model.RuleLabInputQuery) (model.FactPage[model.RuleLabInput], error) {
	return model.FactPage[model.RuleLabInput]{Items: []model.RuleLabInput{}, FactPageMeta: model.FactPageMeta{Complete: true, Source: model.FactSourceCoverage{Source: "controlled-empty-fixture", CoverageStart: q.Start, CoverageEnd: q.End, ReadAt: time.Now().UnixMilli(), Complete: true, Status: "AVAILABLE", HistoricalReconstructionQuality: "CONTROLLED_SYNTHETIC"}}}, nil
}
func TestRuleLabHTTPActualActionsScopedBaselineAndImmutableLabelVersions(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "pressure", DataType: "number", Unit: "kPa"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "hidden"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, ProductID: "p", AccessKey: "lab-http-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	revision, err := repo.PublishRule(ctx, model.RulePublishRequest{Rule: model.AlarmRule{TenantID: "t", ProductID: "p", ID: "r", Name: "压力求值", AlarmType: "PRESSURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "pressure", Operator: ">", Value: 100}}}, Reason: "isolated HTTP fixture", SemanticsVersion: eval.RevisionV2})
	if err != nil {
		t.Fatal(err)
	}
	permissions := []string{"menu:devices", "menu:ruleLab", "POST /api/v1/rule-lab/datasets", "POST /api/v1/rule-lab/labels", "POST /api/v1/rule-lab/labels/:id/confirm", "POST /api/v1/rule-lab/experiments", "POST /api/v1/rule-lab/experiments/:id/runs"}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "analyst", Enabled: true, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a"}, SessionVersion: 1}, {Username: "other", Enabled: true, Permissions: permissions, DeviceScope: "selected", DeviceIDs: []string{"a"}, SessionVersion: 1}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "rule-lab-test-secret-32-characters", DevMode: true}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.rulelab.Inputs = ruleLabHTTPInputs{}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.IssueUser("analyst", "t", 1, time.Hour)
	other, _ := api.auth.IssueUser("other", "t", 1, time.Hour)
	page := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rule-lab/rule-sources?deviceIds=a", token, nil, 200)
	if page["total"] != float64(1) || page["items"].([]any)[0].(map[string]any)["id"] != revision.ID {
		t.Fatal("partial user cannot read applicable immutable rule", page)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rule-lab/rule-sources?deviceIds=hidden", token, nil, 403)
	start := time.Now().Add(-time.Hour).UnixMilli()
	q := model.RuleLabDatasetRequest{DeviceIDs: []string{"a"}, Start: start, End: start + 10000, WarmupStart: start, TimeBasis: "EVENT", ClockPolicy: "EVENT_AS_PROCESSING", InitialStatePolicy: "EMPTY_UNKNOWN", SemanticsVersion: eval.RevisionV2, IdempotencyKey: "http-dataset"}
	dataset := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rule-lab/datasets", token, q, 202)
	if dataset["runId"] == "" {
		t.Fatal(dataset)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rule-lab/datasets/"+dataset["id"].(string), other, nil, 403)
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rule-lab/datasets/"+dataset["id"].(string)+"/inputs", token, nil, 200)
	q.IdempotencyKey = "http-shared"
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rule-lab/datasets/publish", token, q, 403)
	q.DeviceIDs = []string{"hidden"}
	q.IdempotencyKey = "http-hidden"
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rule-lab/datasets", token, q, 403)
	labelQ := map[string]any{"resourceId": "confirmed-fire", "deviceIds": []string{"a"}, "body": model.RuleLabLabel{DeviceID: "a", EventType: "FIRE", Start: start, End: start + 1000, Conclusion: "UNKNOWN", Basis: "人工现场核实资料尚不充分"}}
	label := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rule-lab/labels", token, labelQ, 201)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rule-lab/labels/"+label["id"].(string)+"/confirm", token, map[string]any{"expectedVersion": 99}, 409)
	confirmed := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/rule-lab/labels/"+label["id"].(string)+"/confirm", token, map[string]any{"expectedVersion": 1}, 201)
	if confirmed["version"] != float64(2) || confirmed["body"].(map[string]any)["confirmedBy"] != "analyst" {
		t.Fatal(confirmed)
	}
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = nil
	if _, err = repo.SaveAccessState(ctx, "t", state); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/rule-lab/datasets/"+dataset["id"].(string), token, nil, 403)
}
