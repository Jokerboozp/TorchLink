package monitoring

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

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type fixture struct {
	mu                       sync.Mutex
	values                   []model.MeasurementFact
	states                   []model.DeviceStateIntervalFact
	deps                     []model.DependencyFact
	raw                      []model.RawParseOutcomeFact
	windowReads, memberReads int
	fail                     bool
	collectionStart          int64
}
type reader struct {
	ports.AnalyticsFactReader
	f               *fixture
	values          []model.MeasurementFact
	states          []model.DeviceStateIntervalFact
	deps            []model.DependencyFact
	raw             []model.RawParseOutcomeFact
	fail            bool
	collectionStart int64
}

func (f *fixture) AnalyticsFactsRead(ctx context.Context, tenant string, fn func(ports.AnalyticsFactReader) error) error {
	f.mu.Lock()
	r := reader{f: f, values: slices.Clone(f.values), states: slices.Clone(f.states), deps: slices.Clone(f.deps), raw: slices.Clone(f.raw), fail: f.fail, collectionStart: f.collectionStart}
	f.mu.Unlock()
	if r.fail {
		return errors.New("fixture source unavailable")
	}
	return fn(&r)
}
func factPage[T any](q model.FactQuery, items []T, source string) model.FactPage[T] {
	offset := 0
	if q.Cursor != "" {
		_, _ = fmt.Sscan(q.Cursor, &offset)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	end := min(len(items), offset+limit)
	page := model.FactPage[T]{Items: items[offset:end], FactPageMeta: model.FactPageMeta{Complete: true, HasMore: end < len(items), Source: model.FactSourceCoverage{Source: source, SourceVersion: "synthetic-fixture-v1", ReadAt: time.Now().UnixMilli(), CoverageStart: q.Start, CoverageEnd: q.End, CollectionStartedAt: 1, Complete: true, Status: "AVAILABLE", BackfillStatus: "FIXTURE", HistoricalReconstructionQuality: "CONTROLLED_SYNTHETIC"}}}
	if page.HasMore {
		page.Cursor = fmt.Sprint(end)
	}
	return page
}
func (r *reader) QueryMeasurementSeries(q model.FactQuery) (model.FactPage[model.MeasurementFact], error) {
	r.f.mu.Lock()
	if len(q.Members) > 0 {
		r.f.memberReads++
	} else {
		r.f.windowReads++
	}
	r.f.mu.Unlock()
	ids := map[string]bool{}
	for _, v := range q.Members {
		ids[v.MessageID+"\x00"+v.Property] = true
	}
	items := []model.MeasurementFact{}
	unknown := false
	for _, v := range r.values {
		if !slices.Contains(q.DeviceIDs, v.DeviceID) || len(q.Properties) > 0 && !slices.Contains(q.Properties, v.Property) {
			continue
		}
		if v.AvailableAt == 0 {
			unknown = true
		}
		if len(ids) > 0 {
			if !ids[v.MessageID+"\x00"+v.Property] {
				continue
			}
		} else {
			at := v.EventAt
			if q.TimeBasis == "RECEIVED" {
				at = v.ReceivedAt
			}
			if q.TimeBasis == "AVAILABLE" {
				at = v.AvailableAt
			}
			if at < q.Start || at >= q.End {
				continue
			}
		}
		items = append(items, v)
	}
	slices.SortFunc(items, func(a, b model.MeasurementFact) int { return strings.Compare(a.ID, b.ID) })
	page := factPage(q, items, "fixture_measurements")
	if unknown {
		page.Complete = false
		page.Source.Complete = false
		page.Source.Limitations = []string{"FIRST_AVAILABILITY_UNKNOWN"}
	}
	if r.collectionStart > q.Start {
		page.Complete = false
		page.Source.Complete = false
		page.Source.CoverageStart = min(q.End, r.collectionStart)
		page.Source.CollectionStartedAt = r.collectionStart
		page.Source.Limitations = append(page.Source.Limitations, "HISTORICAL_COLLECTION_OR_FUTURE_RANGE_NOT_COVERED")
	}
	return page, nil
}
func (r *reader) ListDeviceStateIntervals(q model.FactQuery) (model.FactPage[model.DeviceStateIntervalFact], error) {
	items := []model.DeviceStateIntervalFact{}
	for _, v := range r.states {
		if slices.Contains(q.DeviceIDs, v.DeviceID) && v.Start < q.End && v.End > q.Start {
			items = append(items, v)
		}
	}
	return factPage(q, items, "fixture_states"), nil
}
func (r *reader) GetDependencySnapshot(q model.FactQuery) (model.FactPage[model.DependencyFact], error) {
	items := []model.DependencyFact{}
	for _, v := range r.deps {
		if slices.Contains(q.DeviceIDs, v.DeviceID) && (v.Kind != "parent-device" || slices.Contains(q.DeviceIDs, v.ResourceID)) {
			items = append(items, v)
		}
	}
	return factPage(q, items, "fixture_dependencies"), nil
}
func (r *reader) ListRawParseOutcomes(q model.FactQuery) (model.FactPage[model.RawParseOutcomeFact], error) {
	items := []model.RawParseOutcomeFact{}
	for _, v := range r.raw {
		if slices.Contains(q.DeviceIDs, v.DeviceID) && v.ReceivedAt >= q.Start && v.ReceivedAt < q.End {
			items = append(items, v)
		}
	}
	return factPage(q, items, "fixture_raw"), nil
}
func setup(t *testing.T) (*Service, *fixture, analytics.Actor, model.MonitoringConfigRequest, int64) {
	t.Helper()
	repo := memory.NewRepository()
	ctx := context.Background()
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "pressure", DataType: "number", Unit: "kPa"}}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		d := model.ManagedDevice{TenantID: "t", ID: id, ProductID: "p", AccessKey: "fixture-" + id, ConnectorProfileID: "ingress"}
		if id == "b" {
			d.GatewayID = "a"
		}
		if err := repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "ingress", DeviceID: "a", ProductID: "p", CollectorID: "fixture-collector", UpdatedAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	a := analytics.Actor{TenantID: "t", Username: "analyst", Permissions: []string{"*"}, AllDevices: true, AccessVersion: "scope-v1"}
	analysis := analytics.NewService(analytics.NewMemoryStore(), config.AnalyticsConfig{BatchSize: 3, Workers: 1, Lease: 300 * time.Millisecond, Poll: time.Millisecond, RunTimeout: time.Minute}, func(_ context.Context, input analytics.Actor) (analytics.Actor, error) {
		input.Permissions = a.Permissions
		input.AllDevices = a.AllDevices
		input.DeviceIDs = a.DeviceIDs
		input.AccessVersion = a.AccessVersion
		return input, nil
	}, func(ctx context.Context, tenant, id string) error {
		_, err := repo.GetManagedDevice(ctx, tenant, id)
		return err
	})
	f := &fixture{}
	svc := NewService(analysis, f)
	svc.Catalog = repo
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).UnixMilli()
	body, _ := json.Marshal(model.MonitoringProfile{Mode: "periodic", EffectiveFrom: start, Attributes: []model.MonitoringAttribute{{ID: "pressure", ValueType: "number"}}, MessageTypes: []model.MessageType{model.PropertyReport}, Merge: "ALL", PeriodMs: 1000, ToleranceMs: 100, LongGapMs: 1000, FrequentGapCount: 2, Importance: "重点测点"})
	return svc, f, a, model.MonitoringConfigRequest{ResourceID: "pressure", Scope: "personal", DeviceIDs: []string{"a"}, Body: body}, start
}
func seed(f *fixture, start int64) {
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("m%03d", i)
		f.values = append(f.values, model.MeasurementFact{ID: id + ":pressure", TenantID: "t", DeviceID: "a", ProductID: "p", MessageID: id, RawMessageID: "raw-" + id, Property: "pressure", Value: 100.0, MessageType: model.PropertyReport, EventAt: start + int64(i)*1000, ReceivedAt: start + int64(i)*1000 + 20, AvailableAt: start + int64(i)*1000 + 40, AvailableAtSource: "synthetic_ack", Unit: "kPa", ProtocolVersion: "v1", ConfigurationVersion: "p1", HistoricalReconstructionQuality: "RECORDED"})
		f.raw = append(f.raw, model.RawParseOutcomeFact{RawMessageID: "raw-" + id, DeviceID: "a", ReceivedAt: start + int64(i)*1000 + 20, ArchivedAt: start + int64(i)*1000 + 25, ParseAttemptedAt: start + int64(i)*1000 + 35, MessageID: id, Outcome: "STANDARD_SAVED", SuccessfulStandardMessages: 1})
	}
	f.states = []model.DeviceStateIntervalFact{{DeviceID: "a", Start: start, End: start + 2000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "DISCONNECTED"}}, {DeviceID: "a", Start: start + 2000, End: start + 10000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "CONNECTED"}}, {DeviceID: "b", Start: start, End: start + 10000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "CONNECTED"}}}
	f.deps = []model.DependencyFact{{ID: "da", DeviceID: "a", Kind: "access-profile", ResourceID: "ingress", EffectiveFrom: start, EffectiveTo: start + 10000, RecordedAt: start, Quality: "RECORDED"}, {ID: "db", DeviceID: "b", Kind: "access-profile", ResourceID: "ingress", EffectiveFrom: start, EffectiveTo: start + 10000, RecordedAt: start, Quality: "RECORDED"}, {ID: "parent", DeviceID: "b", Kind: "parent-device", ResourceID: "a", EffectiveFrom: start, EffectiveTo: start + 10000, RecordedAt: start, Quality: "RECORDED"}}
}
func create(t *testing.T, s *Service, a analytics.Actor, devices []string, start int64, p model.MonitoringRunParameters) model.AnalysisRun {
	t.Helper()
	body, _ := json.Marshal(p)
	q := analytics.CreateRequest{DeviceIDs: devices, Start: start, End: start + 10000, Parameters: body, IdempotencyKey: fmt.Sprint(time.Now().UnixNano())}
	if err := s.ValidateCreate(context.Background(), a, &q); err != nil {
		t.Fatal(err)
	}
	r, err := s.Analysis.Create(context.Background(), a, analytics.KindMonitoring, AlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func workers(s *Service) (context.CancelFunc, chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Analysis.RunWorkers(ctx, "fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	return cancel, done
}
func completed(t *testing.T, s *Service, id string) model.AnalysisRun {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		r, err := s.Analysis.Store.GetAnalysisRun(context.Background(), "t", id)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(r.Status) {
			if r.Status == model.AnalysisFailed {
				t.Fatal("monitoring failed", r.Error)
			}
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("monitoring did not complete")
	return model.AnalysisRun{}
}
func TestMonitoringMissingWarmupDoesNotConfirmEarlyGap(t *testing.T) {
	s, f, a, q, start := setup(t)
	seed(f, start)
	f.collectionStart = start - 1000 // required prefix starts at start-1100
	revision, err := s.SaveProfile(context.Background(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	run := create(t, s, a, []string{"a"}, start, model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}})
	cancel, done := workers(s)
	defer func() { cancel(); <-done }()
	run = completed(t, s, run.ID)
	outputs, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Kind: "metrics", Limit: 100})
	if err != nil || len(outputs) != 1 {
		t.Fatal(outputs, err)
	}
	var body struct {
		Metrics     []continuity.Metric `json:"metrics"`
		Limitations []string            `json:"limitations"`
	}
	if err = json.Unmarshal(outputs[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	metrics := map[string]continuity.Metric{}
	for _, v := range body.Metrics {
		if v.ProfileID == "" && v.AttributeID == "" {
			metrics[v.Track] = v
		}
	}
	if metrics["received"].UnknownMs != 20 || metrics["received"].UnavailableMs != 0 || metrics["received"].AvailableMs != 9980 || metrics["data"].UnknownMs != 40 || metrics["data"].UnavailableMs != 0 || metrics["data"].AvailableMs != 9960 {
		t.Fatal("missing prefix became a confirmed gap or hid positive source proof", metrics)
	}
	snapshot, err := s.Analysis.Snapshot(context.Background(), a, analytics.KindMonitoring, run.ID)
	if err != nil || run.Status != model.AnalysisPartial || !slices.Contains(snapshot.Limitations, "SOURCE_WARMUP_NOT_COVERED") {
		t.Fatal("warmup limitation hidden", run.Status, snapshot.Limitations, err)
	}
}
func TestMonitoringCommonGapAndDependencyEvidenceUseFrozenFacts(t *testing.T) {
	s, f, a, q, start := setup(t)
	seed(f, start)
	q.Scope = "shared"
	q.DeviceIDs = []string{"a", "b"}
	revision, err := s.SaveProfile(context.Background(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	run := create(t, s, a, q.DeviceIDs, start, model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}, CommonGapPolicy: &model.MonitoringCommonGapPolicy{Version: "manual-v1", MinimumGapCount: 1, MinimumOverlapMs: 20, MinimumJaccard: 0}})
	cancel, done := workers(s)
	defer func() { cancel(); <-done }()
	run = completed(t, s, run.ID)
	findings, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Kind: "findings", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	evidence, _, err := s.Analysis.Evidence(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]model.AnalysisEvidence{}
	relationSources := false
	for _, v := range evidence {
		byID[v.ID] = v
		if v.SourceKind == "dependency_snapshot" && strings.Contains(string(v.Summary), `"id":"da"`) && strings.Contains(string(v.Summary), `"id":"db"`) {
			relationSources = true
		}
	}
	found := false
	for _, v := range findings {
		var f continuity.Finding
		_ = json.Unmarshal(v.Body, &f)
		if f.Kind != "COMMON_MISSING_REPORT" {
			continue
		}
		found = true
		if len(f.EvidenceIDs) == 0 {
			t.Fatal("common gaps omitted interval evidence")
		}
		for _, id := range f.EvidenceIDs {
			if byID[id].ID == "" || byID[id].PermissionCategory != "monitoring-evidence" {
				t.Fatal("common gap references missing/nonfixed evidence", id)
			}
		}
	}
	if !found || !relationSources {
		t.Fatal("common gaps or actual dependency source metadata absent", found, relationSources)
	}
}
func TestMonitoringCurrentCollectorAllowsOnlyOwnedVisibleProfile(t *testing.T) {
	s, f, a, q, start := setup(t)
	f.fail = true
	q.Scope = "shared"
	q.DeviceIDs = []string{"a", "b"}
	revision, err := s.SaveProfile(context.Background(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	run := create(t, s, a, q.DeviceIDs, start, model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}})
	cancel, done := workers(s)
	defer func() { cancel(); <-done }()
	run = completed(t, s, run.ID)
	groups, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Kind: "dependency-groups", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range groups {
		var g continuity.DependencyGroup
		_ = json.Unmarshal(v.Body, &g)
		if g.Kind != "collector" {
			continue
		}
		found = true
		if g.HistoryQuality != "CURRENT_ONLY" || g.Start != g.End || !slices.Equal(g.MemberIDs, []string{"a", "b"}) {
			t.Fatal("collector snapshot invented historical membership", g)
		}
	}
	if !found {
		t.Fatal("actual profile CollectorID omitted")
	}
	// The child alone cannot reveal its inaccessible parent's access profile.
	child := create(t, s, a, []string{"b"}, start, model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}})
	child = completed(t, s, child.ID)
	groups, _, err = s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, child.ID, model.AnalysisFilter{Kind: "dependency-groups", Limit: 100})
	if err != nil || len(groups) != 0 {
		t.Fatal("inaccessible profile owner was exposed", len(groups), err)
	}
}
func TestMonitoringFrozenTracksObservationHypothesisReviewAndSourceExpiry(t *testing.T) {
	s, f, a, q, start := setup(t)
	seed(f, start)
	q.Scope = "shared"
	q.DeviceIDs = []string{"a", "b"}
	revision, err := s.SaveProfile(context.Background(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(model.MonitoringObservation{DeviceID: "a", Start: start + 5000, End: start + 6000, Type: "MAINTENANCE", Basis: "人工确认的受控检修", Reason: "隔离测试观察窗口"})
	observation, err := s.SaveObservation(context.Background(), a, model.MonitoringConfigRequest{ResourceID: "maintenance", Scope: "personal", DeviceIDs: []string{"a"}, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	params := model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}, ObservationRevisionIDs: []string{observation.ID}}
	bad, _ := json.Marshal(params)
	request := analytics.CreateRequest{DeviceIDs: []string{"a", "b"}, Start: start, End: start + 10000, Parameters: bad}
	if err := s.ValidateCreate(context.Background(), a, &request); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("unconfirmed exclusion accepted", err)
	}
	confirmed, err := s.ConfirmObservation(context.Background(), a, observation.ID, observation.Version)
	if err != nil {
		t.Fatal(err)
	}
	params.ObservationRevisionIDs = []string{confirmed.ID}
	run := create(t, s, a, []string{"a", "b"}, start, params)
	cancel, done := workers(s)
	defer func() { cancel(); <-done }()
	run = completed(t, s, run.ID)
	page, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Kind: "metrics", DeviceID: "a"})
	if err != nil || len(page) != 1 {
		t.Fatal(page, err)
	}
	var metrics struct {
		Metrics []continuity.Metric `json:"metrics"`
	}
	_ = json.Unmarshal(page[0].Body, &metrics)
	for _, m := range metrics.Metrics {
		if m.ProfileID == "" && m.AttributeID == "" {
			if m.PlannedMs != 9000 || m.ExcludedMs != 1000 {
				t.Fatal("confirmed observation changed wrong denominator", m)
			}
			if m.Track == "data" && (m.AvailableMs != 8960 || m.UnavailableMs != 40 || m.UnknownMs != 0) {
				t.Fatal("availability used receive clock or sample count", m)
			}
			if m.Track == "connection" && (m.AvailableMs != 7000 || m.UnavailableMs != 2000) {
				t.Fatal("seed connection intervals lost", m)
			}
		}
	}
	groups, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Kind: "dependency-groups"})
	if err != nil || len(groups) == 0 {
		t.Fatal(groups, err)
	}
	var group continuity.DependencyGroup
	for _, v := range groups {
		_ = json.Unmarshal(v.Body, &group)
		if group.Kind == "access-profile" {
			break
		}
	}
	hypothesis := model.MonitoringHypothesisRequest{GroupID: group.ID, At: start + 1000, ExpectedRunVersion: run.Version, IdempotencyKey: "hypothesis"}
	h, err := s.Hypothesis(context.Background(), a, run.ID, hypothesis)
	if err != nil || !slices.Equal(h.DeviceIDs, []string{"a", "b"}) {
		t.Fatal(h, err)
	}
	again, err := s.Hypothesis(context.Background(), a, run.ID, hypothesis)
	if err != nil || again.ID != h.ID {
		t.Fatal("hypothesis idempotency failed", again, err)
	}
	hypothesis.At = start + 2000
	if _, err = s.Hypothesis(context.Background(), a, run.ID, hypothesis); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("hypothesis idempotency mutated snapshot", err)
	}
	findings, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Kind: "findings"})
	if err != nil || len(findings) == 0 {
		t.Fatal(findings, err)
	}
	if _, err = s.Review(context.Background(), a, findings[0].ID, model.MonitoringReviewRequest{RunID: run.ID, ExpectedRunVersion: run.Version, Result: "OBSERVE", Explanation: "仅为受控样本，待现场资料", IdempotencyKey: "review"}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Export(context.Background(), a, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.values = nil
	f.states = nil
	f.deps = nil
	f.mu.Unlock()
	after, err := s.Export(context.Background(), a, run.ID)
	if err != nil || string(after) != string(before) {
		t.Fatal("fixed report was recalculated after source expiry", err)
	}
	partial := a
	partial.AllDevices = false
	partial.DeviceIDs = []string{"a"}
	s.Analysis.Resolve = func(context.Context, analytics.Actor) (analytics.Actor, error) { return partial, nil }
	if _, err = s.Export(context.Background(), a, run.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("partial scope read full snapshot", err)
	}
}

func TestMonitoringUnknownAvailabilityReceiptAndReadCapsRemainPartial(t *testing.T) {
	for _, test := range []string{"availability", "cap", "unavailable"} {
		t.Run(test, func(t *testing.T) {
			s, f, a, q, start := setup(t)
			seed(f, start)
			switch test {
			case "availability":
				f.values[5].AvailableAt = 0
				f.values[5].AvailableAtSource = "UNKNOWN"
			case "cap":
				s.RecordLimit = 5
			case "unavailable":
				f.fail = true
			}
			revision, err := s.SaveProfile(context.Background(), a, q)
			if err != nil {
				t.Fatal(err)
			}
			run := create(t, s, a, []string{"a"}, start, model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}})
			cancel, done := workers(s)
			defer func() { cancel(); <-done }()
			run = completed(t, s, run.ID)
			if run.Status != model.AnalysisPartial {
				t.Fatal("deficient source became complete", run.Status)
			}
			snapshot, err := s.Analysis.Snapshot(context.Background(), a, analytics.KindMonitoring, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			var summary struct {
				Tracks map[string]continuity.Metric `json:"tracks"`
			}
			_ = json.Unmarshal(snapshot.Statistics, &summary)
			data := summary.Tracks["data"]
			received := summary.Tracks["received"]
			if data.UnknownMs <= 0 || data.FullWindowAvailability != nil {
				t.Fatal("unknown was removed from window", data)
			}
			if test == "availability" && (received.UnknownMs != 0 || received.AvailableMs != 9980) {
				t.Fatal("unknown availability erased reception coverage", received)
			}
			if test == "cap" && !slices.Contains(snapshot.Limitations, "MEASUREMENT_READ_CAP_REACHED") {
				t.Fatal("read truncation hidden", snapshot)
			}
			if test == "unavailable" {
				groups, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindMonitoring, run.ID, model.AnalysisFilter{Kind: "dependency-groups"})
				if err != nil || len(groups) == 0 {
					t.Fatal("current-only assumption missing", groups, err)
				}
				var g continuity.DependencyGroup
				_ = json.Unmarshal(groups[0].Body, &g)
				if g.HistoryQuality != "CURRENT_ONLY" || g.Start != g.End {
					t.Fatal("current relation rewrote historical interval", g)
				}
				h, err := s.Hypothesis(context.Background(), a, run.ID, model.MonitoringHypothesisRequest{GroupID: g.ID, At: g.Start, ExpectedRunVersion: run.Version, IdempotencyKey: "current"})
				if err != nil || len(h.DeviceIDs) != 1 {
					t.Fatal(h, err)
				}
				if _, err := s.Hypothesis(context.Background(), a, run.ID, model.MonitoringHypothesisRequest{GroupID: g.ID, At: start, ExpectedRunVersion: run.Version, IdempotencyKey: "historic"}); !errors.Is(err, model.ErrAnalysisInvalid) {
					t.Fatal("current assumption accepted historical time", err)
				}
			}
		})
	}
}

type pausingStore struct {
	ports.AnalysisStore
	entered chan struct{}
	once    sync.Once
}

func (p *pausingStore) CommitAnalysisBatch(ctx context.Context, tenant, id string, token int64, b model.AnalysisBatch) (model.AnalysisRun, error) {
	r, err := p.AnalysisStore.CommitAnalysisBatch(ctx, tenant, id, token, b)
	if err != nil {
		return r, err
	}
	if strings.HasPrefix(b.ID, "device/0/0") {
		pause := false
		p.once.Do(func() { pause = true; close(p.entered) })
		if pause {
			<-ctx.Done()
			return r, ctx.Err()
		}
	}
	return r, nil
}
func TestMonitoringRestartUsesStagedDerivedResultWithoutRescan(t *testing.T) {
	s, f, a, q, start := setup(t)
	seed(f, start)
	revision, err := s.SaveProfile(context.Background(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	run := create(t, s, a, []string{"a"}, start, model.MonitoringRunParameters{ProfileRevisionIDs: []string{revision.ID}})
	store := &pausingStore{AnalysisStore: s.Analysis.Store, entered: make(chan struct{})}
	s.Analysis.Store = store
	cancel, done := workers(s)
	select {
	case <-store.entered:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("stage boundary not reached")
	}
	cancel()
	<-done
	f.mu.Lock()
	windowReads, memberReads := f.windowReads, f.memberReads
	f.values = nil
	f.states = nil
	f.deps = nil
	f.mu.Unlock()
	resume, finished := workers(s)
	defer func() { resume(); <-finished }()
	run = completed(t, s, run.ID)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.windowReads != windowReads || f.memberReads != memberReads {
		t.Fatal("resume replaced frozen members or recomputed persisted derived result", f.windowReads, f.memberReads)
	}
	page, _, err := s.Analysis.Store.ListAnalysisOutputs(context.Background(), "t", model.AnalysisFilter{RunID: run.ID, Kind: "metrics"})
	if err != nil || len(page) != 1 {
		t.Fatal(page, err)
	}
	var b struct {
		Metrics []continuity.Metric `json:"metrics"`
	}
	_ = json.Unmarshal(page[0].Body, &b)
	for _, m := range b.Metrics {
		if m.Track == "data" && m.AttributeID == "" && m.ProfileID == "" && m.AvailableMs != 9960 {
			t.Fatal("source expiry overwrote frozen result", m)
		}
	}
}

func TestMonitoringInheritedProfileScopeAndPolicyPairProtection(t *testing.T) {
	s, _, a, q, start := setup(t)
	q.Scope = "shared"
	q.DeviceIDs = []string{"a", "b"}
	var body model.MonitoringProfile
	_ = json.Unmarshal(q.Body, &body)
	body.TargetType = "PRODUCT"
	body.ProductID = "p"
	q.Body, _ = json.Marshal(body)
	v, err := s.SaveProfile(context.Background(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	partial := a
	partial.AllDevices = false
	partial.DeviceIDs = []string{"b"}
	s.Analysis.Resolve = func(_ context.Context, input analytics.Actor) (analytics.Actor, error) {
		partial.Username = input.Username
		return partial, nil
	}
	visible, err := s.GetConfig(context.Background(), a, "profiles", v.ID)
	if err != nil || !slices.Equal(visible.DeviceIDs, []string{"b"}) {
		t.Fatal("shared membership leaked", visible, err)
	}
	params, _ := json.Marshal(model.MonitoringRunParameters{ProfileRevisionIDs: []string{v.ID}, CommonGapPolicy: &model.MonitoringCommonGapPolicy{Version: "policy-v1", MinimumGapCount: 1, MinimumOverlapMs: 100, MinimumJaccard: .5}})
	request := analytics.CreateRequest{DeviceIDs: []string{"b"}, Start: start, End: start + 10000, Parameters: params}
	if err := s.ValidateCreate(context.Background(), a, &request); err != nil {
		t.Fatal("authorized child could not inherit shared profile", err)
	}
	partial.AllDevices = true
	request.DeviceIDs = make([]string, 143)
	for i := range request.DeviceIDs {
		request.DeviceIDs[i] = fmt.Sprint(i)
	}
	if err := s.ValidateCreate(context.Background(), a, &request); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("quadratic policy guard absent", err)
	}
}
