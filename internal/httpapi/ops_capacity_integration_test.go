package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/capacity"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
)

// Observe the real in-memory platform writes, including processing completion.
// Only the PostgreSQL/ClickHouse observer is replaced; onboarding, credentials,
// HTTP ingress, parsing and the capacity controller/Worker are production code.
type capacityPlatformRepository struct {
	*memory.Repository
	engine    *core.Engine
	mu        sync.Mutex
	processed map[string]int64
}

func (s *capacityPlatformRepository) MarkStandardMessageProcessed(ctx context.Context, tenant, id string, token int64) error {
	if err := s.Repository.MarkStandardMessageProcessed(ctx, tenant, id, token); err != nil {
		return err
	}
	s.mu.Lock()
	s.processed[tenant+"/"+id] = time.Now().UnixMilli()
	s.mu.Unlock()
	return nil
}

func (s *capacityPlatformRepository) CompleteStandardMessage(ctx context.Context, state *model.DeviceState, tenant, id string, token int64) (bool, error) {
	ok, err := s.Repository.CompleteStandardMessage(ctx, state, tenant, id, token)
	if ok && err == nil {
		s.mu.Lock()
		s.processed[tenant+"/"+id] = time.Now().UnixMilli()
		s.mu.Unlock()
	}
	return ok, err
}

type capacityPlatformStore struct{ *capacityPlatformRepository }

func (s *capacityPlatformStore) RawIndexes(ctx context.Context, tenant string, ids []string) (map[string]capacity.RawRecord, error) {
	out := map[string]capacity.RawRecord{}
	for _, id := range ids {
		v, err := s.GetRawIndex(ctx, tenant, id)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = capacity.RawRecord{PayloadHash: v.PayloadHash, Published: v.PublishedAt > 0, ParseError: v.ParseError}
	}
	return out, nil
}

func (s *capacityPlatformStore) RawBodies(ctx context.Context, tenant string, ids []string) (map[string]int, error) {
	out := map[string]int{}
	for _, id := range ids {
		idx, err := s.GetRawIndex(ctx, tenant, id)
		if err != nil {
			return nil, err
		}
		if _, err = s.engine.GetRaw(ctx, idx); err != nil {
			return nil, err
		}
		out[id] = 1
	}
	return out, nil
}

func (s *capacityPlatformStore) Standards(ctx context.Context, tenant string, ids []string) (map[string][]capacity.StandardRecord, error) {
	out := map[string][]capacity.StandardRecord{}
	for _, id := range ids {
		v, err := s.GetStandardMessageByRaw(ctx, tenant, id)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		at := s.processed[tenant+"/"+v.MessageID]
		s.mu.Unlock()
		out[id] = []capacity.StandardRecord{{MessageID: v.MessageID, Type: string(v.MessageType), HasProperties: len(v.Properties) > 0, ProcessedAt: at}}
	}
	return out, nil
}

func (*capacityPlatformStore) TelemetryConfigured() bool { return false }
func (*capacityPlatformStore) Telemetry(context.Context, string, []string) (map[string]int, error) {
	return nil, errors.New("telemetry is not configured in this integration test")
}
func (*capacityPlatformStore) RuleAlarms(context.Context, string, string, []string, int64) (map[string][]capacity.AlarmRecord, error) {
	return nil, errors.New("alarm reconciliation is not configured in this integration test")
}
func (*capacityPlatformStore) ClockOffset(context.Context) (time.Duration, time.Duration, error) {
	return 0, time.Millisecond, nil
}
func (*capacityPlatformStore) Close() {}

func TestCapacityRunUsesAuthorizedTrialForUnverifiedTemplate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	repo := &capacityPlatformRepository{Repository: memory.NewRepository(), processed: map[string]int64{}}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	repo.engine = engine
	m := metrics.New()
	engine.Metrics = m
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "capacity-onboarding-integration-test-key"
	cfg.AdminUser, cfg.AdminTenants = "root", []string{"capacity-tenant"}
	api := New(cfg, engine, m, log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("root", "capacity-tenant", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.yaml")
	plan := `schemaVersion: 1
name: real-onboarding-regression
preset: quick
fixtures: {tenant: capacity-tenant, deviceCount: 1, reuseDevices: true, autoProvision: true, messageBytes: 200}
load: {ingressShare: {http: 1}, initialMessagesPerSecond: 2}
search: {rates: [2], warmup: 0s, measure: 10s, drainTimeout: 10s, cooldown: 1s, observeInterval: 1s}
budget: {maximumWallTime: 1m, maximumMessagesPerSecond: 2, maximumEvidenceGiB: 1}
outputs: {formats: [json]}
`
	if err = os.WriteFile(planPath, []byte(plan), 0600); err != nil {
		t.Fatal(err)
	}
	results := filepath.Join(dir, "results")
	runID, runErr := capacity.Run(ctx, capacity.RunOptions{
		PlanPath: planPath, ResultsDir: results, OperatorToken: token,
		Inventory: &capacity.Inventory{
			Name: "integration", API: server.URL,
			Metrics:   []capacity.MetricsTarget{{Role: "combined", Instance: "api", URL: server.URL + "/metrics"}},
			Agents:    []capacity.AgentTarget{{Name: "local"}},
			Observers: capacity.Observers{PostgresSecretRef: "observer"},
		},
		ExtraSecrets: map[string]string{"observer": "in-memory-observer-test-only"},
		NewStore: func(context.Context, string, string) (capacity.Store, error) {
			return &capacityPlatformStore{repo}, nil
		},
	})
	state, err := capacity.ReadState(results, runID)
	if err != nil {
		t.Fatalf("run failed before state creation: run=%v state=%v", runErr, err)
	}
	if runErr != nil || state.Status != capacity.StatusFinished || len(state.Completed) != 1 {
		events, _ := os.ReadFile(filepath.Join(results, runID, "events.jsonl"))
		t.Fatalf("fresh standard template must reach measurement: error=%v state=%+v events=%s", runErr, state, events)
	}
	var summary capacity.Summary
	b, err := os.ReadFile(filepath.Join(results, runID, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Integrity.UniqueSent == nil || *summary.Integrity.UniqueSent == 0 || summary.Integrity.UniqueBusinessCompleted == nil || *summary.Integrity.UniqueBusinessCompleted != *summary.Integrity.UniqueSent {
		t.Fatalf("real HTTP ingress did not complete: %+v", summary.Integrity)
	}
	product, err := repo.GetProduct(ctx, "capacity-tenant", capacity.AutoProductID)
	if err != nil || product.ProtocolPackageID != onboarding.StandardPackageID {
		t.Fatalf("standard template was not auto-provisioned: %+v %v", product, err)
	}
	_, ready, err := api.onboarding.TemplateReadiness(ctx, "capacity-tenant", product.ID)
	if err != nil || ready {
		t.Fatalf("capacity fixtures must not manufacture template acceptance: ready=%v err=%v", ready, err)
	}

	enroll := onboarding.EnrollRequest{RequestID: "ordinary", ProductID: product.ID, Device: onboarding.EnrollDevice{ID: "ordinary", Name: "普通设备"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}}
	ordinary := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/onboarding", token, enroll, 409)
	if !strings.Contains(ordinary["detail"].(string), "首台实机验证") {
		t.Fatalf("ordinary enrollment lost readiness protection: %v", ordinary)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/access/users", token, map[string]any{
		"username": "installer", "password": "capacity-installer-test-password", "enabled": true,
		"permissions": []string{"menu:devices", "POST /api/v1/device-registry"}, "deviceScope": "all",
	}, 200)
	installer := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/auth/login", "", map[string]any{
		"username": "installer", "password": "capacity-installer-test-password", "tenantId": "capacity-tenant",
	}, 200)["accessToken"].(string)
	enroll.Trial = true
	denied := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/onboarding", installer, enroll, 403)
	if !strings.Contains(denied["detail"].(string), "设备模板配置权限") {
		t.Fatalf("trial enrollment must require template editing permission: %v", denied)
	}
	if _, err = repo.GetManagedDevice(ctx, "capacity-tenant", "ordinary"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("rejected enrollment created a device: %v", err)
	}
}
