package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type maintenanceCatalog struct{ alarms map[string]model.Alarm }

type maintenanceReportAI struct{ job model.AnalysisAIRevision }

func (f maintenanceReportAI) List(_ context.Context, _ analytics.Actor, _, _ string, q model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error) {
	if q.Offset > 0 {
		return []model.AnalysisAIRevision{}, 1, nil
	}
	return []model.AnalysisAIRevision{f.job}, 1, nil
}

func (c maintenanceCatalog) GetManagedDevice(_ context.Context, tenant, id string) (model.ManagedDevice, error) {
	if tenant != "t" || !slices.Contains([]string{"d1", "hidden"}, id) {
		return model.ManagedDevice{}, model.ErrNotFound
	}
	return model.ManagedDevice{TenantID: tenant, ID: id, ProductID: "p", CreatedAt: 123}, nil
}
func (c maintenanceCatalog) GetAlarm(_ context.Context, tenant, id string) (model.Alarm, error) {
	v, ok := c.alarms[id]
	if !ok || v.TenantID != tenant {
		return v, model.ErrNotFound
	}
	return v, nil
}

type maintenanceFacts struct {
	mu     sync.Mutex
	states []model.DeviceStateIntervalFact
	events []model.BusinessEventFact
	config []model.ConfigurationFact
	reads  int
	fail   bool
}
type maintenanceReader struct {
	ports.AnalyticsFactReader
	states []model.DeviceStateIntervalFact
	events []model.BusinessEventFact
	config []model.ConfigurationFact
}

func (f *maintenanceFacts) AnalyticsFactsRead(ctx context.Context, tenant string, fn func(ports.AnalyticsFactReader) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.fail {
		return errors.New("original source unavailable")
	}
	return fn(maintenanceReader{states: slices.Clone(f.states), events: slices.Clone(f.events), config: slices.Clone(f.config)})
}
func maintenancePage[T any](q model.FactQuery, items []T, source string) model.FactPage[T] {
	offset := 0
	if q.Cursor != "" {
		fmt.Sscan(q.Cursor, &offset)
	}
	end := min(len(items), offset+q.Limit)
	p := model.FactPage[T]{Items: items[offset:end], FactPageMeta: model.FactPageMeta{Complete: true, HasMore: end < len(items), Source: model.FactSourceCoverage{Source: source, SourceVersion: "controlled-synthetic-v1", Status: "AVAILABLE", Complete: true, CoverageStart: q.Start, CoverageEnd: q.End, CollectionStartedAt: 1, ReadAt: 100000000, HistoricalReconstructionQuality: "CONTROLLED_SYNTHETIC", BackfillStatus: "FIXTURE"}}}
	if p.HasMore {
		p.Cursor = fmt.Sprint(end)
	}
	return p
}
func (r maintenanceReader) ListDeviceStateIntervals(q model.FactQuery) (model.FactPage[model.DeviceStateIntervalFact], error) {
	v := []model.DeviceStateIntervalFact{}
	for _, x := range r.states {
		if slices.Contains(q.DeviceIDs, x.DeviceID) && x.End > q.Start && x.Start < q.End {
			v = append(v, x)
		}
	}
	return maintenancePage(q, v, "state"), nil
}
func (r maintenanceReader) ListAlarmReportEvents(q model.FactQuery) (model.FactPage[model.BusinessEventFact], error) {
	v := []model.BusinessEventFact{}
	for _, x := range r.events {
		if slices.Contains(q.DeviceIDs, x.DeviceID) && x.OccurredAt >= q.Start && x.OccurredAt < q.End {
			v = append(v, x)
		}
	}
	return maintenancePage(q, v, "fault-ledger"), nil
}
func (r maintenanceReader) GetConfigurationHistory(q model.FactQuery) (model.FactPage[model.ConfigurationFact], error) {
	return maintenancePage(q, r.config, "configuration"), nil
}
func maintenanceFixture(t *testing.T) (*Service, analytics.Actor, *maintenanceFacts) {
	t.Helper()
	actor := analytics.Actor{TenantID: "t", Username: "admin", AllDevices: true, Permissions: []string{"*"}, AccessVersion: "v1"}
	base := analytics.NewService(analytics.NewMemoryStore(), config.AnalyticsConfig{Poll: time.Millisecond, Workers: 1, Lease: time.Second, BatchSize: 3}, func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }, func(_ context.Context, tenant, id string) error {
		if tenant != "t" || id != "d1" {
			return model.ErrNotFound
		}
		return nil
	})
	facts := &maintenanceFacts{}
	svc := NewService(base, facts)
	svc.Now = func() time.Time { return time.UnixMilli(100000000) }
	svc.Catalog = maintenanceCatalog{alarms: map[string]model.Alarm{}}
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	return svc, actor, facts
}
func request(id, key string, version int64, body any) model.MaintenanceRevisionRequest {
	return model.MaintenanceRevisionRequest{ResourceID: id, ExpectedVersion: version, DeviceIDs: []string{"d1"}, IdempotencyKey: key, Body: jsonBody(body)}
}
func human() []model.ResponseEvidenceReference {
	return []model.ResponseEvidenceReference{{Kind: "HUMAN_CONFIRMATION", DeviceID: "d1", Description: "隔离手算固定的实物边界依据"}}
}
func assetFixture(t *testing.T, s *Service, a analytics.Actor) model.AnalysisConfigRevision {
	t.Helper()
	v, err := s.SaveAsset(context.Background(), a, request("asset", "asset", 0, model.AssetInstance{Name: "实物", DeviceID: "d1", PhysicalID: "serial-a", EffectiveStart: 1000, BoundaryStatus: "CONFIRMED", Evidence: human(), Importance: 7}))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func interventionFixture(t *testing.T, s *Service, a analytics.Actor, asset string) model.AnalysisConfigRevision {
	t.Helper()
	ctx := context.Background()
	v, err := s.SaveIntervention(ctx, a, request("repair", "repair", 0, model.MaintenanceIntervention{Name: "实际维修", Type: "REPAIR", AssetRevisionID: asset, Reason: "故障检修", Actions: []string{"检查接线"}}))
	if err != nil {
		t.Fatal(err)
	}
	v, err = s.InterventionAction(ctx, a, "repair", model.MaintenanceActionRequest{ExpectedVersion: v.Version, IdempotencyKey: "start", Action: "START", At: 10*hourMs + 2000})
	if err != nil {
		t.Fatal(err)
	}
	v, err = s.InterventionAction(ctx, a, "repair", model.MaintenanceActionRequest{ExpectedVersion: v.Version, IdempotencyKey: "end", Action: "COMPLETE", At: 10*hourMs + 3000, Reason: "实际动作已结束"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func observationRequest(asset string, work model.AnalysisConfigRevision) model.MaintenanceObservationRequest {
	return model.MaintenanceObservationRequest{ExpectedVersion: work.Version, IdempotencyKey: "observe", Parameters: model.MaintenanceObservationParameters{BeforeAssetRevisionID: asset, AfterAssetRevisionID: asset, ComparisonType: "SAME_INSTANCE_REPAIR", Before: model.FactRange{Start: 1000, End: 10*hourMs + 1000}, After: model.FactRange{Start: 11*hourMs + 1000, End: 21*hourMs + 1000}}}
}
func awaitRun(t *testing.T, s *Service, a analytics.Actor, kind, id string) model.AnalysisRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Analysis.RunWorkers(ctx, "maintenance-test", slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	defer func() { cancel(); <-done }()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		run, err := s.Analysis.Get(context.Background(), a, kind, id)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			if run.Status == model.AnalysisFailed {
				t.Fatal(run.Error)
			}
			return run
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatal("worker timeout")
	return model.AnalysisRun{}
}
func TestMaintenanceAssetAtomicSlotCASIdentityAndIndependentVerification(t *testing.T) {
	s, a, _ := maintenanceFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make([]model.AnalysisConfigRevision, 2)
	errs := make([]error, 2)
	q := request("asset", "same-key", 0, model.AssetInstance{Name: "实物", DeviceID: "d1", PhysicalID: "physical-a", EffectiveStart: 1000, BoundaryStatus: "CONFIRMED", Evidence: human()})
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = s.SaveAsset(ctx, a, q) }(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || results[0].ID != results[1].ID {
		t.Fatalf("same receipt diverged %v %+v", errs, results)
	}
	var asset model.AssetInstance
	decode(results[0].Body, &asset)
	if asset.CommissionedAt != nil {
		t.Fatal("registration date became physical age")
	}
	asset.PhysicalID = "changed"
	asset.Requests = nil
	if _, err := s.SaveAsset(ctx, a, request("asset", "new", 1, asset)); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("same platform asset silently replaced", err)
	}
	asset.PhysicalID = "physical-b"
	if _, err := s.SaveAsset(ctx, a, request("other", "overlap", 0, asset)); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("overlapping physical instance accepted", err)
	}
	asset.ComponentID = "free-text"
	if _, err := s.SaveAsset(ctx, a, request("component", "free", 0, asset)); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("component identity fabricated", err)
	}
	work := interventionFixture(t, s, a, results[0].ID)
	var b model.MaintenanceIntervention
	decode(work.Body, &b)
	if b.Status != "COMPLETED" || len(b.Verifications) != 0 {
		t.Fatal("work completion implied verification")
	}
	// A real service action may register earlier actual work. These two clocks
	// must stay separate in the immutable revision and its history.
	if b.StartedAt != 10*hourMs+2000 || b.EndedAt != 10*hourMs+3000 || len(b.History) != 2 || b.History[0].OccurredAt != b.StartedAt || b.History[1].OccurredAt != b.EndedAt || b.History[0].RecordedAt != s.now() || b.History[1].RecordedAt != s.now() || b.History[0].RecordedAt <= b.StartedAt || b.History[1].RecordedAt <= b.EndedAt {
		t.Fatal("backdated actual work was mistaken for registration time", b)
	}
	verification := model.MaintenanceVerificationRequest{ExpectedVersion: work.Version, IdempotencyKey: "verify", Verification: model.MaintenanceVerification{RequiredItems: []string{"通电", "功能上报"}, CheckedItems: []string{"通电"}, Result: "PASSED", Explanation: "实际检查", Evidence: human()}}
	if _, err := s.VerifyIntervention(ctx, a, "repair", verification); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("partial checklist passed", err)
	}
	verification.Verification.Result = "FAILED"
	work, err := s.VerifyIntervention(ctx, a, "repair", verification)
	if err != nil {
		t.Fatal(err)
	}
	decode(work.Body, &b)
	if b.Status != "COMPLETED" || b.Verifications[0].Result != "FAILED" {
		t.Fatal("failed verification rewrote work")
	}
	retried, err := s.SaveIntervention(ctx, a, request("repair", "repair", 0, model.MaintenanceIntervention{Name: "实际维修", Type: "REPAIR", AssetRevisionID: results[0].ID, Reason: "故障检修", Actions: []string{"检查接线"}}))
	if err != nil || retried.Version != 1 {
		t.Fatal("late original request lost receipt", err)
	}
}
func TestMaintenanceFixedEventClockDedupUnknownDenominatorsAndFrozenRestart(t *testing.T) {
	s, a, f := maintenanceFixture(t)
	ctx := context.Background()
	asset := assetFixture(t, s, a)
	work := interventionFixture(t, s, a, asset.ID)
	q := observationRequest(asset.ID, work)
	// 10h: 9h unknown and 1h offline; device offline remains in denominator.
	f.states = []model.DeviceStateIntervalFact{stateRange(9*hourMs+1000, 10*hourMs+1000, "OFFLINE"), stateRange(11*hourMs+1000, 21*hourMs+1000, "ONLINE")}
	alarm := model.Alarm{ID: "cycle", TenantID: "t", DeviceID: "d1", TriggerID: "m1", AlarmType: "FAULT", Details: map[string]any{"message": model.StandardMessage{MessageID: "m1", DeviceID: "d1", Timestamp: hourMs + 1000}}}
	f.events = []model.BusinessEventFact{{SourceEventID: "create", DeviceID: "d1", ResourceID: "cycle", Type: "ALARM_CREATED", OccurredAt: 20 * hourMs, Body: jsonBody(alarm)}, {SourceEventID: "repeat", DeviceID: "d1", ResourceID: "cycle", Type: "ALARM_REPORTED", OccurredAt: 20*hourMs + 1, Body: jsonBody(alarm)}}
	s.Catalog = maintenanceCatalog{alarms: map[string]model.Alarm{"cycle": alarm}}
	assessment, err := s.SaveFaultAssessment(ctx, a, request("cycle-confirm", "assessment", 0, model.MaintenanceFaultAssessment{AlarmID: "cycle", DeviceID: "d1", Type: "FAULT", Classification: "PRODUCTION", Basis: "已核实的同类故障"}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ConfirmFault(ctx, a, "cycle-confirm", model.MaintenanceActionRequest{ExpectedVersion: assessment.Version, IdempotencyKey: "confirm"})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := s.SaveAdmission(ctx, a, request("admission", "admission", 0, model.MaintenanceAdmissionRecord{FaultType: "FAULT", Parameters: model.MaintenanceAdmission{Version: "v1", MinimumEffectiveHours: 1, MinimumObservationCoverage: .8, MinimumFaultSourceCoverage: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	q.Parameters.AdmissionRevisionID = admission.ID
	run, err := s.CreateObservation(ctx, a, "repair", q)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, s, a, analytics.KindMaintenance, run.ID)
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindMaintenance, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var observed model.MaintenanceObservation
	json.Unmarshal(snapshot.Statistics, &observed)
	if run.Status != model.AnalysisPartial || observed.Before.ConfirmedFaultCycles != 1 || observed.After.ConfirmedFaultCycles != 0 || observed.Before.EffectiveMs != 10*hourMs || observed.Before.StateUnknownMs != 9*hourMs || *observed.Before.KnownOfflineRatio != 1 || *observed.Before.StateCoverage != .1 || observed.Before.FullWindowOfflineRatio != nil {
		t.Fatalf("processing clock/repetition/unknown denominator corruption %+v %+v", run, observed)
	}
	frozen, err := s.loadManifest(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	reads := f.reads
	f.fail = true
	f.events = nil
	f.states = nil
	f.mu.Unlock()
	restored, err := s.loadManifest(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	oldHash, _ := analytics.AnalysisHash(frozen)
	newHash, _ := analytics.AnalysisHash(restored)
	if oldHash != newHash {
		t.Fatal("restart changed input")
	}
	_, _, err = observe(restored)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.reads != reads {
		t.Fatal("restart rescanned original windows")
	}
	f.mu.Unlock()
	s.AI = maintenanceReportAI{job: model.AnalysisAIRevision{ID: "fixed-ai", LeaseOwner: "internal-worker", LeaseToken: 42, LeaseExpiresAt: 100, CreatorSessionVersion: 7, PermissionVersion: "internal-access-version"}}
	report, err := s.Export(ctx, a, analytics.KindMaintenance, run.ID)
	if err != nil || !json.Valid(report) {
		t.Fatal(err)
	}
	var exported struct {
		Run model.AnalysisRun          `json:"run"`
		AI  []model.AnalysisAIRevision `json:"aiRevisions"`
	}
	if err = json.Unmarshal(report, &exported); err != nil || exported.Run.LeaseOwner != "" || exported.Run.LeaseToken != 0 || exported.Run.LeaseExpiresAt != 0 || exported.Run.CreatorSessionVersion != 0 || exported.Run.PermissionsVersion != "" || len(exported.Run.Checkpoint) != 0 || len(exported.AI) != 1 || exported.AI[0].LeaseOwner != "" || exported.AI[0].LeaseToken != 0 || exported.AI[0].LeaseExpiresAt != 0 || exported.AI[0].CreatorSessionVersion != 0 || exported.AI[0].PermissionVersion != "" {
		t.Fatal("report exposed internal lease/session details", exported, err)
	}
}

func TestMaintenanceRecurrenceUsesActualAfterPhysicalWindowAndExcludedCycles(t *testing.T) {
	end, commissioned, retired := int64(850), int64(450), int64(800)
	after := SideInput{Asset: model.AssetInstance{DeviceID: "d1", ComponentID: "c1", PhysicalID: "serial", BoundaryStatus: "CONFIRMED", EffectiveStart: 400, EffectiveEnd: &end, CommissionedAt: &commissioned, RetiredAt: &retired}, Window: model.FactRange{Start: 500, End: 900}, Exclusions: []model.MaintenanceExclusion{{Kind: "CONFIRMED_TEST", Start: 550, End: 600, SourceRevisionID: "confirmed-test"}}}
	for i, at := range []int64{0, 399, 449, 499, 550, 800, 850, 900, 920, 700, 600} {
		after.Faults = append(after.Faults, FaultCycle{ID: fmt.Sprint(i), DeviceID: "d1", ComponentID: "c1", EventAt: at, Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"})
	}
	after.Faults = append(after.Faults, FaultCycle{ID: "hidden-component", DeviceID: "d1", ComponentID: "c2", EventAt: 510, Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"}, FaultCycle{ID: "conflicting-cycle", DeviceID: "d1", ComponentID: "c1", EventAt: 510, Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"}, FaultCycle{ID: "conflicting-cycle", DeviceID: "d1", ComponentID: "c1", EventAt: 520, Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"})
	if got := firstRecurrence(after, 350); got != 600 {
		t.Fatalf("recurrence attributed outside actual asset/observation bounds: %d", got)
	}
	after.Window.End = 590 // actual data cutoff, before the first admissible event
	if got := firstRecurrence(after, 350); got != 0 {
		t.Fatalf("future event beyond fixed data cutoff refreshed recurrence: %d", got)
	}
}
func TestMaintenanceInvestmentFinanceAndQualityPermissionInheritance(t *testing.T) {
	s, a, _ := maintenanceFixture(t)
	ctx := context.Background()
	asset := assetFixture(t, s, a)
	amount := "2.125"
	quote, err := s.SaveCost(ctx, a, request("quote", "quote", 0, model.MaintenanceCost{SourceKind: AssetKind, SourceID: asset.ResourceID, Type: "ESTIMATE", Currency: "CNY", Material: &amount, PlanningStart: 1000, PlanningEnd: 2000, OccurredAt: 1000, Basis: "固定报价；其余费用未知"}))
	if err != nil {
		t.Fatal(err)
	}
	scenario := model.InvestmentScenario{UseFinance: true, Name: "投入", Currency: "CNY", Budget: &amount, PlanningStart: 1000, PlanningEnd: 2000, PolicyVersion: DefaultInvestmentPolicy, Candidates: []model.InvestmentCandidate{{ID: "inspect", AssetRevisionID: asset.ID, Action: "INSPECT", RequiredTier: 1, TierBasis: "人工必需项", QuoteRevisionID: quote.ID}}}
	saved, err := s.SaveScenario(ctx, a, request("scenario", "scenario", 0, scenario))
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.EvaluateScenario(ctx, a, "scenario", model.InvestmentEvaluationRequest{ExpectedVersion: saved.Version, IdempotencyKey: "evaluate", UseFinance: true})
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, s, a, analytics.KindInvestment, run.ID)
	if !slices.Equal(run.RequiredPermissions, []string{FinancePermission}) {
		t.Fatal("financial facts lost required permission", run)
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindInvestment, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var evaluation InvestmentEvaluation
	json.Unmarshal(snapshot.Statistics, &evaluation)
	if evaluation.Items[0].Quote != nil || evaluation.Items[0].BudgetStatus != "QUOTE_MISSING_OR_INCOMPARABLE" {
		t.Fatal("unknown money became zero", evaluation)
	}
	current := a
	current.Permissions = []string{"menu:devices", "menu:maintenance", "POST /api/v1/investment-scenarios", "POST /api/v1/investment-scenarios/:id/evaluate"}
	s.Analysis.Resolve = func(context.Context, analytics.Actor) (analytics.Actor, error) { return current, nil }
	if _, err = s.Revision(ctx, a, ScenarioKind, saved.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("financial scenario body readable after revocation", err)
	}
	if _, err = s.Analysis.Get(ctx, a, analytics.KindInvestment, run.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("financial derived run readable after revocation", err)
	}
	if _, err = s.Export(ctx, a, analytics.KindInvestment, run.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("financial export readable after revocation", err)
	}
	list, total, err := s.List(ctx, a, ScenarioKind, model.AnalysisFilter{})
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatal("financial count leaked", list, total, err)
	}
	scenario.UseFinance = false
	scenario.Budget = nil
	scenario.Candidates[0].QuoteRevisionID = ""
	scenario.Name = "普通资料"
	ordinary, err := s.SaveScenario(ctx, a, request("ordinary", "ordinary", 0, scenario))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EvaluateScenario(ctx, a, ordinary.ResourceID, model.InvestmentEvaluationRequest{ExpectedVersion: ordinary.Version, IdempotencyKey: "ordinary-eval"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceWorkerTakeoverKeepsFrozenMembersAndRecordCapIsPartial(t *testing.T) {
	s, a, f := maintenanceFixture(t)
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	s.Analysis.Store = analytics.NewMemoryStoreWithClock(func() time.Time { return time.UnixMilli(clock.Load()) })
	asset := assetFixture(t, s, a)
	work := interventionFixture(t, s, a, asset.ID)
	f.states = []model.DeviceStateIntervalFact{stateRange(1000, 21*hourMs+1000, "ONLINE")}
	ready := make(chan struct{}, 1)
	if err := s.Analysis.Register(analytics.KindMaintenance, func(ctx context.Context, e *analytics.Execution) error {
		var p model.MaintenanceRunParameters
		decode(e.Run.Parameters, &p)
		b, err := s.observationInputs(ctx, runActor(e.Run), p, e.Run.DeviceIDs)
		if err != nil {
			return err
		}
		m, err := s.freezeObservation(ctx, e, b)
		if err != nil {
			return err
		}
		if err = s.saveManifest(ctx, e, m); err != nil {
			return err
		}
		ready <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateObservation(ctx, a, "repair", observationRequest(asset.ID, work))
	if err != nil {
		t.Fatal(err)
	}
	first, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.Analysis.RunWorkers(first, "first", slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("freeze not reached")
	}
	cancel()
	<-done
	f.mu.Lock()
	reads := f.reads
	f.fail = true
	f.states = nil
	f.events = nil
	f.mu.Unlock()
	clock.Add(2000)
	if err = s.Analysis.Register(analytics.KindMaintenance, s.ProcessObservation); err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, s, a, analytics.KindMaintenance, run.ID)
	if run.Status != model.AnalysisSucceeded || !run.InputsFrozen {
		t.Fatal("takeover did not complete fixed facts", run)
	}
	f.mu.Lock()
	if f.reads != reads {
		t.Fatal("takeover rescanned unavailable originals")
	}
	f.mu.Unlock()
	// Same logical range but a new run has a hard record cap and must explicitly
	// preserve partial coverage, even though the first returned interval is known.
	f.mu.Lock()
	f.fail = false
	f.states = []model.DeviceStateIntervalFact{stateRange(1000, 2000, "ONLINE"), stateRange(2000, 21*hourMs+1000, "OFFLINE")}
	f.mu.Unlock()
	s.RecordLimit = 1
	q := observationRequest(asset.ID, work)
	q.IdempotencyKey = "cap"
	run, err = s.CreateObservation(ctx, a, "repair", q)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, s, a, analytics.KindMaintenance, run.ID)
	if run.Status != model.AnalysisPartial {
		t.Fatal("cap silently complete", run)
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindMaintenance, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(snapshot.Limitations, "STATE_RECORD_CAP_REACHED") {
		t.Fatal("cap reason absent", snapshot.Limitations)
	}
}
func TestMaintenanceExpiredBudgetBeforeFreezePreservesPartialUnknown(t *testing.T) {
	s, a, _ := maintenanceFixture(t)
	asset := assetFixture(t, s, a)
	work := interventionFixture(t, s, a, asset.ID)
	s.Analysis.Limits.RunTimeout = time.Nanosecond
	run, err := s.CreateObservation(context.Background(), a, "repair", observationRequest(asset.ID, work))
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, s, a, analytics.KindMaintenance, run.ID)
	if run.Status != model.AnalysisPartial {
		t.Fatal("timeout lost partial result", run)
	}
	snapshot, err := s.Analysis.Snapshot(context.Background(), a, analytics.KindMaintenance, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(snapshot.Limitations, "RUN_TIME_BUDGET_EXHAUSTED") || len(snapshot.UncomputableMetrics) == 0 || snapshot.Sources[0].Complete {
		t.Fatal("unread source claimed complete", snapshot)
	}
}
func fixedSourceSnapshot(t *testing.T, s *Service, a analytics.Actor, id, kind string, devices []string, required []string, statistics any) model.AnalysisRun {
	t.Helper()
	ctx := context.Background()
	r, err := s.Analysis.Store.CreateAnalysisRun(ctx, model.AnalysisRun{ID: id, TenantID: a.TenantID, Creator: a.Username, DeviceIDs: devices, Kind: kind, Start: 1000, End: 21*hourMs + 1000, ConfigurationVersion: "controlled-config", AlgorithmVersion: "controlled-source", Parameters: jsonBody(map[string]any{}), IdempotencyKey: id, RequestHash: id, PermissionsVersion: a.AccessVersion, RequiredPermissions: required}, 100)
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Analysis.Store.ClaimAnalysisRun(ctx, "source-fixture", time.Minute, []string{kind})
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.Analysis.Store.CommitAnalysisBatch(ctx, a.TenantID, r.ID, r.LeaseToken, model.AnalysisBatch{ID: "fixed", Status: model.AnalysisSucceeded, Snapshot: &model.AnalysisSnapshot{ID: id + ":snapshot", DeviceIDs: devices, DataCutoff: 100000000, InputHashes: []string{id}, Statistics: jsonBody(statistics)}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestMaintenanceQualityWholeSourceScopeAndInvestmentDifferentBases(t *testing.T) {
	s, a, f := maintenanceFixture(t)
	ctx := context.Background()
	asset := assetFixture(t, s, a)
	work := interventionFixture(t, s, a, asset.ID)
	f.states = []model.DeviceStateIntervalFact{stateRange(1000, 21*hourMs+1000, "ONLINE")}
	quality := fixedSourceSnapshot(t, s, a, "quality", analytics.KindDataQuality, []string{"d1"}, nil, map[string]any{"source": "CONTROLLED_SYNTHETIC", "unknownSamples": 2})
	hidden := fixedSourceSnapshot(t, s, a, "quality-hidden", analytics.KindDataQuality, []string{"d1", "hidden"}, nil, map[string]any{"hidden": true})
	q := observationRequest(asset.ID, work)
	q.Parameters.QualityRunIDs = []string{hidden.ID}
	if _, err := s.CreateObservation(ctx, a, "repair", q); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("quality full source clipped to smaller task", err)
	}
	q.Parameters.QualityRunIDs = []string{quality.ID}
	q.UseFinance = true
	run, err := s.CreateObservation(ctx, a, "repair", q)
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRun(t, s, a, analytics.KindMaintenance, run.ID)
	if !slices.Equal(run.RequiredPermissions, []string{FinancePermission, analytics.QualityReadPermission}) {
		t.Fatal("quality/money inputs lost inherited permission", run)
	}
	current := a
	current.Permissions = []string{"menu:devices", "menu:maintenance", FinancePermission}
	s.Analysis.Resolve = func(context.Context, analytics.Actor) (analytics.Actor, error) { return current, nil }
	if _, err = s.Analysis.Get(ctx, a, analytics.KindMaintenance, run.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("quality derivative readable after revoke", err)
	}
	current = a
	// Two valid fixed observations use different fault/context/admission bases.
	// They cannot be numerically ranked against each other within one tier.
	observations := []model.AnalysisRun{}
	for i, basis := range []string{"same-context-fault-code-A", "same-context-fault-code-B"} {
		rate := float64(i + 1)
		obs := model.MaintenanceObservation{Status: "READY", Parameters: model.MaintenanceObservationParameters{AfterAssetRevisionID: asset.ID}, After: model.MaintenanceSideMetrics{FaultsPer1000Hours: &rate, OfflineMs: int64(i + 1)}}
		var body map[string]any
		json.Unmarshal(jsonBody(obs), &body)
		body["supplement"] = map[string]any{"contextComparable": true, "comparisonBasis": basis}
		observations = append(observations, fixedSourceSnapshot(t, s, a, fmt.Sprintf("observation-%d", i), analytics.KindMaintenance, []string{"d1"}, []string{analytics.QualityReadPermission}, body))
	}
	scenario := model.InvestmentScenario{Name: "不可混合口径", Currency: "CNY", PlanningStart: 1000, PlanningEnd: 2000, PolicyVersion: DefaultInvestmentPolicy, Candidates: []model.InvestmentCandidate{{ID: "repair", AssetRevisionID: asset.ID, ObservationRunID: observations[0].ID, Action: "REPAIR", RequiredTier: 1, TierBasis: "单位确认必需"}, {ID: "replace", AssetRevisionID: asset.ID, ObservationRunID: observations[1].ID, Action: "REPLACE", RequiredTier: 1, TierBasis: "单位确认必需"}}}
	config, err := s.SaveScenario(ctx, a, request("mixed", "mixed", 0, scenario))
	if err != nil {
		t.Fatal(err)
	}
	var saved model.InvestmentScenario
	decode(config.Body, &saved)
	if !slices.Equal(saved.RequiredPermissions, []string{analytics.QualityReadPermission}) || saved.Candidates[0].Comparable || saved.Candidates[1].Comparable || !slices.Contains(saved.Candidates[0].Constraints, "CROSS_CANDIDATE_METRIC_BASIS_DIFFERS") {
		t.Fatal("different bases silently ranked", saved)
	}
	rank, err := RankInvestment(saved, nil, false)
	if err != nil || rank.Items[0].Rank != 0 || rank.Items[0].Status != "AWAITING_INFORMATION" {
		t.Fatal(rank, err)
	}
	current.Permissions = []string{"menu:devices", "menu:maintenance"}
	if _, err = s.Revision(ctx, a, ScenarioKind, config.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("scenario quality derivative lost permission", err)
	}
}
func TestMaintenanceReplacementKeepsBothPhysicalInstancesAndDisputedBoundaryRejected(t *testing.T) {
	s, a, _ := maintenanceFixture(t)
	ctx := context.Background()
	old := assetFixture(t, s, a)
	work := interventionFixture(t, s, a, old.ID)
	switchAt := 10*hourMs + 3000
	var b model.AssetInstance
	decode(old.Body, &b)
	b.Requests = nil
	b.EffectiveEnd = &switchAt
	closed, err := s.SaveAsset(ctx, a, request("asset", "close-old", old.Version, b))
	if err != nil {
		t.Fatal(err)
	}
	newAsset := model.AssetInstance{Name: "替换新实物", DeviceID: "d1", PhysicalID: "serial-new", EffectiveStart: switchAt, BoundaryStatus: "CONFIRMED", Evidence: human()}
	newRef, err := s.SaveAsset(ctx, a, request("new-asset", "new-asset", 0, newAsset))
	if err != nil {
		t.Fatal(err)
	}
	q := observationRequest(closed.ID, work)
	q.Parameters.ComparisonType = "CROSS_INSTANCE_REPLACEMENT"
	q.Parameters.AfterAssetRevisionID = newRef.ID
	q.Parameters.SwitchAt = switchAt
	if _, err = s.CreateObservation(ctx, a, "repair", q); err != nil {
		t.Fatal("explicit same-device different-physical comparison rejected", err)
	}
	q.IdempotencyKey = "wrong-same"
	q.Parameters.ComparisonType = "SAME_INSTANCE_REPAIR"
	if _, err = s.CreateObservation(ctx, a, "repair", q); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("same platform ID erased physical boundary", err)
	}
	newAsset.BoundaryStatus = "DISPUTED"
	newAsset.Requests = nil
	disputed, err := s.SaveAsset(ctx, a, request("new-asset", "dispute", newRef.Version, newAsset))
	if err != nil {
		t.Fatal(err)
	}
	q.IdempotencyKey = "disputed"
	q.Parameters.ComparisonType = "CROSS_INSTANCE_REPLACEMENT"
	q.Parameters.AfterAssetRevisionID = disputed.ID
	if _, err = s.CreateObservation(ctx, a, "repair", q); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("disputed boundary became valid replacement comparison", err)
	}
}
