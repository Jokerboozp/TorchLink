package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
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
	aiadapter "iot-platform/internal/adapters/ai"
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
	// The fixture has archived and successfully parsed these immutable samples;
	// mark their controlled processing complete for the source-field directory.
	if _, err = seed.Exec(ctx, `UPDATE standard_message SET processed_at=$2 WHERE tenant_id=$1`, tenant, time.Now().UnixMilli()); err != nil {
		t.Fatal("isolated successful sample processing fixture failed")
	}
	// Governance browser facts are isolated historical REPORT observations. They
	// never enter ingress or create/modify production alarms or device state.
	governanceObservations := []string{}
	for index := 0; index < 3; index++ {
		at := data.Start + int64(index)*10000
		observation, _, err := repo.SaveAlarmObservation(ctx, model.AlarmObservation{
			TenantID: tenant, DeviceID: data.DeviceID, AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE", FactKind: "REPORT",
			SourceSystem: "ISOLATED_BROWSER_FIXTURE", SourceEventID: fmt.Sprintf("governance-browser-report-%d", index), EventIndex: "alarm",
			EventAt: at, ReceivedAt: at + 100, RecordedAt: at + 200, AvailableAt: at + 200, TimeQuality: "TRUSTED", IdentityQuality: "PLATFORM_INPUT",
			HistoricalQuality: "CONTROLLED_SYNTHETIC", Acceptance: "ACCEPTED", Payload: map[string]any{"synthetic": true, "purpose": "historical browser verification"},
		})
		if err != nil {
			t.Fatal("isolated governance observation seed failed", err)
		}
		governanceObservations = append(governanceObservations, observation.ID)
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
		if _, err := tx.Put(model.NewDutyDocument(model.DutyItemKind, "governance-browser-duty-item", model.DutyItem{Title: "隔离治理值班核查来源", DeviceID: data.DeviceID, OwnerID: "admin", Status: "OPEN", NextAction: "持续核查隔离现场", CreatedBy: "admin"}), 0); err != nil {
			return err
		}
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
	const videoID = "governance-browser-video-event"
	camera := model.VideoCameraMapping{TenantID: tenant, CameraID: "governance-browser-camera", CameraName: "隔离历史证据摄像头", DeviceID: data.DeviceID, Enabled: true}
	if err = repo.SaveVideoCameraMapping(ctx, camera); err != nil {
		t.Fatal("isolated video binding seed failed", err)
	}
	picture, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	videoKey := fmt.Sprintf("%s/%s/%s/snapshot-%s.png", tenant, time.UnixMilli(data.Start).UTC().Format("2006/01/02"), videoID, camera.CameraID)
	videoURL, e := engine.Archive.PutObject(ctx, "video-alarm", videoKey, bytes.NewReader(picture), int64(len(picture)), "image/png")
	if e != nil {
		t.Fatal("isolated video archive seed failed", e)
	}
	if _, err = repo.SaveVideoEvent(ctx, model.VideoAlarmEvent{TenantID: tenant, EventID: videoID, CameraID: camera.CameraID, CameraName: camera.CameraName, AlarmType: "FIRE", AlarmName: "隔离历史视频事件", EventTime: data.Start, ReceivedAt: data.Start + 100, Source: "ISOLATED_BROWSER_FIXTURE", SnapshotURL: videoURL, Raw: map[string]any{"synthetic": true, "mediaTransferStatus": "STORED"}}); err != nil {
		t.Fatal("isolated video event seed failed", err)
	}
	engine.RawStore = rawstore.New(rawstore.Config{PostgreSQL: repo, Resolver: repo})
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAnalysisStorage(repo, repo)
	if harnessURL := os.Getenv("IOT_TEST_LIVE_HARNESS_URL"); harnessURL != "" {
		workflow, e := aiadapter.NewHarnessPool(harnessURL, os.Getenv("IOT_TEST_LIVE_HARNESS_TOKEN"), "http://host.orb.internal:18181/mcp/harness", os.Getenv("IOT_TEST_LIVE_HARNESS_MODEL"), 2*time.Minute)
		if e != nil {
			t.Fatal("live Harness configuration failed")
		}
		engine.AIWorkflows, engine.HarnessTokens = workflow, api.auth
		api.analysis.AI.StopRunner = workflow.StopWorkflowRun
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
		governanceInfo := map[string]any{"observationIds": governanceObservations, "dutyItemId": "governance-browser-duty-item", "videoEventId": videoID, "alarmType": "FIRE", "originKind": "DEVICE_DIRECT", "signalKey": "device:FIRE", "templateRevisionId": "template-general-v1", "scenePresetRevisionId": "scene-kitchen-v1", "typeProfileRevisionId": "profile-report-only-v1", "start": data.Start, "end": data.End, "synthetic": true}
		info, _ := json.Marshal(map[string]any{"schema": schema, "tenantId": tenant, "deviceId": data.DeviceID, "hiddenDeviceId": "quality-device-hidden", "start": data.Start, "end": data.End, "periodMs": 1000, "unit": "kPa", "sampleCount": 40, "address": listener.Addr().String(), "profileRequest": data.ProfileRequest, "baselineRequest": data.BaselineRequest, "monitoringProfileRequest": model.MonitoringConfigRequest{ResourceID: "monitoring-profile", Scope: "personal", DeviceIDs: []string{data.DeviceID, secondDevice}, Body: monitoringBody}, "dependencyProfileId": profileID, "dependencyCollectorId": "monitoring-fixture-collector", "monitoring": monitoringInfo, "governance": governanceInfo})
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
