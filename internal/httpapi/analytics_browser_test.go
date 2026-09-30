package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/adapters/rawstore"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/dataquality"
	"iot-platform/internal/analytics/dataquality/testkit"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

// This optional fixture serves the real API against an isolated PostgreSQL
// schema for browser checks. It never starts ingress, production processors or
// scheduled jobs. The stop file permits cleanup after a separate browser run.
func TestAnalyticsBrowserFixture(t *testing.T) {
	if os.Getenv("IOT_TEST_ANALYTICS_BROWSER") != "1" {
		t.Skip("explicit browser fixture required")
	}
	dsn, password, stopFile := os.Getenv("IOT_TEST_POSTGRES_DSN"), os.Getenv("IOT_TEST_ADMIN_PASSWORD"), os.Getenv("IOT_TEST_ANALYTICS_BROWSER_STOP_FILE")
	if dsn == "" || password == "" || stopFile == "" {
		t.Fatal("database, fixture password and stop file required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("fixture database configuration failed")
	}
	defer pool.Close()
	schema := fmt.Sprintf("analytics_browser_%d", time.Now().UnixNano())
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal("fixture schema creation failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("fixture schema cleanup failed")
		}
	}()
	fixtureDSN := analyticsBrowserDSN(dsn, schema)
	seedConfig, err := pgxpool.ParseConfig(fixtureDSN)
	if err != nil {
		t.Fatal("fixture database configuration failed")
	}
	seed, err := pgxpool.NewWithConfig(ctx, seedConfig)
	if err != nil {
		t.Fatal("fixture connection failed")
	}
	defer seed.Close()
	var activeSchema string
	if err = seed.QueryRow(ctx, "SELECT current_schema()").Scan(&activeSchema); err != nil || activeSchema != schema {
		t.Fatal("browser fixture schema isolation failed")
	}
	repo, err := postgres.New(ctx, fixtureDSN)
	if err != nil {
		t.Fatal("fixture repository initialization failed")
	}
	defer repo.Close()
	const tenant = "ai-app-e2e"
	data, err := testkit.SeedAt(ctx, repo, tenant, 40, time.Now().Add(-2*time.Minute).Truncate(time.Second).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "quality-device-hidden", TenantID: tenant, ProductID: data.ProductID, Name: "范围隔离验证设备", AccessKey: "fixture-hidden"}); err != nil {
		t.Fatal(err)
	}
	// Current-only dependency fixtures are registered records, never running
	// collectors. Their creation cannot reconstruct the preceding sample window.
	const profileID = "monitoring-fixture-access-profile"
	if err = repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{ID: profileID, TenantID: tenant, DeviceID: data.DeviceID, ProductID: data.ProductID, ProtocolID: "synthetic-json", CollectorID: "monitoring-fixture-collector", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	device, err := repo.GetManagedDevice(ctx, tenant, data.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	device.ConnectorProfileID = profileID
	device.GatewayID = "quality-device-hidden"
	if err = repo.SaveManagedDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	const secondDevice = "monitoring-device-second"
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: secondDevice, TenantID: tenant, ProductID: data.ProductID, Name: "隔离静默子设备", DeviceRole: "CHILD", GatewayID: data.DeviceID, ConnectorProfileID: profileID, AccessKey: "fixture-monitoring-second"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.DutyTransaction(ctx, tenant, func(tx ports.DutyTx) error {
		for index, event := range []struct {
			device, status string
			at             int64
		}{{data.DeviceID, "CONNECTED", data.Start - 1000}, {data.DeviceID, "DISCONNECTED", data.Start + 20000}, {data.DeviceID, "CONNECTED", data.Start + 30000}, {secondDevice, "CONNECTED", data.Start - 1000}} {
			state := model.DeviceState{TenantID: tenant, DeviceID: event.device, ProductID: data.ProductID, ConnectionStatus: event.status, StatusSource: "isolated-synthetic-monitoring-test"}
			body, _ := json.Marshal(state)
			if err := tx.AppendEvent(model.DutyBusinessEvent{ID: fmt.Sprintf("monitoring-fixture-state-%d", index), TenantID: tenant, Type: "DEVICE_CONNECTION_CHANGED", Source: "device", ResourceID: event.device, DeviceID: event.device, OccurredAt: event.at, Body: body}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = seed.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=$1,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC' WHERE source<>'configuration_history'`, data.Start-5000); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{AdminUser: "admin", AdminPassword: password, AdminTenants: []string{tenant}, JWTSecret: "analytics-browser-fixture-secret-32-characters", DevMode: true, InstanceID: "analytics-browser-fixture", Analytics: config.AnalyticsConfig{Poll: 100 * time.Millisecond}}
	engine := &core.Engine{Repo: repo, Parsers: parser.NewPlatformRegistry(t.TempDir())}
	engine.Archive, err = local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine.RawStore = rawstore.New(rawstore.Config{PostgreSQL: repo, Resolver: repo})
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAnalysisStorage(repo, repo)
	// Registration now cannot reconstruct activation during the old sample
	// window; the browser exercises HISTORICAL_SIMULATION on a real revision.
	ruleRevision, err := engine.PublishRule(ctx, model.RulePublishRequest{
		Rule: model.AlarmRule{ID: "rule-lab-fixture-pressure", TenantID: tenant, ProductID: data.ProductID, Name: "隔离压力实验基线", AlarmType: "HIGH_PRESSURE", Level: "HIGH", Enabled: true,
			Conditions: []model.RuleCondition{{Field: data.AttributeID, Operator: ">", Value: 100.5}}, Recovery: []model.RuleCondition{{Field: data.AttributeID, Operator: "<=", Value: 100}}},
		ExpectedBaselineVersion: 0, Reason: "isolated synthetic rule experiment fixture", Actor: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	go api.RunAnalysisWorkers(ctx)
	go api.RunAnalysisAIWorkers(ctx)
	actor := analytics.Actor{TenantID: tenant, Username: "admin"}
	qualityConfig, err := api.quality.SaveProfile(ctx, actor, data.ProfileRequest)
	if err != nil {
		t.Fatal(err)
	}
	qualityParameters, _ := json.Marshal(model.QualityRunParameters{AttributeIDs: []string{data.AttributeID}, ProfileRevisionIDs: []string{qualityConfig.ID}})
	qualityRequest := analytics.CreateRequest{DeviceIDs: []string{data.DeviceID}, Start: data.Start, End: data.End, Parameters: qualityParameters, IdempotencyKey: "fixture-fixed-quality"}
	if err = api.quality.ValidateCreate(ctx, actor, &qualityRequest); err != nil {
		t.Fatal(err)
	}
	qualityRun, err := api.analysis.Create(ctx, actor, analytics.KindDataQuality, dataquality.AlgorithmVersion, qualityRequest)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 200; attempt++ {
		qualityRun, err = api.analysis.Get(ctx, actor, analytics.KindDataQuality, qualityRun.ID)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(qualityRun.Status) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if qualityRun.Status != model.AnalysisSucceeded && qualityRun.Status != model.AnalysisPartial {
		t.Fatal("fixture fixed quality analysis failed")
	}
	addr := os.Getenv("IOT_TEST_ANALYTICS_BROWSER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8092"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	if path := os.Getenv("IOT_TEST_ANALYTICS_BROWSER_INFO"); path != "" {
		monitoringBody, _ := json.Marshal(model.MonitoringProfile{TargetType: "DEVICE", EffectiveFrom: data.Start, Mode: "periodic", Attributes: []model.MonitoringAttribute{{ID: data.AttributeID, ValueType: "number"}}, MessageTypes: []model.MessageType{model.PropertyReport}, Merge: "ALL", PeriodMs: 1000, ToleranceMs: 100, Importance: "人工确认的验证重点", LongGapMs: 5000, FrequentGapCount: 2})
		monitoringInfo := map[string]any{"deviceIds": []string{data.DeviceID, secondDevice}, "start": data.Start, "end": data.End, "attributeId": data.AttributeID, "periodMs": 1000, "toleranceMs": 100, "observationStart": data.Start + 10000, "observationEnd": data.Start + 15000, "qualityRunId": qualityRun.ID, "expected": map[string]any{"synthetic": true, "connectionOnlineMs": 30000, "connectionOfflineMs": 10000, "eventExpiredBeforeAvailable": true, "connectionSecondOnlineMs": 40000}}
		info, _ := json.Marshal(map[string]any{"tenantId": tenant, "deviceId": data.DeviceID, "hiddenDeviceId": "quality-device-hidden", "start": data.Start, "end": data.End, "periodMs": 1000, "unit": "kPa", "sampleCount": 40, "address": listener.Addr().String(), "profileRequest": data.ProfileRequest, "baselineRequest": data.BaselineRequest, "monitoringProfileRequest": model.MonitoringConfigRequest{ResourceID: "monitoring-profile", Scope: "personal", DeviceIDs: []string{data.DeviceID, secondDevice}, Body: monitoringBody}, "dependencyProfileId": profileID, "dependencyCollectorId": "monitoring-fixture-collector", "monitoring": monitoringInfo, "ruleLab": map[string]any{"deviceIds": []string{data.DeviceID}, "start": data.Start, "end": data.End, "warmupStart": data.Start, "ruleId": ruleRevision.RuleID, "baselineRevisionId": ruleRevision.ID, "expected": map[string]any{"synthetic": true, "sourceInputCount": 40, "baselineCycles": 8, "candidateCycles": 8}}})
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, info, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("isolated PostgreSQL browser fixture ready", listener.Addr().String())
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("browser fixture exceeded time budget")
		case err := <-serveErr:
			if err != http.ErrServerClosed {
				t.Fatal(err)
			}
			return
		case <-ticker.C:
			if _, err := os.Stat(stopFile); err == nil {
				cancel()
				return
			}
		}
	}
}

func analyticsBrowserDSN(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, _ := url.Parse(dsn)
		q := u.Query()
		q.Set("search_path", schema+",public")
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + " search_path='" + schema + ",public'"
}

func TestAnalyticsBrowserDSNIncludesActualIsolatedSearchPath(t *testing.T) {
	for _, dsn := range []string{"postgres://test-user:fixture-only@localhost/test-db?sslmode=disable", "host=localhost user=test-user dbname=test-db"} {
		cfg, err := pgx.ParseConfig(analyticsBrowserDSN(dsn, "owned_fixture"))
		if err != nil || cfg.RuntimeParams["search_path"] != "owned_fixture,public" {
			t.Fatal("test fixture does not preserve its database isolation")
		}
	}
}
