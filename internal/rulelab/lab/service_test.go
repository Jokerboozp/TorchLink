package lab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/rulelab/eval"
)

type inputFixture struct {
	mu          sync.Mutex
	items       []model.RuleLabInput
	reads       int
	unavailable bool
}
type fixtureReader struct {
	f     *inputFixture
	items []model.RuleLabInput
}

func (f *inputFixture) RuleLabInputsRead(ctx context.Context, tenant string, fn func(ports.RuleLabInputReader) error) error {
	f.mu.Lock()
	f.reads++
	items := slices.Clone(f.items)
	fail := f.unavailable
	f.mu.Unlock()
	if fail {
		return errors.New("source removed")
	}
	return fn(&fixtureReader{f, items})
}
func (r *fixtureReader) ListStandardInputs(q model.RuleLabInputQuery) (model.FactPage[model.RuleLabInput], error) {
	offset := 0
	if q.Cursor != "" {
		fmt.Sscan(q.Cursor, &offset)
	}
	values := []model.RuleLabInput{}
	for _, v := range r.items {
		at := v.Message.Timestamp
		if q.TimeBasis == "RECEIVED" {
			at = v.ReceivedAt
		}
		if at >= q.Start && at < q.End && slices.Contains(q.DeviceIDs, v.Message.DeviceID) {
			values = append(values, v)
		}
	}
	end := min(len(values), offset+q.Limit)
	page := model.FactPage[model.RuleLabInput]{Items: values[offset:end], FactPageMeta: model.FactPageMeta{Complete: true, HasMore: end < len(values), Source: model.FactSourceCoverage{Source: "controlled-fixture", ReadAt: 200000, CoverageStart: q.Start, CoverageEnd: q.End, CollectionStartedAt: 1, Complete: true, Status: "AVAILABLE", BackfillStatus: "FIXTURE", HistoricalReconstructionQuality: "CONTROLLED_SYNTHETIC", SourceVersion: "fixture-v1"}}}
	if page.HasMore {
		page.Cursor = fmt.Sprint(end)
	}
	return page, nil
}
func setupLab(t *testing.T) (*Service, *inputFixture, *memory.Repository, analytics.Actor, model.AlarmRuleRevision) {
	t.Helper()
	repo := memory.NewRepository()
	ctx := context.Background()
	for _, id := range []string{"a", "hidden"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, AccessKey: "key-" + id, TenantID: "t", ProductID: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveProduct(ctx, model.Product{ID: "p", TenantID: "t", Name: "隔离验证产品"}); err != nil {
		t.Fatal(err)
	}
	actor := analytics.Actor{TenantID: "t", Username: "analyst", Managed: true, SessionVersion: 1, AccessVersion: "fixture-v1", DeviceIDs: []string{"a"}, Permissions: []string{"menu:devices", "menu:ruleLab", "POST /api/v1/rule-lab/datasets", "POST /api/v1/rule-lab/experiments", "POST /api/v1/rule-lab/experiments/:id/runs", "POST /api/v1/rule-lab/labels", "POST /api/v1/rule-lab/labels/:id/confirm", "POST /api/v1/rule-lab/findings/:id/reviews", "GET /api/v1/rule-lab/experiments/:id/report"}}
	a := analytics.NewService(analytics.NewMemoryStore(), config.AnalyticsConfig{Workers: 1, BatchSize: 2, Lease: 300 * time.Millisecond, Poll: time.Millisecond, RunTimeout: time.Minute}, func(_ context.Context, id analytics.Actor) (analytics.Actor, error) {
		if id.TenantID != actor.TenantID || id.Username != actor.Username {
			return analytics.Actor{}, analytics.ErrForbidden
		}
		return actor, nil
	}, func(ctx context.Context, tenant, id string) error {
		_, err := repo.GetManagedDevice(ctx, tenant, id)
		return err
	})
	f := &inputFixture{}
	s := NewService(a, f)
	s.Catalog = repo
	s.History = repo
	s.ValidateCandidate = func(_ context.Context, _ analytics.Actor, r model.AlarmRule) error {
		for _, act := range r.Actions {
			if act.CameraID == "hidden-camera" {
				return analytics.ErrForbidden
			}
		}
		return nil
	}
	if err := s.Register(); err != nil {
		t.Fatal(err)
	}
	rule := model.AlarmRule{ID: "r", TenantID: "t", ProductID: "p", Name: "温度边界", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Recovery: []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 80}}, DurationSeconds: 2, Actions: []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}}}
	revision, err := repo.PublishRule(ctx, model.RulePublishRequest{Rule: rule, ExpectedBaselineVersion: 0, Reason: "controlled fixture", Actor: "fixture", SemanticsVersion: eval.RevisionV2})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range []struct {
		at    int64
		value float64
	}{{99000, 70}, {100000, 90}, {101000, 90}, {102000, 90}, {103000, 70}, {106000, 90}, {108000, 90}} {
		id := fmt.Sprint("m", i)
		message := model.StandardMessage{MessageID: id, RawMessageID: "raw-" + id, TenantID: "t", ProductID: "p", DeviceID: "a", MessageType: model.PropertyReport, Timestamp: v.at, Properties: map[string]any{"temperature": v.value, "text": "2026-10-01T00:00:00Z"}}
		f.items = append(f.items, model.RuleLabInput{ID: id, Message: message, ReceivedAt: v.at + 20, AvailableAt: v.at + 40, AvailableAtSource: "fixture_ack", ProtocolVersion: "fixture-protocol", ConfigurationVersion: "fixture-model", Units: map[string]string{"temperature": "C"}, MetadataQuality: "CONTROLLED_SYNTHETIC"})
	}
	return s, f, repo, actor, revision
}
func startWorkers(s *Service) (context.CancelFunc, chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Analysis.RunWorkers(ctx, "lab-fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	return cancel, done
}
func await(t *testing.T, s *Service, id string) model.AnalysisRun {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		r, err := s.Analysis.Store.GetAnalysisRun(context.Background(), "t", id)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(r.Status) {
			if r.Status == model.AnalysisFailed {
				t.Fatal("lab failed", r.Error)
			}
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("run did not complete")
	return model.AnalysisRun{}
}
func dataset(t *testing.T, s *Service, a analytics.Actor, key string) model.RuleLabDataset {
	t.Helper()
	d, err := s.CreateDataset(context.Background(), a, model.RuleLabDatasetRequest{DeviceIDs: []string{"a"}, Start: 100000, End: 120000, WarmupStart: 90000, TimeBasis: "EVENT", ClockPolicy: "EVENT_AS_PROCESSING", InitialStatePolicy: "EMPTY_UNKNOWN", SemanticsVersion: eval.RevisionV2, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	await(t, s, d.RunID)
	d, err = s.Dataset(context.Background(), a, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func experiment(t *testing.T, s *Service, a analytics.Actor, d model.RuleLabDataset, revision model.AlarmRuleRevision, key string) model.AnalysisConfigRevision {
	t.Helper()
	candidate := revision.Rule
	candidate.Conditions[0].Value = 95
	body, _ := json.Marshal(model.RuleLabExperiment{DatasetID: d.ID, BaselineRevisionIDs: []string{revision.ID}, CandidateRuleID: revision.RuleID, Candidate: candidate, CandidateEnabled: true, Hypothesis: "高阈值不会达到该隔离样本的触发条件", EvaluationPolicy: model.RuleLabEvaluationPolicy{Version: "fixed-fixture-v1", ToleranceMs: 100, MatchTimeBasis: "EVENT", ExtraCyclePolicy: "COUNT_EACH_CYCLE", Split: "TUNING"}})
	v, err := s.SaveExperiment(context.Background(), a, model.RuleLabConfigRequest{ResourceID: key, DeviceIDs: []string{"a"}, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestRuleLabFrozenDatasetIndependentBranchesAndZeroProductionWrites(t *testing.T) {
	s, f, repo, a, revision := setupLab(t)
	cancel, done := startWorkers(s)
	defer func() { cancel(); <-done }()
	d := dataset(t, s, a, "dataset")
	if d.InputCount != 7 || d.InitialStateQuality != "UNKNOWN" || d.ReproductionQuality != "HISTORICAL_SIMULATION" {
		t.Fatal(d)
	}
	v := experiment(t, s, a, d, revision, "experiment")
	run, err := s.CreateExperimentRun(context.Background(), a, v.ID, model.RuleLabRunRequest{IdempotencyKey: "compare"})
	if err != nil {
		t.Fatal(err)
	}
	run = await(t, s, run.ID)
	outputs, _, err := s.Analysis.Outputs(context.Background(), a, analytics.KindRuleLab, run.ID, model.AnalysisFilter{Kind: "metrics", Limit: 100})
	if err != nil || len(outputs) != 2 {
		t.Fatal(outputs, err)
	}
	for _, out := range outputs {
		var body struct {
			Branch   string          `json:"branch"`
			Counters branchMetrics   `json:"counters"`
			Labels   labelEvaluation `json:"labelEvaluation"`
		}
		if err = json.Unmarshal(out.Body, &body); err != nil {
			t.Fatal(err)
		}
		want := 0
		if body.Branch == "BASELINE" {
			want = 2
		}
		if body.Counters.NewCycles != want || body.Counters.InputCount != 7 || body.Labels.Status != "NOT_EVALUABLE" || body.Labels.Precision != nil || body.Labels.Recall != nil {
			t.Fatalf("unexpected fixed metrics %+v", body)
		}
	}
	alarms, err := repo.ListAlarms(context.Background(), ports.AlarmFilter{TenantID: "t"})
	if err != nil || len(alarms) != 0 {
		t.Fatal("experiment wrote production alarms", alarms, err)
	}
	before, _ := s.Report(context.Background(), a, v.ID, run.ID)
	f.mu.Lock()
	reads := f.reads
	f.unavailable = true
	f.items = nil
	f.mu.Unlock()
	after, err := s.Report(context.Background(), a, v.ID, run.ID)
	if err != nil || string(before) != string(after) {
		t.Fatal("fixed report changed after source removal", err)
	}
	items, total, err := s.DatasetInputs(context.Background(), a, d.ID, model.AnalysisFilter{Limit: 2, Offset: 2})
	if err != nil || len(items) != 2 || total != 7 {
		t.Fatal(items, total, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads != reads {
		t.Fatal("fixed result rescanned source")
	}
}
func TestRuleLabResourceCapAndScopedRules(t *testing.T) {
	s, _, repo, a, revision := setupLab(t)
	s.RecordLimit = 3
	cancel, done := startWorkers(s)
	defer func() { cancel(); <-done }()
	d := dataset(t, s, a, "capped")
	if d.InputCount != 3 || d.Status != model.AnalysisPartial || !slices.Contains(d.Limitations, "DATASET_RECORD_OR_BYTE_CAP_REACHED") {
		t.Fatal(d)
	}
	s.RecordLimit = 100
	rules, total, err := s.RuleSources(context.Background(), a, []string{"a"}, model.AnalysisFilter{Limit: 100})
	if err != nil || total != 1 || len(rules) != 1 || rules[0].ID != revision.ID {
		t.Fatal(rules, total, err)
	}
	if _, _, err = s.RuleSources(context.Background(), a, []string{"hidden"}, model.AnalysisFilter{}); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("hidden device source allowed", err)
	}
	hidden := revision.Rule
	hidden.Actions = []model.RuleAction{{Type: "OPEN_CAMERA", CameraID: "hidden-camera"}}
	if _, err = repo.PublishRule(context.Background(), model.RulePublishRequest{Rule: hidden, ExpectedBaselineVersion: revision.Version, Reason: "private camera", SemanticsVersion: eval.RevisionV2}); err != nil {
		t.Fatal(err)
	}
	rules, total, err = s.RuleSources(context.Background(), a, []string{"a"}, model.AnalysisFilter{Limit: 100})
	if err != nil || total != 1 || rules[0].ID != revision.ID {
		t.Fatal("hidden camera version escaped", rules, total, err)
	}
}
func TestRuleLabAICandidatePreparationIsReadOnlyAndSharedPrivateRejected(t *testing.T) {
	s, _, _, a, revision := setupLab(t)
	cancel, done := startWorkers(s)
	defer func() { cancel(); <-done }()
	d := dataset(t, s, a, "ai-source")
	v := experiment(t, s, a, d, revision, "ai-experiment")
	candidate := revision.Rule
	prepared, expected, err := s.PrepareAICandidate(context.Background(), a, v.ID, candidate)
	if err != nil || expected != 1 || prepared.ID == v.ID {
		t.Fatal(prepared, expected, err)
	}
	var b model.RuleLabExperiment
	json.Unmarshal(prepared.Body, &b)
	if b.Candidate.Enabled || b.CandidateEnabled {
		t.Fatal("AI draft enabled")
	}
	all, total, err := s.ListConfigs(context.Background(), a, "experiments", model.AnalysisFilter{Limit: 100})
	if err != nil || len(all) != 1 || total != 1 {
		t.Fatal("prepare wrote a candidate", all, total, err)
	}
	q := model.RuleLabConfigRequest{ResourceID: "shared-private", DeviceIDs: []string{"a"}, Scope: "SHARED", Body: v.Body}
	if _, err = s.SaveExperiment(context.Background(), a, q); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("shared experiment accepted personal source", err)
	}
}
func TestRuleLabDeterministicMaximumMatchingAndCommonLabelDenominators(t *testing.T) {
	left := []matchPoint{{ID: "a", Device: "d", Type: "FIRE", At: 1}, {ID: "b", Device: "d", Type: "FIRE", At: 2}}
	right := []matchPoint{{ID: "x", Device: "d", Type: "FIRE", At: 2}, {ID: "y", Device: "d", Type: "FIRE", At: 0}}
	pairs, err := maximumMatch(left, right, 1)
	if err != nil || len(pairs) != 2 || pairs[0].Right != 1 || pairs[1].Right != 0 {
		t.Fatal("greedy lost maximum cardinality", pairs, err)
	}
	labels := []model.AnalysisConfigRevision{}
	for _, v := range []model.RuleLabLabel{{DeviceID: "d", EventType: "FIRE", Start: 100, End: 200, Conclusion: "CONFIRMED_EVENT", Status: "CONFIRMED"}, {DeviceID: "d", EventType: "FIRE", Start: 200, End: 300, Conclusion: "NO_ABNORMALITY_FOUND", Status: "CONFIRMED"}, {DeviceID: "d", EventType: "FIRE", Start: 400, End: 500, Conclusion: "UNKNOWN", Status: "CONFIRMED"}} {
		body, _ := json.Marshal(v)
		labels = append(labels, model.AnalysisConfigRevision{ID: fmt.Sprint(len(labels)), Body: body})
	}
	cycles := []model.RuleLabOutcome{{ID: "1", DeviceID: "d", AlarmType: "FIRE", TriggerEventAt: 110}, {ID: "2", DeviceID: "d", AlarmType: "FIRE", TriggerEventAt: 120}, {ID: "3", DeviceID: "d", AlarmType: "FIRE", TriggerEventAt: 210}, {ID: "4", DeviceID: "d", AlarmType: "FIRE", TriggerEventAt: 450}}
	policy := model.RuleLabEvaluationPolicy{RepresentativeConfirmed: true, MatchTimeBasis: "EVENT", ToleranceMs: 0, Split: "HOLDOUT"}
	fixed := frozenExperiment{Labels: labels}
	r := evaluateLabels(model.AnalysisRun{ID: "run"}, fixed, policy, cycles, true)
	if r.Status != "EVALUABLE" || r.TP != 1 || r.AlarmCycles != 3 || r.RealEvents != 1 || r.Precision == nil || *r.Precision != 1.0/3 || r.Recall == nil || *r.Recall != 1 {
		t.Fatal(r)
	}
	unknown := evaluateLabels(model.AnalysisRun{}, fixed, policy, cycles, false)
	if unknown.Status != "NOT_EVALUABLE" || unknown.Precision != nil || unknown.Recall != nil {
		t.Fatal(unknown)
	}
	fixed.HoldoutPreviousUses = 1
	reuse := evaluateLabels(model.AnalysisRun{}, fixed, policy, cycles, true)
	if reuse.IndependentValidation {
		t.Fatal("reused holdout called independent")
	}
}

type comparisonPauseStore struct {
	ports.AnalysisStore
	entered chan struct{}
	once    sync.Once
}

func (p *comparisonPauseStore) CommitAnalysisBatch(ctx context.Context, tenant, id string, token int64, b model.AnalysisBatch) (model.AnalysisRun, error) {
	r, err := p.AnalysisStore.CommitAnalysisBatch(ctx, tenant, id, token, b)
	if err != nil {
		return r, err
	}
	pause := false
	if strings.HasPrefix(b.ID, "comparison-output:") {
		p.once.Do(func() { pause = true; close(p.entered) })
	}
	if pause {
		<-ctx.Done()
		return r, ctx.Err()
	}
	return r, nil
}
func TestRuleLabRestartUsesFixedManifestAndDerivedDocuments(t *testing.T) {
	s, f, _, a, revision := setupLab(t)
	cancel, done := startWorkers(s)
	d := dataset(t, s, a, "restart-dataset")
	v := experiment(t, s, a, d, revision, "restart-experiment")
	cancel()
	<-done
	gate := &comparisonPauseStore{AnalysisStore: s.Analysis.Store, entered: make(chan struct{})}
	s.Analysis.Store = gate
	run, err := s.CreateExperimentRun(context.Background(), a, v.ID, model.RuleLabRunRequest{IdempotencyKey: "restart-run"})
	if err != nil {
		t.Fatal(err)
	}
	cancel, done = startWorkers(s)
	select {
	case <-gate.entered:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("output checkpoint not reached")
	}
	cancel()
	<-done
	f.mu.Lock()
	reads := f.reads
	f.items = nil
	f.unavailable = true
	f.mu.Unlock()
	s.History = nil
	resume, finished := startWorkers(s)
	defer func() { resume(); <-finished }()
	run = await(t, s, run.ID)
	outputs, total, err := s.Analysis.Outputs(context.Background(), a, analytics.KindRuleLab, run.ID, model.AnalysisFilter{Kind: "outcomes", Limit: 100})
	if err != nil || total != 2 || len(outputs) != 2 {
		t.Fatal("derived frozen results changed after source expiry", outputs, total, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reads != reads {
		t.Fatal("restart replaced frozen dataset")
	}
}

func TestRuleLabDatasetMatchesProductionCommittedTraceSerialization(t *testing.T) {
	s, f, repo, a, _ := setupLab(t)
	ctx := context.Background()
	input := f.items[1]
	f.items = []model.RuleLabInput{input}
	claim, err := repo.ClaimStandardMessage(ctx, input.Message, "production-fixture", time.Minute)
	if err != nil || !claim.ShouldProcess {
		t.Fatal(claim, err)
	}
	rules, err := repo.RuleEvaluationRules(ctx, "t", "a")
	if err != nil || len(rules) != 1 {
		t.Fatal(rules, err)
	}
	// This is the production encoding from Engine.evaluateRules, independent
	// of the analysis hash helper: typed field order must remain unchanged.
	body, err := json.Marshal(input.Message)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	messageHash := hex.EncodeToString(digest[:])
	canonicalHash, _ := analytics.AnalysisHash(input.Message)
	if canonicalHash == messageHash {
		t.Fatal("fixture does not expose typed versus canonical JSON mismatch")
	}
	binding := model.RuleTraceBinding{TenantID: "t", MessageID: input.ID, ClaimToken: claim.Token}
	if err = repo.BeginRuleEvaluationTrace(ctx, model.RuleEvaluationTrace{RuleTraceBinding: binding, DeviceID: "a", ProductID: "p", RawMessageID: input.Message.RawMessageID, MessageTimestamp: input.Message.Timestamp, MessageHash: messageHash, ClaimOwner: "production-fixture", Rules: rules, RuleSetHash: model.RuleSetHash(rules), SemanticsVersion: eval.RevisionV2}); err != nil {
		t.Fatal(err)
	}
	durationAt := int64(100)
	step, err := repo.CommitRuleEvaluationStep(ctx, binding, 0, func(state model.RuleEvaluationState) (model.RuleEvaluationStep, error) {
		d, e := eval.TransitionRevision(rules[0], input.Message, eval.RuleState{Pending: state.Pending, Alarm: state.Alarm, NewAlarmID: "production-fixture-alarm"}, state.RecoveryRevision, eval.StageTimes{DurationAtSeconds: &durationAt})
		return model.RuleEvaluationStep{RuleRevisionID: rules[0].ID, SemanticsVersion: d.SemanticsVersion, Covered: d.Covered, Matched: d.Matched, DurationSatisfied: d.DurationSatisfied, Pending: d.Pending, PendingMutation: d.PendingMutation, RuleAlarmHandled: d.RuleAlarmHandled, Alarm: d.Alarm, WriteAlarm: d.WriteAlarm, Created: d.Created, Event: d.Event, Actions: d.Actions, Times: d.Times}, e
	})
	if err != nil || step.Times.DurationAtSeconds == nil || step.PendingMutation != eval.SetPending {
		t.Fatal(step, err)
	}
	if err = repo.RecordRuleRoutingTrace(ctx, binding, model.RuleRoutingTrace{}); err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkStandardMessageProcessed(ctx, "t", input.ID, claim.Token); err != nil {
		t.Fatal(err)
	}
	traces, total, err := repo.ListRuleEvaluationTraces(ctx, "t", model.RuleTraceFilter{DeviceIDs: []string{"a"}, MessageIDs: []string{input.ID}})
	if err != nil || total != 1 || traces[0].Status != "COMPLETE" || traces[0].ReproductionQuality != "EXACT" || len(traces[0].Steps) != 1 || traces[0].MessageHash != messageHash {
		t.Fatal("production step/completion was not exact", traces, err)
	}
	cancel, done := startWorkers(s)
	defer func() { cancel(); <-done }()
	dataset, err := s.CreateDataset(ctx, a, model.RuleLabDatasetRequest{DeviceIDs: []string{"a"}, Start: 100000, End: 120000, WarmupStart: 90000, TimeBasis: "EVENT", ClockPolicy: "RECORDED_TRACE", InitialStatePolicy: "TRACE_INITIAL", SemanticsVersion: eval.RevisionV2, IdempotencyKey: "real-committed-trace"})
	if err != nil {
		t.Fatal(err)
	}
	run := await(t, s, dataset.RunID)
	dataset, err = s.Dataset(ctx, a, dataset.ID)
	if err != nil || dataset.ReproductionQuality != "EXACT_TRACE_AVAILABLE" || dataset.InitialStateQuality != "KNOWN_TRACE_INITIAL" || dataset.InputCount != 1 {
		t.Fatal("dataset rejected an exact committed production trace", dataset, err)
	}
	frozen, err := s.loadDatasetChunk(ctx, run)
	if err != nil || len(frozen.Inputs[0].Traces) != 1 || frozen.Inputs[0].Traces[0].MessageHash != messageHash {
		t.Fatal("frozen trace binding changed", frozen, err)
	}
}

func TestRuleLabRecordedDirectCommitUsesActualCASAndCandidateOwnState(t *testing.T) {
	at := int64(120000)
	msg := model.StandardMessage{TenantID: "t", DeviceID: "a", ProductID: "p", MessageID: "assertion", RawMessageID: "raw-assertion", MessageType: model.AlarmReport, Timestamp: 110000, Event: map[string]any{"alarmType": "FIRE", "alarmLevel": "HIGH"}}
	route, err := eval.Route(msg, false)
	if err != nil || route.DirectRaise == nil {
		t.Fatal(route, err)
	}
	direct := route.DirectRaise
	initial := model.Alarm{ID: "actual-alarm", TenantID: "t", DeviceID: "a", RuleID: direct.RuleID, AlarmType: direct.AlarmType, AlarmLevel: direct.Level, Status: "ACTIVE", FirstTriggeredAt: 100000, LastTriggeredAt: 101000, TriggerCount: 2}
	actualBefore := initial
	actualBefore.Status = "ACKED"
	actualBefore.TriggerCount = 8
	actualBefore.LastTriggeredAt = 119000
	report := eval.RuleAlarmCandidate(model.AlarmRule{ID: direct.RuleID, Name: "设备直接断言", AlarmType: direct.AlarmType, Level: direct.Level}, msg, "unused", at)
	actualAfter, _, _ := eval.UpsertAlarm(report, actualBefore)
	trace := model.RuleEvaluationTrace{ID: "actual-trace", Status: "COMPLETE", Routing: model.RuleRoutingTrace{Initial: model.RuleRoutingSnapshot{Direct: map[string]model.RuleRoutingAlarm{direct.RuleID: {Alarm: initial}}}}, RoutingSteps: []model.RuleRoutingStep{{Kind: "DIRECT_RAISE", RuleID: direct.RuleID, Before: model.RuleRoutingAlarm{Alarm: actualBefore}, After: model.RuleRoutingAlarm{Alarm: actualAfter}, Applied: true, Times: model.RuleStageTimes{RaiseAtMillis: &at}}}}
	input := model.RuleLabInput{ID: msg.MessageID, Message: msg, Hash: "frozen-member", Traces: []model.RuleEvaluationTrace{trace}}
	m := datasetChunk{Selection: model.RuleLabDatasetRequest{Start: 100000, End: 130000, TimeBasis: "EVENT", ClockPolicy: "RECORDED_TRACE", InitialStatePolicy: "TRACE_INITIAL", SemanticsVersion: eval.RevisionV2}, Inputs: []model.RuleLabInput{input}, InitialStateQuality: "KNOWN_TRACE_INITIAL"}
	experiment := model.RuleLabExperiment{BaselinePolicy: "RECORDED_ACTIVATIONS"}
	run := model.AnalysisRun{ID: "run", TenantID: "t", DeviceIDs: []string{"a"}, Start: 100000, End: 130000}
	baseline, candidate := newBranch("BASELINE"), newBranch("CANDIDATE")
	for _, branch := range []*branchState{baseline, candidate} {
		if err = branch.apply(run, m, frozenExperiment{}, experiment, workItem{InputIndex: 0, TraceIndex: 0}); err != nil {
			t.Fatal(err)
		}
	}
	if got := baseline.Alarms[alarmKey("a", direct.RuleID)]; got.Status != "ACKED" || got.TriggerCount != 9 {
		t.Fatal("baseline discarded observed manual/CAS state", got)
	}
	if got := candidate.Alarms[alarmKey("a", direct.RuleID)]; got.Status != "ACTIVE" || got.TriggerCount != 3 {
		t.Fatal("candidate inherited baseline state", got)
	}
	trace.RoutingSteps[0].Applied = false
	trace.RoutingSteps[0].After.Alarm = actualBefore
	m.Inputs[0].Traces[0] = trace
	failedCAS := newBranch("BASELINE")
	if err = failedCAS.apply(run, m, frozenExperiment{}, experiment, workItem{InputIndex: 0, TraceIndex: 0}); err != nil {
		t.Fatal(err)
	}
	if len(failedCAS.Outcomes) != 0 || !slices.Contains(failedCAS.Metrics.Limitations, "TRACE_ROUTING_COMMIT_DIFFERS_FROM_PURE_EVALUATION") {
		t.Fatal("failed production CAS became committed outcome", failedCAS)
	}
}
