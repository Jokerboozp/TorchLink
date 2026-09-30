package dataquality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type factFixture struct {
	mu           sync.Mutex
	values       []model.MeasurementFact
	raw          []model.RawParseOutcomeFact
	windowReads  int
	membersReads int
	block        chan struct{}
	entered      chan struct{}
	once         sync.Once
}
type fixtureReader struct {
	ports.AnalyticsFactReader
	source *factFixture
	ctx    context.Context
	values []model.MeasurementFact
	raw    []model.RawParseOutcomeFact
}

func (f *factFixture) AnalyticsFactsRead(ctx context.Context, tenant string, fn func(ports.AnalyticsFactReader) error) error {
	f.mu.Lock()
	values := slices.Clone(f.values)
	raw := slices.Clone(f.raw)
	f.mu.Unlock()
	return fn(&fixtureReader{source: f, ctx: ctx, values: values, raw: raw})
}
func (f *fixtureReader) QueryMeasurementSeries(q model.FactQuery) (model.FactPage[model.MeasurementFact], error) {
	f.source.mu.Lock()
	if len(q.Members) > 0 {
		f.source.membersReads++
	} else {
		f.source.windowReads++
	}
	block, entered := f.source.block, f.source.entered
	f.source.mu.Unlock()
	if len(q.Members) > 0 && block != nil {
		if entered != nil {
			f.source.once.Do(func() { close(entered) })
		}
		select {
		case <-f.ctx.Done():
			return model.FactPage[model.MeasurementFact]{}, f.ctx.Err()
		case <-block:
		}
	}
	out := []model.MeasurementFact{}
	members := map[string]bool{}
	for _, m := range q.Members {
		members[m.MessageID+"\x00"+m.Property] = true
	}
	for _, v := range f.values {
		if !slices.Contains(q.DeviceIDs, v.DeviceID) || len(q.Properties) > 0 && !slices.Contains(q.Properties, v.Property) {
			continue
		}
		if len(q.Members) > 0 {
			if !members[v.MessageID+"\x00"+v.Property] {
				continue
			}
		} else {
			at := v.EventAt
			if q.TimeBasis == "RECEIVED" {
				at = v.ReceivedAt
			}
			if at < q.Start || at >= q.End {
				continue
			}
		}
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b model.MeasurementFact) int { return strings.Compare(a.ID, b.ID) })
	offset := 0
	if q.Cursor != "" {
		_, _ = fmt.Sscan(q.Cursor, &offset)
	}
	limit := q.Limit
	if limit == 0 {
		limit = 500
	}
	end := min(len(out), offset+limit)
	page := model.FactPage[model.MeasurementFact]{Items: out[offset:end], FactPageMeta: model.FactPageMeta{Complete: true, HasMore: end < len(out), Source: model.FactSourceCoverage{Source: "fixture_standard_message", SourceVersion: "fixture-v1", ReadAt: time.Now().UnixMilli(), CoverageStart: q.Start, CoverageEnd: q.End, Complete: true, Status: "AVAILABLE", HistoricalReconstructionQuality: "RECORDED"}}}
	if page.HasMore {
		page.Cursor = fmt.Sprint(end)
	}
	return page, nil
}
func (f *fixtureReader) ListRawParseOutcomes(q model.FactQuery) (model.FactPage[model.RawParseOutcomeFact], error) {
	items := []model.RawParseOutcomeFact{}
	for _, v := range f.raw {
		if slices.Contains(q.DeviceIDs, v.DeviceID) && v.ReceivedAt >= q.Start && v.ReceivedAt < q.End {
			items = append(items, v)
		}
	}
	offset := 0
	if q.Cursor != "" {
		_, _ = fmt.Sscan(q.Cursor, &offset)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	end := min(len(items), offset+limit)
	page := model.FactPage[model.RawParseOutcomeFact]{Items: items[offset:end], FactPageMeta: model.FactPageMeta{Complete: true, HasMore: end < len(items), Source: model.FactSourceCoverage{Source: "fixture_raw", SourceVersion: "fixture-v1", ReadAt: time.Now().UnixMilli(), CoverageStart: q.Start, CoverageEnd: q.End, Complete: true, Status: "AVAILABLE"}}}
	if page.HasMore {
		page.Cursor = fmt.Sprint(end)
	}
	return page, nil
}
func serviceFixture(t *testing.T) (*Service, *factFixture, analytics.Actor, model.QualityConfigRequest) {
	t.Helper()
	repo := memory.NewRepository()
	ctx := context.Background()
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "pressure", DataType: "float", Unit: "kPa"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, ProductID: "p", AccessKey: id})
	}
	actor := analytics.Actor{TenantID: "t", Username: "analyst", AllDevices: true, AccessVersion: "access-v1", Permissions: []string{"*"}}
	analysis := analytics.NewService(analytics.NewMemoryStore(), config.AnalyticsConfig{BatchSize: 10, Workers: 1, Lease: 300 * time.Millisecond, Poll: time.Millisecond, RunTimeout: time.Minute}, func(_ context.Context, a analytics.Actor) (analytics.Actor, error) { return actor, nil }, func(_ context.Context, tenant, id string) error {
		_, err := repo.GetManagedDevice(ctx, tenant, id)
		return err
	})
	facts := &factFixture{}
	svc := NewService(analysis, facts)
	svc.Catalog = repo
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Minute).UnixMilli()
	minv, maxv, epsilon := 0.0, 10.0, .1
	body, _ := json.Marshal(model.QualityProfile{AttributeID: "pressure", Mode: "periodic", EffectiveFrom: start, ScheduleAnchor: start, PeriodMs: 1000, ToleranceMs: 100, ValueType: "number", Required: true, Unit: "kPa", UnitConfirmed: true, RangeConfirmed: true, Minimum: &minv, Maximum: &maxv, Epsilon: &epsilon, StableDurationMs: 10000, MinimumSamples: 30, MaxSequenceGapMs: 2000})
	return svc, facts, actor, model.QualityConfigRequest{ResourceID: "pressure", Scope: "personal", DeviceIDs: []string{"a"}, Body: body}
}
func launchRun(t *testing.T, svc *Service, a analytics.Actor, revision model.AnalysisConfigRevision, start, end int64) model.AnalysisRun {
	t.Helper()
	parameters, _ := json.Marshal(model.QualityRunParameters{AttributeIDs: []string{"pressure"}, ProfileRevisionIDs: []string{revision.ID}})
	q := analytics.CreateRequest{DeviceIDs: []string{"a"}, Start: start, End: end, ConfigurationVersion: "selection", Parameters: parameters, IdempotencyKey: fmt.Sprint(time.Now().UnixNano())}
	if err := svc.ValidateCreate(context.Background(), a, &q); err != nil {
		t.Fatal(err)
	}
	run, err := svc.Analysis.Create(context.Background(), a, analytics.KindDataQuality, AlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func runWorkers(t *testing.T, s *Service) (context.CancelFunc, chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Analysis.RunWorkers(ctx, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	return cancel, done
}
func terminalRun(t *testing.T, s *Service, id string) model.AnalysisRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := s.Analysis.Store.GetAnalysisRun(context.Background(), "t", id)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			if run.Status == model.AnalysisFailed {
				t.Fatal("analysis failed", run.Error)
			}
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("run did not complete")
	return model.AnalysisRun{}
}
func seedMeasurements(f *factFixture, start int64, count int) {
	for i := 0; i < count; i++ {
		value := float64(1)
		if i == 12 {
			value = 20
		}
		id := fmt.Sprintf("m%03d", i)
		f.values = append(f.values, model.MeasurementFact{ID: id + ":pressure", MessageID: id, RawMessageID: "raw-" + id, TenantID: "t", ProductID: "p", DeviceID: "a", Property: "pressure", Value: value, MessageType: model.PropertyReport, EventAt: start + int64(i)*1000, ReceivedAt: start + int64(i)*1000 + 20, AvailableAt: start + int64(i)*1000 + 40, AvailableAtSource: "fixture_ack", Unit: "kPa", ProtocolVersion: "protocol-v1", ConfigurationVersion: "configuration-v1", OperatingCondition: "confirmed-normal", HistoricalReconstructionQuality: "RECORDED"})
		f.raw = append(f.raw, model.RawParseOutcomeFact{RawMessageID: "raw-" + id, MessageID: id, DeviceID: "a", ReceivedAt: start + int64(i)*1000 + 20, ArchivedAt: start + int64(i)*1000 + 30, ParseAttemptedAt: start + int64(i)*1000 + 40, Outcome: "STANDARD_SAVED", SuccessfulStandardMessages: 1})
	}
}
func TestQualityEndToEndFrozenMetricsReviewsExportAndExpiredCurve(t *testing.T) {
	svc, facts, actor, q := serviceFixture(t)
	revision, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	var profile model.QualityProfile
	_ = json.Unmarshal(q.Body, &profile)
	start := profile.EffectiveFrom
	seedMeasurements(facts, start, 40)
	run := launchRun(t, svc, actor, revision, start, start+40000)
	cancel, done := runWorkers(t, svc)
	defer func() { cancel(); <-done }()
	run = terminalRun(t, svc, run.ID)
	if run.Status != model.AnalysisSucceeded {
		t.Fatal(run.Status, run.Error)
	}
	outputs, _, err := svc.Analysis.Store.ListAnalysisOutputs(context.Background(), "t", model.AnalysisFilter{RunID: run.ID, Kind: "metrics"})
	if err != nil || len(outputs) != 1 {
		t.Fatal(outputs, err)
	}
	var body map[string]any
	_ = json.Unmarshal(outputs[0].Body, &body)
	metrics := body["metrics"].([]any)
	complete := metrics[0].(map[string]any)["eventCompleteness"].(map[string]any)
	if complete["expected"] != float64(40) || complete["covered"] != float64(40) {
		t.Fatal("source denominator changed", complete)
	}
	findings, _, _ := svc.Analysis.Store.ListAnalysisOutputs(context.Background(), "t", model.AnalysisFilter{RunID: run.ID, Kind: "findings"})
	if len(findings) == 0 {
		t.Fatal("range anomaly missing")
	}
	review, err := svc.Review(context.Background(), actor, findings[0].ID, model.QualityReviewRequest{RunID: run.ID, ExpectedRunVersion: run.Version, Result: "CONFIRMED_PROBLEM", Explanation: "现场校准依据待核实", IdempotencyKey: "review"})
	if err != nil || review.Reviewer != actor.Username {
		t.Fatal(review, err)
	}
	data, err := svc.Export(context.Background(), actor, run.ID)
	if err != nil || !json.Valid(data) {
		t.Fatal(string(data), err)
	}
	curve, err := svc.Series(context.Background(), actor, run.ID, "a", "pressure", 5)
	if err != nil || curve.OriginalCount != 40 || curve.ReturnedCount != 5 || !curve.Downsampled {
		t.Fatal(curve, err)
	}
	facts.mu.Lock()
	facts.values = facts.values[1:]
	facts.mu.Unlock()
	curve, err = svc.Series(context.Background(), actor, run.ID, "a", "pressure", 5)
	if err != nil || curve.Complete || curve.AvailableCount != 39 {
		t.Fatal("source expiry hidden", curve, err)
	}
	again, _, _ := svc.Analysis.Store.ListAnalysisOutputs(context.Background(), "t", model.AnalysisFilter{RunID: run.ID, Kind: "metrics"})
	if string(again[0].Body) != string(outputs[0].Body) {
		t.Fatal("curve expiry changed frozen metric")
	}
}
func TestQualityRestartUsesFixedMembersAndPartialOnSourceLoss(t *testing.T) {
	svc, facts, actor, q := serviceFixture(t)
	revision, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	var p model.QualityProfile
	_ = json.Unmarshal(q.Body, &p)
	seedMeasurements(facts, p.EffectiveFrom, 40)
	facts.block = make(chan struct{})
	facts.entered = make(chan struct{})
	run := launchRun(t, svc, actor, revision, p.EffectiveFrom, p.EffectiveFrom+40000)
	cancel, done := runWorkers(t, svc)
	select {
	case <-facts.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("member read not reached")
	}
	facts.mu.Lock()
	frozenReads := facts.windowReads
	facts.mu.Unlock()
	cancel()
	<-done
	frozen, err := svc.Analysis.Store.GetAnalysisRun(context.Background(), "t", run.ID)
	if err != nil || !frozen.InputsFrozen || frozen.Status == model.AnalysisFailed {
		t.Fatal(frozen, err)
	}
	facts.mu.Lock()
	facts.values = facts.values[1:]
	late := facts.values[0]
	late.ID = "late:pressure"
	late.MessageID = "late"
	facts.values = append(facts.values, late)
	facts.block = nil
	facts.mu.Unlock()
	time.Sleep(350 * time.Millisecond)
	cancel, done = runWorkers(t, svc)
	defer func() { cancel(); <-done }()
	finished := terminalRun(t, svc, run.ID)
	if finished.Status != model.AnalysisPartial {
		t.Fatal("expired frozen input claimed complete", finished)
	}
	facts.mu.Lock()
	reads := facts.windowReads
	facts.mu.Unlock()
	if reads != frozenReads {
		t.Fatal("restart rescanned event/reception windows", reads)
	}
	curve, err := svc.Series(context.Background(), actor, run.ID, "a", "pressure", 1000)
	if err != nil || curve.OriginalCount != 40 || curve.AvailableCount != 39 {
		t.Fatal("late data replaced frozen member", curve, err)
	}
}
func TestQualityPersonalSharedAndPhysicalConfigBoundaries(t *testing.T) {
	svc, _, actor, q := serviceFixture(t)
	var profile model.QualityProfile
	_ = json.Unmarshal(q.Body, &profile)
	profile.ToleranceMs = 500
	q.Body, _ = json.Marshal(profile)
	if _, err := svc.SaveProfile(context.Background(), actor, q); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("half-period tolerance accepted", err)
	}
	profile.ToleranceMs = 100
	profile.Unit = "bar"
	q.Body, _ = json.Marshal(profile)
	if _, err := svc.SaveProfile(context.Background(), actor, q); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("unit mismatch accepted", err)
	}
	profile.Unit = "kPa"
	profile.ProductID = "p"
	q.Body, _ = json.Marshal(profile)
	q.Scope = "shared"
	if _, err := svc.SaveProfile(context.Background(), actor, q); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("partial product scope published", err)
	}
	q.DeviceIDs = []string{"a", "b"}
	saved, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	other := actor
	other.Username = "other"
	svc.Analysis.Resolve = func(_ context.Context, a analytics.Actor) (analytics.Actor, error) {
		a.Permissions = []string{"*"}
		a.AllDevices = true
		return a, nil
	}
	if _, err = svc.GetConfig(context.Background(), other, model.DataQualityProfileKind, saved.ID); err != nil {
		t.Fatal(err)
	}
	q.Scope = "personal"
	q.ResourceID = "private"
	q.DeviceIDs = []string{"a"}
	private, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.GetConfig(context.Background(), other, model.DataQualityProfileKind, private.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("other user's personal config exposed", err)
	}
}
func TestQualityPublicJSONPreservesDateTextMeasurement(t *testing.T) {
	value := "2026-10-01T00:00:00Z"
	data := publicJSON(map[string]any{"eventAt": time.UnixMilli(123456), "value": value, "operatingCondition": value, "nested": map[string]any{"value": value}})
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body["value"] != value || body["operatingCondition"] != value || body["eventAt"] != float64(123456) {
		t.Fatal("time conversion corrupted string measurements", body)
	}
}

func TestQualitySharedProfileInheritanceAndImmutableScopeHash(t *testing.T) {
	svc, facts, actor, q := serviceFixture(t)
	q.Scope = "shared"
	q.DeviceIDs = []string{"a", "b"}
	var p model.QualityProfile
	_ = json.Unmarshal(q.Body, &p)
	p.TargetType = "PRODUCT"
	p.ProductID = "p"
	q.Body, _ = json.Marshal(p)
	revision, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	actor.AllDevices = false
	actor.DeviceIDs = []string{"a"}
	svc.Analysis.Resolve = func(_ context.Context, a analytics.Actor) (analytics.Actor, error) {
		a.AllDevices = false
		a.DeviceIDs = []string{"a"}
		a.Permissions = []string{"*"}
		a.AccessVersion = actor.AccessVersion
		return a, nil
	}
	visible, err := svc.GetConfig(context.Background(), actor, model.DataQualityProfileKind, revision.ID)
	if err != nil || !slices.Equal(visible.DeviceIDs, []string{"a"}) {
		t.Fatal("shared profile membership leaked", visible, err)
	}
	page, total, err := svc.ListConfigs(context.Background(), actor, "profiles", model.AnalysisFilter{})
	if err != nil || total != 1 || !slices.Equal(page[0].DeviceIDs, []string{"a"}) {
		t.Fatal(page, total, err)
	}
	seedMeasurements(facts, p.EffectiveFrom, 40)
	run := launchRun(t, svc, actor, visible, p.EffectiveFrom, p.EffectiveFrom+40000)
	cancel, done := runWorkers(t, svc)
	defer func() { cancel(); <-done }()
	run = terminalRun(t, svc, run.ID)
	if run.Status != model.AnalysisSucceeded || !slices.Equal(run.DeviceIDs, []string{"a"}) {
		t.Fatal("projected revision hash or scope changed", run)
	}
	manifest, _, err := svc.loadManifest(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range manifest.Members {
		if member.DeviceID != "a" || member.Metadata.Value != nil {
			t.Fatal("manifest copied values or hidden device", member)
		}
	}
}

func TestQualityTimeoutProducesPartialSnapshotAtFrozenBoundary(t *testing.T) {
	svc, facts, actor, q := serviceFixture(t)
	svc.Analysis.Limits.RunTimeout = 50 * time.Millisecond
	facts.block = make(chan struct{})
	revision, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	var p model.QualityProfile
	_ = json.Unmarshal(q.Body, &p)
	seedMeasurements(facts, p.EffectiveFrom, 40)
	run := launchRun(t, svc, actor, revision, p.EffectiveFrom, p.EffectiveFrom+40000)
	cancel, done := runWorkers(t, svc)
	defer func() { cancel(); <-done }()
	run = terminalRun(t, svc, run.ID)
	if run.Status != model.AnalysisPartial || !run.InputsFrozen {
		t.Fatal("timeout lost fixed input or partial state", run)
	}
	snapshot, err := svc.Analysis.Snapshot(context.Background(), actor, analytics.KindDataQuality, run.ID)
	if err != nil || !slices.Contains(snapshot.Limitations, "RUN_TIME_BUDGET_EXHAUSTED") || len(snapshot.AffectedIntervals) == 0 {
		t.Fatal(snapshot, err)
	}
	var counters map[string]any
	_ = json.Unmarshal(snapshot.Statistics, &counters)
	if counters["expectedSeries"] != float64(1) || counters["analysedSeries"] != float64(0) || counters["processedMeasurements"] != float64(40) {
		t.Fatal("timeout invented calculated denominators", counters)
	}
}

func TestQualitySharedCalibrationAttachmentsRespectPublicationScope(t *testing.T) {
	svc, _, actor, _ := serviceFixture(t)
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.Archive = archive
	personal, err := svc.UploadAttachment(context.Background(), actor, []string{"a"}, "personal", "calibration.txt", "text/plain", 4, strings.NewReader("test"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(model.QualityCalibration{DeviceID: "a", AttributeID: "pressure", CalibratedAt: time.Now().Add(-time.Hour).UnixMilli(), Basis: "受控测试依据", ImplementedBy: "测试人员", Attachments: []string{personal.ID}})
	q := model.QualityConfigRequest{ResourceID: "calibration", Scope: "shared", DeviceIDs: []string{"a"}, Body: body}
	if _, err := svc.SaveCalibration(context.Background(), actor, q); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("shared record referenced private evidence", err)
	}
	shared, err := svc.UploadAttachment(context.Background(), actor, []string{"a"}, "shared", "shared.txt", "text/plain", 4, strings.NewReader("test"))
	if err != nil {
		t.Fatal(err)
	}
	var cal model.QualityCalibration
	_ = json.Unmarshal(body, &cal)
	cal.Attachments = []string{shared.ID}
	q.Body, _ = json.Marshal(cal)
	if _, err := svc.SaveCalibration(context.Background(), actor, q); err != nil {
		t.Fatal(err)
	}
	other := actor
	other.Username = "other"
	svc.Analysis.Resolve = func(_ context.Context, a analytics.Actor) (analytics.Actor, error) {
		a.AllDevices = true
		a.Permissions = []string{"*"}
		return a, nil
	}
	_, reader, err := svc.DownloadAttachment(context.Background(), other, shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	if _, _, err := svc.DownloadAttachment(context.Background(), other, personal.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("personal attachment leaked", err)
	}
}

func TestQualityCreateRejectsDuplicateOverlapAndExcessiveSlots(t *testing.T) {
	svc, _, actor, q := serviceFixture(t)
	revision, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	var p model.QualityProfile
	_ = json.Unmarshal(q.Body, &p)
	params := model.QualityRunParameters{AttributeIDs: []string{"pressure"}, ProfileRevisionIDs: []string{revision.ID, revision.ID}}
	body, _ := json.Marshal(params)
	request := analytics.CreateRequest{DeviceIDs: []string{"a"}, Start: p.EffectiveFrom, End: p.EffectiveFrom + 10000, Parameters: body}
	if err := svc.ValidateCreate(context.Background(), actor, &request); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("duplicate fixed versions accepted", err)
	}
	q.ResourceID = "pressure-overlap"
	second, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	params.ProfileRevisionIDs = []string{revision.ID, second.ID}
	request.Parameters, _ = json.Marshal(params)
	if err := svc.ValidateCreate(context.Background(), actor, &request); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("overlapping effective versions queued", err)
	}
	params.ProfileRevisionIDs = []string{revision.ID}
	request.Parameters, _ = json.Marshal(params)
	request.End = request.Start + int64(MaxSlots)*p.PeriodMs
	if err := svc.ValidateCreate(context.Background(), actor, &request); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("slot resource protection bypassed", err)
	}
}

func TestQualityReadCapIsDisclosedAndCannotBecomeComplete(t *testing.T) {
	svc, facts, actor, q := serviceFixture(t)
	svc.Analysis.Limits.BatchSize = 1000
	svc.RecordLimit = 40
	var p model.QualityProfile
	_ = json.Unmarshal(q.Body, &p)
	p.Mode = "event"
	p.PeriodMs = 0
	p.ToleranceMs = 0
	q.Body, _ = json.Marshal(p)
	revision, err := svc.SaveProfile(context.Background(), actor, q)
	if err != nil {
		t.Fatal(err)
	}
	seedMeasurements(facts, p.EffectiveFrom, svc.RecordLimit+1)
	run := launchRun(t, svc, actor, revision, p.EffectiveFrom, p.EffectiveFrom+int64(svc.RecordLimit+1)*1000)
	// Verify the freeze boundary directly through the real execution/store
	// contract; no large curve values are copied into the manifest.
	if err := svc.Analysis.Register(analytics.KindDataQuality, func(ctx context.Context, e *analytics.Execution) error {
		revisions, params, err := svc.configsForRun(ctx, e.Run)
		if err != nil {
			return err
		}
		manifest, _, err := svc.freeze(ctx, e, revisions, params)
		if err != nil {
			return err
		}
		if len(manifest.Members) != svc.RecordLimit || !slices.Contains(manifest.Limitations, "MEASUREMENT_READ_CAP_REACHED") || !slices.Contains(manifest.Limitations, "RAW_READ_CAP_REACHED") {
			return fmt.Errorf("read caps were not disclosed")
		}
		for _, member := range manifest.Members {
			if member.Metadata.Value != nil {
				return fmt.Errorf("manifest copied raw values")
			}
		}
		hash, _ := analytics.AnalysisHash(manifest)
		snapshot := model.AnalysisSnapshot{ID: e.Run.ID + "/snapshot", DataCutoff: manifest.DataCutoff, InputHashes: []string{hash}, Sources: manifest.Sources, Statistics: json.RawMessage(`{"protectedRead":true}`), Limitations: manifest.Limitations, UncomputableMetrics: []string{"full-series"}}
		return e.Commit(ctx, model.AnalysisBatch{ID: "cap-final", Processed: int64(len(manifest.Members)), Status: model.AnalysisPartial, Stage: "read-limit", Snapshot: &snapshot})
	}); err != nil {
		t.Fatal(err)
	}
	cancel, done := runWorkers(t, svc)
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		run, err = svc.Analysis.Store.GetAnalysisRun(context.Background(), "t", run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run.Status != model.AnalysisPartial || !run.InputsFrozen {
		t.Fatal("cap did not retain partial fixed-input state", run.Status, run.Error)
	}
}

func TestQualityAttachmentNewDigestAndLegacyRead(t *testing.T) {
	svc, _, actor, _ := serviceFixture(t)
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.Archive = archive
	attachment, err := svc.UploadAttachment(context.Background(), actor, []string{"a"}, "personal", "calibration.txt", "text/plain", 4, strings.NewReader("test"))
	if err != nil || len(attachment.SHA256) != 64 {
		t.Fatal("upload digest missing", err)
	}
	revision, err := svc.Analysis.Store.GetAnalysisConfig(context.Background(), actor.TenantID, attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
	var record model.QualityAttachmentRecord
	_ = json.Unmarshal(revision.Body, &record)
	if _, err = archive.PutObject(context.Background(), AttachmentBucket, record.StorageKey, strings.NewReader("fake"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if _, reader, err := svc.DownloadAttachment(context.Background(), actor, attachment.ID); !errors.Is(err, model.ErrAnalysisInvalid) || reader != nil {
		t.Fatal("tampered attachment exposed", err)
	}
	if _, err = svc.UploadAttachment(context.Background(), actor, []string{"a"}, "personal", "wrong.txt", "text/plain", 4, strings.NewReader("longer")); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("declared size mismatch accepted", err)
	}
	record.ID = "legacy"
	record.StorageKey = actor.TenantID + "/legacy"
	record.SHA256 = ""
	body, _ := json.Marshal(record)
	if _, err = archive.PutObject(context.Background(), AttachmentBucket, record.StorageKey, strings.NewReader("test"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Analysis.Store.PutAnalysisConfig(context.Background(), model.AnalysisConfigRevision{ID: record.ID, TenantID: actor.TenantID, Kind: model.DataQualityAttachmentKind, ResourceID: record.ID, Scope: "PERSONAL", Creator: actor.Username, DeviceIDs: []string{"a"}, Body: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	old, reader, err := svc.DownloadAttachment(context.Background(), actor, record.ID)
	if err != nil || old.SHA256 != "" {
		t.Fatal("legacy original digest invented", err)
	}
	reader.Close()
}
