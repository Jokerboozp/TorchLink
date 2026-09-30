package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/analytics/dataquality/testkit"
	"iot-platform/internal/analytics/monitoring"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func monitoringIntegrationService(t *testing.T, repo *Repository, facts ports.AnalyticsFactStore) *monitoring.Service {
	t.Helper()
	quality := qualityIntegrationService(t, repo, facts)
	svc := monitoring.NewService(quality.Analysis, facts)
	svc.Catalog = repo
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	return svc
}
func monitoringIntegrationProfile(t *testing.T, svc *monitoring.Service, seed testkit.SeedResult, start, period int64) model.AnalysisConfigRevision {
	t.Helper()
	body, _ := json.Marshal(model.MonitoringProfile{Mode: "periodic", EffectiveFrom: start, PeriodMs: period, ToleranceMs: 100, Merge: "ALL", Attributes: []model.MonitoringAttribute{{ID: seed.AttributeID, ValueType: "number"}}, MessageTypes: []model.MessageType{model.PropertyReport}, LongGapMs: period, FrequentGapCount: 2, Importance: "人工登记的隔离验证测点"})
	revision, err := svc.SaveProfile(context.Background(), analytics.Actor{TenantID: seed.TenantID, Username: "quality-analyst"}, model.MonitoringConfigRequest{ResourceID: fmt.Sprint("monitoring-", start), Scope: "personal", DeviceIDs: []string{seed.DeviceID}, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
func runMonitoringIntegration(t *testing.T, svc *monitoring.Service, seed testkit.SeedResult, revision model.AnalysisConfigRevision, qualityIDs []string, key string) model.AnalysisRun {
	t.Helper()
	a := analytics.Actor{TenantID: seed.TenantID, Username: "quality-analyst"}
	parameters, _ := json.Marshal(model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}, QualityRunIDs: qualityIDs})
	q := analytics.CreateRequest{DeviceIDs: []string{seed.DeviceID}, Start: seed.Start, End: seed.End, Parameters: parameters, IdempotencyKey: key}
	if err := svc.ValidateCreate(context.Background(), a, &q); err != nil {
		t.Fatal(err)
	}
	run, err := svc.Analysis.Create(context.Background(), a, analytics.KindMonitoring, monitoring.AlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Analysis.RunWorkers(ctx, "monitoring-integration", slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		run, err = svc.Analysis.Store.GetAnalysisRun(context.Background(), seed.TenantID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			if run.Status == model.AnalysisFailed {
				t.Fatal("monitoring integration failed", run.Error)
			}
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("monitoring integration did not finish")
	return run
}
func monitoringIntegrationMetrics(t *testing.T, repo *Repository, run model.AnalysisRun) map[string]continuity.Metric {
	t.Helper()
	outputs, _, err := repo.ListAnalysisOutputs(context.Background(), run.TenantID, model.AnalysisFilter{RunID: run.ID, Kind: "metrics", Limit: 100})
	if err != nil || len(outputs) != 1 {
		t.Fatal("fixed monitoring metric missing", err, len(outputs))
	}
	var body struct {
		Metrics []continuity.Metric `json:"metrics"`
	}
	if err = json.Unmarshal(outputs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	result := map[string]continuity.Metric{}
	for _, v := range body.Metrics {
		if v.ProfileID == "" && v.AttributeID == "" {
			result[v.Track] = v
		}
	}
	return result
}
func monitoringIntegrationFacts(t *testing.T, store ports.AnalyticsFactStore, seed testkit.SeedResult, source string) []model.MeasurementFact {
	t.Helper()
	values := []model.MeasurementFact{}
	err := store.AnalyticsFactsRead(context.Background(), seed.TenantID, func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: []string{seed.DeviceID}, Properties: []string{seed.AttributeID}, Start: seed.Start, End: seed.End, AvailabilitySource: source, Limit: 7}
		for {
			page, err := reader.QueryMeasurementSeries(q)
			if err != nil {
				return err
			}
			values = append(values, page.Items...)
			if !page.HasMore {
				return nil
			}
			if page.Cursor == "" || page.Cursor == q.Cursor {
				return fmt.Errorf("fact cursor stalled")
			}
			q.Cursor = page.Cursor
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return values
}
func TestMonitoringPostgresRealExpiredReplayAndFixedQualitySnapshot(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	seed, err := testkit.Seed(ctx, repo, "quality-test", 40)
	if err != nil {
		t.Fatal(err)
	}
	// Only this temporary schema declares the synthetic observation coverage.
	// First-availability receipts remain the database's actual ACK timestamps.
	if _, err = repo.pool.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=$1,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC'`, seed.Start-1200); err != nil {
		t.Fatal(err)
	}
	quality := qualityIntegrationService(t, repo, repo)
	actor := analytics.Actor{TenantID: seed.TenantID, Username: "quality-analyst"}
	qp, err := quality.SaveProfile(ctx, actor, seed.ProfileRequest)
	if err != nil {
		t.Fatal(err)
	}
	qr := runQualityIntegration(t, quality, seed, qp, "monitoring-fixed-quality")
	svc := monitoring.NewService(quality.Analysis, repo)
	svc.Catalog = repo
	if err = svc.Register(); err != nil {
		t.Fatal(err)
	}
	revision := monitoringIntegrationProfile(t, svc, seed, seed.Start-1200, 1000)
	run := runMonitoringIntegration(t, svc, seed, revision, []string{qr.ID}, "pg-monitoring-expired")
	metrics := monitoringIntegrationMetrics(t, repo, run)
	if run.Status != model.AnalysisPartial || metrics["data"].AvailableMs != 0 || metrics["data"].UnavailableMs != 40000 || metrics["received"].AvailableMs != 39980 || metrics["connection"].UnknownMs != 40000 {
		t.Fatal("old replay refreshed monitoring or erased independent receipt", run.Status, metrics)
	}
	for _, v := range monitoringIntegrationFacts(t, repo, seed, "") {
		if v.AvailableAt <= seed.End || v.AvailableAtSource != "postgres_standard_commit" || v.Unit != "kPa" || v.ProtocolVersion != "quality-protocol-v1" {
			t.Fatal("actual source metadata was fabricated", v.ID, v.AvailableAtSource)
		}
	}
	snapshot, err := svc.Analysis.Snapshot(ctx, actor, analytics.KindMonitoring, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		QualitySnapshots []struct {
			RunID     string `json:"runId"`
			FactsHash string `json:"factsHash"`
		} `json:"qualitySnapshots"`
		AvailabilitySources map[string]int `json:"availabilitySources"`
	}
	if err = json.Unmarshal(snapshot.Statistics, &summary); err != nil || len(summary.QualitySnapshots) != 1 || summary.QualitySnapshots[0].RunID != qr.ID || summary.QualitySnapshots[0].FactsHash == "" || summary.AvailabilitySources["postgres_standard_commit"] != 40 {
		t.Fatal("fixed quality/source reference missing", summary, err)
	}
	before, err := svc.Export(ctx, actor, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.pool.Exec(ctx, `DELETE FROM standard_message WHERE tenant_id=$1`, seed.TenantID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Export(ctx, actor, run.ID)
	if err != nil || string(before) != string(after) {
		t.Fatal("source retention rewrote fixed monitoring result", err)
	}
}

func monitoringClickHouseFixture(t *testing.T, repo *Repository) *clickhouse.Repository {
	t.Helper()
	base := os.Getenv("IOT_TEST_CLICKHOUSE_URL")
	if base == "" {
		t.Skip("IOT_TEST_CLICKHOUSE_URL is not configured")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal("invalid test ClickHouse URL")
	}
	database := fmt.Sprintf("monitoring_test_%d", time.Now().UnixNano())
	params := parsed.Query()
	params.Set("database", database)
	parsed.RawQuery = params.Encode()
	ch, err := clickhouse.New(context.Background(), parsed.String(), repo)
	if err != nil {
		t.Fatal("existing VM ClickHouse fixture setup failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		u := *parsed
		p := u.Query()
		p.Del("database")
		p.Set("query", "DROP DATABASE IF EXISTS "+database)
		u.RawQuery = p.Encode()
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Error("isolated ClickHouse fixture cleanup failed")
			return
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Error("isolated ClickHouse fixture cleanup rejected")
		}
	})
	return ch
}
func TestMonitoringPostgresClickHouseRealClockParityAndUnknownACK(t *testing.T) {
	repo := testRepository(t)
	ch := monitoringClickHouseFixture(t, repo)
	ctx := context.Background()
	seed, err := testkit.Seed(ctx, ch, "quality-test", 40)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.pool.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=$1,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC'`, seed.Start-1200); err != nil {
		t.Fatal(err)
	}
	pg := monitoringIntegrationService(t, repo, repo)
	pg.AvailabilitySource = "clickhouse_telemetry_ack"
	revision := monitoringIntegrationProfile(t, pg, seed, seed.Start-1200, 1000)
	pgRun := runMonitoringIntegration(t, pg, seed, revision, nil, "same-ack-pg")
	chs := monitoringIntegrationService(t, repo, ch)
	chRun := runMonitoringIntegration(t, chs, seed, revision, nil, "same-ack-ch")
	first, second := monitoringIntegrationFacts(t, repo, seed, "clickhouse_telemetry_ack"), monitoringIntegrationFacts(t, ch, seed, "")
	if len(first) != 40 || len(second) != 40 {
		t.Fatal("fixed source sequence size differs", len(first), len(second))
	}
	for i, a := range first {
		b := second[i]
		if a.ID != b.ID || a.AvailableAt != b.AvailableAt || a.EventAt != b.EventAt || a.ReceivedAt != b.ReceivedAt || a.Value != b.Value || a.Unit != b.Unit || a.ProtocolVersion != b.ProtocolVersion {
			t.Fatal("actual common source clocks/value disagree", a.ID, b.ID)
		}
	}
	a, b := monitoringIntegrationMetrics(t, repo, pgRun), monitoringIntegrationMetrics(t, repo, chRun)
	for track, x := range a {
		y := b[track]
		x.DeviceID, y.DeviceID = "", ""
		if !reflect.DeepEqual(x, y) {
			t.Fatal("same actual availability stage changed metrics", track, x, y)
		}
	}
	if a["received"].AvailableMs != 39980 || a["data"].AvailableMs != 0 || b["data"].UnavailableMs != 40000 {
		t.Fatal("expired replay became new data", a, b)
	}
	// Existing stored values without a first ACK are unknown on the available
	// track, while reception still has independently recorded coverage.
	if _, err = repo.pool.Exec(ctx, `DELETE FROM measurement_availability WHERE tenant_id=$1`, seed.TenantID); err != nil {
		t.Fatal(err)
	}
	unknownRun := runMonitoringIntegration(t, chs, seed, revision, nil, "unknown-ack-ch")
	unknown := monitoringIntegrationMetrics(t, repo, unknownRun)
	if unknownRun.Status != model.AnalysisPartial || unknown["data"].UnknownMs != 40000 || unknown["received"].AvailableMs != 39980 || unknown["received"].UnknownMs != 0 {
		t.Fatal("missing ACK erased proven receipt or became a known gap", unknownRun.Status, unknown)
	}
	// A new actual-time sample demonstrates the default clocks are different
	// storage stages. No ACK is backdated to make the tracks match.
	event := time.Now().UnixMilli()
	id := "monitoring-fresh"
	raw := model.RawMessage{TenantID: seed.TenantID, MessageID: "raw-" + id, DeviceID: seed.DeviceID, ProductID: seed.ProductID, ReceivedAt: event, ProtocolVersion: "real-ack-fixture-v1", Payload: []byte(`{"pressure":101}`)}
	if _, err = ch.ReserveRawMessage(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if err = ch.SaveRawMessage(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = ch.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: seed.TenantID, MessageID: raw.MessageID, DeviceID: seed.DeviceID, ProductID: seed.ProductID, ReceivedAt: event, ArchivedAt: time.Now().UnixMilli(), ObjectBucket: "postgres", ObjectKey: raw.MessageID}); err != nil {
		t.Fatal(err)
	}
	if _, err = ch.SaveStandardMessageIfAbsent(ctx, model.StandardMessage{TenantID: seed.TenantID, MessageID: id, RawMessageID: raw.MessageID, DeviceID: seed.DeviceID, ProductID: seed.ProductID, MessageType: model.PropertyReport, Timestamp: event, Properties: map[string]any{"pressure": 101.0}, Tags: map[string]string{"unit:pressure": "kPa", "configurationVersion": "fresh-config-v1"}}); err != nil {
		t.Fatal(err)
	}
	fresh := seed
	fresh.Start = event
	fresh.End = time.Now().UnixMilli() + 1
	pgValue, chValue := monitoringIntegrationFacts(t, repo, fresh, ""), monitoringIntegrationFacts(t, ch, fresh, "")
	if len(pgValue) != 1 || len(chValue) != 1 {
		t.Fatal("fresh actual source sample missing", len(pgValue), len(chValue))
	}
	if chValue[0].AvailableAt <= pgValue[0].AvailableAt || chValue[0].AvailableAtSource != "clickhouse_telemetry_ack" || pgValue[0].AvailableAtSource != "postgres_standard_commit" {
		t.Fatal("independent storage ACK stage not retained", pgValue[0].AvailableAtSource, chValue[0].AvailableAtSource)
	}
	fresh.End = chValue[0].AvailableAt + 1
	pg.AvailabilitySource = ""
	pr := monitoringIntegrationProfile(t, pg, fresh, event-1200, 10000)
	pgFresh := runMonitoringIntegration(t, pg, fresh, pr, nil, "default-fresh-pg")
	chFresh := runMonitoringIntegration(t, chs, fresh, pr, nil, "default-fresh-ch")
	pm, cm := monitoringIntegrationMetrics(t, repo, pgFresh), monitoringIntegrationMetrics(t, repo, chFresh)
	if pm["data"].AvailableMs != fresh.End-pgValue[0].AvailableAt || cm["data"].AvailableMs != 1 || pm["data"].AvailableMs <= cm["data"].AvailableMs {
		t.Fatal("actual ACK delay disappeared", pm["data"], cm["data"])
	}
}
