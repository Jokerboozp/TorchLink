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
	"slices"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/dataquality"
	"iot-platform/internal/analytics/dataquality/testkit"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func qualityIntegrationService(t *testing.T, repo *Repository, facts ports.AnalyticsFactStore) *dataquality.Service {
	t.Helper()
	a := analytics.Actor{TenantID: "quality-test", Username: "quality-analyst", AllDevices: true, Permissions: []string{"*"}, AccessVersion: "fixture-access-v1"}
	analysis := analytics.NewService(repo, config.AnalyticsConfig{Workers: 1, BatchSize: 50, Poll: time.Millisecond, Lease: time.Second, RunTimeout: time.Minute}, func(_ context.Context, _ analytics.Actor) (analytics.Actor, error) { return a, nil }, func(ctx context.Context, tenant, id string) error {
		_, err := repo.GetManagedDevice(ctx, tenant, id)
		return err
	})
	svc := dataquality.NewService(analysis, facts)
	svc.Catalog = repo
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	return svc
}
func runQualityIntegration(t *testing.T, svc *dataquality.Service, seed testkit.SeedResult, revision model.AnalysisConfigRevision, key string) model.AnalysisRun {
	t.Helper()
	a := analytics.Actor{TenantID: seed.TenantID, Username: "quality-analyst"}
	parameters, _ := json.Marshal(model.QualityRunParameters{AttributeIDs: []string{seed.AttributeID}, ProfileRevisionIDs: []string{revision.ID}})
	q := analytics.CreateRequest{DeviceIDs: []string{seed.DeviceID}, Start: seed.Start, End: seed.End, ConfigurationVersion: "selected", Parameters: parameters, IdempotencyKey: key}
	if err := svc.ValidateCreate(context.Background(), a, &q); err != nil {
		t.Fatal(err)
	}
	run, err := svc.Analysis.Create(context.Background(), a, analytics.KindDataQuality, dataquality.AlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Analysis.RunWorkers(ctx, "integration", slog.New(slog.NewTextHandler(io.Discard, nil)))
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
				t.Fatal("quality integration failed", run.Error)
			}
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("quality integration did not finish")
	return run
}
func TestDataQualityPostgresRealStorageAndBaselineConfirmation(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	seed, err := testkit.Seed(ctx, repo, "quality-test", 40)
	if err != nil {
		t.Fatal(err)
	}
	// The time-series values are synthetic. This isolated schema declares the
	// controlled fixture's observation coverage; it is not a deployment backfill.
	if _, err = repo.pool.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=$1,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC'`, seed.Start-1000); err != nil {
		t.Fatal(err)
	}
	// A decoder may produce several standard messages from one raw frame. A
	// property-less secondary message must count as a standard result without
	// becoming a pressure sample or a second raw reception.
	if _, err = repo.SaveStandardMessageIfAbsent(ctx, model.StandardMessage{TenantID: seed.TenantID, MessageID: "quality-fixture-expanded", RawMessageID: "raw-quality-fixture-0000", DeviceID: seed.DeviceID, ProductID: seed.ProductID, MessageType: model.PropertyReport, Timestamp: seed.Start + 100, Properties: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if err = repo.AnalyticsFactsRead(ctx, seed.TenantID, func(reader ports.AnalyticsFactReader) error {
		page, e := reader.ListRawParseOutcomes(model.FactQuery{DeviceIDs: []string{seed.DeviceID}, Start: seed.Start, End: seed.End, Limit: 1000})
		if e != nil {
			return e
		}
		for _, v := range page.Items {
			if v.RawMessageID == "raw-quality-fixture-0000" && v.SuccessfulStandardMessages != 2 {
				return fmt.Errorf("multi-result raw count was flattened")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc := qualityIntegrationService(t, repo, repo)
	actor := analytics.Actor{TenantID: seed.TenantID, Username: "quality-analyst"}
	revision, err := svc.SaveProfile(ctx, actor, seed.ProfileRequest)
	if err != nil {
		t.Fatal(err)
	}
	run := runQualityIntegration(t, svc, seed, revision, "pg-quality")
	if run.Status != model.AnalysisSucceeded {
		t.Fatal("complete controlled sequence did not succeed", run)
	}
	snapshot, err := svc.Analysis.Snapshot(ctx, actor, analytics.KindDataQuality, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var counters struct {
		ParseByDevice map[string]struct{ ObservedRaw, SuccessfulStandardMessages int } `json:"parseByDevice"`
	}
	if err = json.Unmarshal(snapshot.Statistics, &counters); err != nil || counters.ParseByDevice[seed.DeviceID].ObservedRaw != 40 || counters.ParseByDevice[seed.DeviceID].SuccessfulStandardMessages != 41 {
		t.Fatal("raw and standard denominators conflated", counters, err)
	}
	curve, err := svc.Series(ctx, actor, run.ID, seed.DeviceID, seed.AttributeID, 1000)
	if err != nil || len(curve.Items) != 40 || !curve.Complete {
		t.Fatal(curve, err)
	}
	for _, v := range curve.Items {
		if v.Unit != "kPa" || v.ProtocolVersion != "quality-protocol-v1" || v.ConfigurationVersion != "quality-config-v1" || v.AvailableAt <= v.ReceivedAt {
			t.Fatal("first availability or physical version lost", v)
		}
	}
	seed.BaselineRequest.ProfileRevisionID = revision.ID
	body, _ := json.Marshal(seed.BaselineRequest)
	baseline, err := svc.BuildBaseline(ctx, actor, model.QualityConfigRequest{ResourceID: "quality-baseline", Scope: "personal", DeviceIDs: []string{seed.DeviceID}, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := svc.ConfirmBaseline(ctx, actor, baseline.ID, baseline.Version)
	if err != nil {
		t.Fatal(err)
	}
	var b model.QualityBaseline
	_ = json.Unmarshal(confirmed.Body, &b)
	if b.SampleCount != 40 || b.Median != 102 || b.MAD != 1 || b.ConfirmedBy != actor.Username {
		t.Fatal("verified source baseline lost transparent statistics", b)
	}
	if _, err = svc.ConfirmBaseline(ctx, actor, baseline.ID, baseline.Version); err == nil {
		t.Fatal("old baseline revision overwritten")
	}
	before, err := svc.Export(ctx, actor, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.pool.Exec(ctx, `DELETE FROM standard_message WHERE tenant_id=$1 AND message_id='quality-fixture-0000'`, seed.TenantID); err != nil {
		t.Fatal(err)
	}
	curve, err = svc.Series(ctx, actor, run.ID, seed.DeviceID, seed.AttributeID, 1000)
	if err != nil || curve.Complete || curve.AvailableCount != 39 {
		t.Fatal("real source expiry hidden", curve, err)
	}
	after, err := svc.Export(ctx, actor, run.ID)
	if err != nil || string(before) != string(after) {
		t.Fatal("source retention changed immutable export", err)
	}
}
func TestDataQualityPostgresClickHouseRealControlledSequenceParity(t *testing.T) {
	baseURL := os.Getenv("IOT_TEST_CLICKHOUSE_URL")
	if baseURL == "" {
		t.Skip("IOT_TEST_CLICKHOUSE_URL is not configured")
	}
	repo := testRepository(t)
	ctx := context.Background()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal("invalid test ClickHouse URL")
	}
	database := fmt.Sprintf("quality_test_%d", time.Now().UnixNano())
	params := parsed.Query()
	params.Set("database", database)
	parsed.RawQuery = params.Encode()
	ch, err := clickhouse.New(ctx, parsed.String(), repo)
	if err != nil {
		t.Fatal("existing VM ClickHouse fixture setup failed")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		u := *parsed
		p := u.Query()
		p.Del("database")
		p.Set("query", "DROP DATABASE IF EXISTS "+database)
		u.RawQuery = p.Encode()
		request, _ := http.NewRequestWithContext(cleanup, http.MethodPost, u.String(), nil)
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
	seed, err := testkit.Seed(ctx, ch, "quality-test", 40)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.pool.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=$1,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC'`, seed.Start-1000); err != nil {
		t.Fatal(err)
	}
	pgsvc := qualityIntegrationService(t, repo, repo)
	actor := analytics.Actor{TenantID: seed.TenantID, Username: "quality-analyst"}
	revision, err := pgsvc.SaveProfile(ctx, actor, seed.ProfileRequest)
	if err != nil {
		t.Fatal(err)
	}
	pgRun := runQualityIntegration(t, pgsvc, seed, revision, "parity-pg")
	chsvc := qualityIntegrationService(t, repo, ch)
	chRun := runQualityIntegration(t, chsvc, seed, revision, "parity-ch")
	if pgRun.Status != model.AnalysisSucceeded || chRun.Status != model.AnalysisSucceeded {
		t.Fatal("controlled source incomplete", pgRun.Status, chRun.Status)
	}
	first, err := pgsvc.Series(ctx, actor, pgRun.ID, seed.DeviceID, seed.AttributeID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := chsvc.Series(ctx, actor, chRun.ID, seed.DeviceID, seed.AttributeID, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 40 || len(second.Items) != 40 {
		t.Fatal("source sequence size differs", len(first.Items), len(second.Items))
	}
	for i := range first.Items {
		a, b := first.Items[i], second.Items[i]
		if a.ID != b.ID || a.Value != b.Value || a.EventAt != b.EventAt || a.ReceivedAt != b.ReceivedAt || a.ProtocolVersion != b.ProtocolVersion || a.Unit != b.Unit {
			t.Fatalf("same identity %s: value %v(%T)/%v(%T), clocks %d/%d %d/%d, protocol %q/%q, unit %q/%q", a.ID, a.Value, a.Value, b.Value, b.Value, a.EventAt, b.EventAt, a.ReceivedAt, b.ReceivedAt, a.ProtocolVersion, b.ProtocolVersion, a.Unit, b.Unit)
		}
	}
	normalize := func(run model.AnalysisRun) map[string]json.RawMessage {
		outputs, _, err := repo.ListAnalysisOutputs(ctx, seed.TenantID, model.AnalysisFilter{Kind: "metrics", RunID: run.ID})
		if err != nil || len(outputs) != 1 {
			t.Fatal(outputs, err)
		}
		var body map[string]json.RawMessage
		_ = json.Unmarshal(outputs[0].Body, &body)
		var metrics []map[string]json.RawMessage
		_ = json.Unmarshal(body["metrics"], &metrics)
		for _, m := range metrics {
			delete(m, "id")
		}
		data, _ := json.Marshal(metrics)
		body["metrics"] = data
		delete(body, "limitations")
		return body
	}
	a, b := normalize(pgRun), normalize(chRun)
	keys := []string{}
	for key := range a {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if key == "algorithmVersion" || key == "metrics" || key == "originalFindingCount" {
			if strings.TrimSpace(string(a[key])) != strings.TrimSpace(string(b[key])) {
				t.Fatal("deterministic quality metrics differ by storage", key)
			}
		}
	}
}
