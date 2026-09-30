package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/maintenance"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMaintenanceSQLAtomicPhysicalSlotPermanentReceiptsAndFixedLedger(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	const tenant = "maintenance-test"
	for _, id := range []string{"d1", "race-device"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: id, ProductID: "p", Name: "隔离受控设备", AccessKey: id + "-fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.pool.Exec(ctx, "UPDATE analytics_source_collection SET collection_started_at=0,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC'"); err != nil {
		t.Fatal(err)
	}
	otherPool, err := pgxpool.NewWithConfig(ctx, repo.pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(otherPool.Close)
	other := &Repository{pool: otherPool}
	actor := analytics.Actor{TenantID: tenant, Username: "analyst", Permissions: []string{"*"}, AllDevices: true, AccessVersion: "fixture-v1"}
	makeService := func(r *Repository, facts ports.AnalyticsFactStore) *maintenance.Service {
		base := analytics.NewService(r, config.AnalyticsConfig{Poll: time.Millisecond, Workers: 1, Lease: time.Second, BatchSize: 3}, func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }, func(ctx context.Context, tenant, id string) error {
			_, err := r.GetManagedDevice(ctx, tenant, id)
			return err
		})
		s := maintenance.NewService(base, facts)
		s.Catalog = r
		if err := s.Register(); err != nil {
			t.Fatal(err)
		}
		return s
	}
	s1, s2 := makeService(repo, repo), makeService(other, other)
	now := time.Now().UnixMilli()
	hour := int64(3600000)
	start := now - 22*hour
	request := func(id, key string, version int64, device string, body any) model.MaintenanceRevisionRequest {
		b, _ := json.Marshal(body)
		return model.MaintenanceRevisionRequest{ResourceID: id, ExpectedVersion: version, DeviceIDs: []string{device}, IdempotencyKey: key, Body: b}
	}
	evidence := []model.ResponseEvidenceReference{{Kind: "HUMAN_CONFIRMATION", DeviceID: "d1", Description: "受控SQL夹具实物依据，不是现场试点"}}
	assetBody := model.AssetInstance{Name: "实物甲", DeviceID: "d1", PhysicalID: "synthetic-serial", EffectiveStart: start, BoundaryStatus: "CONFIRMED", Evidence: evidence}
	q := request("asset", "same-asset", 0, "d1", assetBody)
	var wg sync.WaitGroup
	results := make([]model.AnalysisConfigRevision, 2)
	errs := make([]error, 2)
	for i, svc := range []*maintenance.Service{s1, s2} {
		wg.Add(1)
		go func(i int, svc *maintenance.Service) {
			defer wg.Done()
			results[i], errs[i] = svc.SaveAsset(ctx, actor, q)
		}(i, svc)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || results[0].ID != results[1].ID {
		t.Fatalf("same-key replicas diverged %v", errs)
	}
	// Different resources race for the same physical slot. Both cannot commit
	// overlapping confirmed instances even when both prechecks saw an empty slot.
	raceResults := make([]model.AnalysisConfigRevision, 2)
	raceErrs := make([]error, 2)
	for i, svc := range []*maintenance.Service{s1, s2} {
		wg.Add(1)
		go func(i int, svc *maintenance.Service) {
			defer wg.Done()
			b := assetBody
			b.DeviceID = "race-device"
			b.PhysicalID = []string{"serial-one", "serial-two"}[i]
			b.Evidence = []model.ResponseEvidenceReference{{Kind: "HUMAN_CONFIRMATION", DeviceID: "race-device", Description: "受控并发实物"}}
			raceResults[i], raceErrs[i] = svc.SaveAsset(ctx, actor, request([]string{"race-one", "race-two"}[i], "race", 0, "race-device", b))
		}(i, svc)
	}
	wg.Wait()
	successes := 0
	for _, err := range raceErrs {
		if err == nil {
			successes++
		} else if !errors.Is(err, model.ErrAnalysisConflict) && !errors.Is(err, model.ErrAnalysisInvalid) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("physical slot atomicity lost: %v", raceErrs)
	}
	asset := results[0]
	workBody := model.MaintenanceIntervention{Name: "检修", AssetRevisionID: asset.ID, Type: "REPAIR", Reason: "受控故障检查", Actions: []string{"重新接线"}}
	work, err := s1.SaveIntervention(ctx, actor, request("repair", "repair", 0, "d1", workBody))
	if err != nil {
		t.Fatal(err)
	}
	startRequest := model.MaintenanceActionRequest{ExpectedVersion: work.Version, IdempotencyKey: "start", Action: "START", At: start + 10*hour + 1000}
	work, err = s1.InterventionAction(ctx, actor, "repair", startRequest)
	if err != nil {
		t.Fatal(err)
	}
	originalStart := work.ID
	work, err = s2.InterventionAction(ctx, actor, "repair", model.MaintenanceActionRequest{ExpectedVersion: work.Version, IdempotencyKey: "complete", Action: "COMPLETE", At: start + 10*hour + 2000, Reason: "记录结束，不代表验收"})
	if err != nil {
		t.Fatal(err)
	}
	restarted := makeService(other, other)
	retried, err := restarted.InterventionAction(ctx, actor, "repair", startRequest)
	if err != nil || retried.ID != originalStart {
		t.Fatal("restart lost permanent receipt", err)
	}
	// Actual production transaction ledger. First creation event's immutable
	// StandardMessage clock is before repair, though processing occurs afterward.
	alarm := model.Alarm{ID: "cycle", TenantID: tenant, DeviceID: "d1", RuleID: "fixture", AlarmType: "FAULT", Status: "ACTIVE", Source: "device", TriggerID: "m1", TriggerCount: 1, FirstTriggeredAt: start + hour, LastTriggeredAt: start + hour, Details: map[string]any{"message": model.StandardMessage{TenantID: tenant, MessageID: "m1", DeviceID: "d1", Timestamp: start + hour}}}
	if _, _, err = repo.UpsertAlarm(ctx, alarm); err != nil {
		t.Fatal(err)
	}
	alarm.TriggerID = "m2"
	alarm.LastTriggeredAt = start + 12*hour
	alarm.Details["message"] = model.StandardMessage{TenantID: tenant, MessageID: "m2", DeviceID: "d1", Timestamp: alarm.LastTriggeredAt}
	if _, _, err = repo.UpsertAlarm(ctx, alarm); err != nil {
		t.Fatal(err)
	}
	assessment, err := s1.SaveFaultAssessment(ctx, actor, request("assessment", "assessment", 0, "d1", model.MaintenanceFaultAssessment{AlarmID: "cycle", DeviceID: "d1", Type: "FAULT", Classification: "PRODUCTION", Basis: "受控同类故障核实"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s1.ConfirmFault(ctx, actor, "assessment", model.MaintenanceActionRequest{ExpectedVersion: assessment.Version, IdempotencyKey: "confirm"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.DutyTransaction(ctx, tenant, func(tx ports.DutyTx) error {
		for i, event := range []struct {
			id, status string
			at         int64
		}{{"offline", "DISCONNECTED", start + 9*hour}, {"online", "CONNECTED", start + 11*hour}} {
			b, _ := json.Marshal(model.DeviceState{TenantID: tenant, DeviceID: "d1", ConnectionStatus: event.status})
			if err := tx.AppendEvent(model.DutyBusinessEvent{ID: event.id, TenantID: tenant, DeviceID: "d1", Type: "DEVICE_STATUS_CHANGED", Source: "device", ResourceID: "d1", ResourceVersion: int64(i + 1), OccurredAt: event.at, RecordedAt: now, Body: b}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	observe := model.MaintenanceObservationRequest{ExpectedVersion: work.Version, IdempotencyKey: "observe", Parameters: model.MaintenanceObservationParameters{ComparisonType: "SAME_INSTANCE_REPAIR", BeforeAssetRevisionID: asset.ID, AfterAssetRevisionID: asset.ID, Before: model.FactRange{Start: start, End: start + 10*hour}, After: model.FactRange{Start: start + 11*hour, End: start + 21*hour}}}
	run, err := s1.CreateObservation(ctx, actor, "repair", observe)
	if err != nil {
		t.Fatal(err)
	}
	await := func(s *maintenance.Service, id, kind string) model.AnalysisSnapshot {
		t.Helper()
		workerCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			s.Analysis.RunWorkers(workerCtx, "maintenance-real", slog.New(slog.NewTextHandler(io.Discard, nil)))
			close(done)
		}()
		defer func() { cancel(); <-done }()
		until := time.Now().Add(8 * time.Second)
		for time.Now().Before(until) {
			r, err := s.Analysis.Get(ctx, actor, kind, id)
			if err != nil {
				t.Fatal(err)
			}
			if analytics.TerminalAnalysisStatus(r.Status) {
				if r.Status == model.AnalysisFailed {
					t.Fatal(r.Error)
				}
				snapshot, err := s.Analysis.Snapshot(ctx, actor, kind, id)
				if err != nil {
					t.Fatal(err)
				}
				return snapshot
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("worker did not complete")
		return model.AnalysisSnapshot{}
	}
	snap := await(s2, run.ID, analytics.KindMaintenance)
	var comparison model.MaintenanceObservation
	json.Unmarshal(snap.Statistics, &comparison)
	if comparison.Before.ConfirmedFaultCycles != 1 || comparison.After.ConfirmedFaultCycles != 0 || comparison.Before.OfflineMs != hour || comparison.Before.StateUnknownMs != 9*hour || comparison.Before.KnownOfflineRatio == nil || *comparison.Before.KnownOfflineRatio != 1 || *comparison.Before.StateCoverage != .1 {
		t.Fatalf("actual ledger clock/denominator wrong %+v", comparison)
	}
	beforeReport, err := s1.Export(ctx, actor, analytics.KindMaintenance, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.pool.Exec(ctx, "DELETE FROM duty_business_event WHERE tenant_id=$1", tenant); err != nil {
		t.Fatal(err)
	}
	afterReport, err := restarted.Export(ctx, actor, analytics.KindMaintenance, run.ID)
	if err != nil || string(beforeReport) != string(afterReport) {
		t.Fatal("source expiry changed fixed report", err)
	}
	// ClickHouse-enabled analytics keeps the same canonical PostgreSQL ledger.
	// This tests real adapter composition, not an invented telemetry ACK clock.
	ch := monitoringClickHouseFixture(t, repo)
	chSvc := makeService(repo, ch)
	observe.IdempotencyKey = "ch-observe"
	runCH, err := chSvc.CreateObservation(ctx, actor, "repair", observe)
	if err != nil {
		t.Fatal(err)
	}
	snapCH := await(chSvc, runCH.ID, analytics.KindMaintenance)
	var unknown model.MaintenanceObservation
	json.Unmarshal(snapCH.Statistics, &unknown)
	if unknown.Before.StateUnknownMs != 10*hour || unknown.Before.FullWindowOfflineRatio != nil {
		t.Fatal("expired canonical history became online", unknown)
	}
	// Stored report remains independent of a later configured store adapter.
	old, err := chSvc.Analysis.Snapshot(ctx, actor, analytics.KindMaintenance, run.ID)
	if err != nil || !reflect.DeepEqual(old.Statistics, snap.Statistics) {
		t.Fatal("CH composition changed old fixed report", err)
	}

	amount := "1.500000"
	quote, err := s1.SaveCost(ctx, actor, request("quote", "quote", 0, "d1", model.MaintenanceCost{SourceKind: maintenance.AssetKind, SourceID: asset.ResourceID, Type: "ESTIMATE", Currency: "CNY", Material: &amount, OccurredAt: now, PlanningStart: now, PlanningEnd: now + hour, Basis: "受控报价，缺失人工与外包分项"}))
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := s1.SaveScenario(ctx, actor, request("scenario", "scenario", 0, "d1", model.InvestmentScenario{UseFinance: true, Name: "受限投入", Currency: "CNY", Budget: &amount, PlanningStart: now, PlanningEnd: now + hour, PolicyVersion: maintenance.DefaultInvestmentPolicy, Candidates: []model.InvestmentCandidate{{ID: "inspect", AssetRevisionID: asset.ID, Action: "INSPECT", RequiredTier: 1, TierBasis: "单位确认必需", QuoteRevisionID: quote.ID}}}))
	if err != nil {
		t.Fatal(err)
	}
	investment, err := s1.EvaluateScenario(ctx, actor, scenario.ResourceID, model.InvestmentEvaluationRequest{ExpectedVersion: scenario.Version, IdempotencyKey: "investment", UseFinance: true})
	if err != nil {
		t.Fatal(err)
	}
	investmentSnapshot := await(s2, investment.ID, analytics.KindInvestment)
	var trial maintenance.InvestmentEvaluation
	json.Unmarshal(investmentSnapshot.Statistics, &trial)
	if trial.Items[0].Quote != nil || trial.Items[0].BudgetStatus != "QUOTE_MISSING_OR_INCOMPARABLE" {
		t.Fatal("actual SQL made unknown fees zero", trial)
	}
	current := actor
	current.Permissions = []string{"menu:devices", "menu:maintenance"}
	s2.Analysis.Resolve = func(context.Context, analytics.Actor) (analytics.Actor, error) { return current, nil }
	if _, err = s2.Analysis.Get(ctx, actor, analytics.KindInvestment, investment.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("actual SQL old financial derivative leaked", err)
	}
	if _, err = s2.Export(ctx, actor, analytics.KindInvestment, investment.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("actual SQL financial report leaked", err)
	}
	if _, err = s2.Revision(ctx, actor, maintenance.ScenarioKind, scenario.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("actual SQL scenario body leaked", err)
	}
	visible, total, err := s2.Analysis.List(ctx, actor, analytics.KindInvestment, model.AnalysisFilter{})
	if err != nil || total != 0 || len(visible) != 0 {
		t.Fatal("SQL pagination/count leaked restricted task", total, err)
	}
	alarmAfter, err := repo.GetAlarm(ctx, tenant, "cycle")
	if err != nil || alarmAfter.TriggerCount != 2 || alarmAfter.Status != "ACTIVE" {
		t.Fatal("analysis mutated production alarm", alarmAfter, err)
	}
	t.Log("existing VM PostgreSQL/ClickHouse adapter composition; controlled synthetic asset/clock fixtures; two-pool atomic CAS; original receipts after restart; late-processing event attribution; 9h unknown/1h offline; immutable report after ledger expiry; production alarm unchanged")
}
