package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func TestFinancialRunRequiresCurrentPermissionForWholeFactsAndPagination(t *testing.T) {
	ctx := context.Background()
	s, a, mu := serviceFixture()
	mu.Lock()
	a.Permissions = []string{"menu:devices", "menu:maintenance", CreateOperation(KindMaintenance), "POST " + RunCollection(KindMaintenance) + "/:id/stop", FinanceReadOperation}
	mu.Unlock()
	_ = s.Register(KindMaintenance, func(context.Context, *Execution) error { return nil })
	q := serviceRequest("financial", "a")
	q.RequiredPermissions = []string{FinanceReadOperation, FinanceReadOperation}
	fund, err := s.Create(ctx, *a, KindMaintenance, "v1", q)
	if err != nil || len(fund.RequiredPermissions) != 1 {
		t.Fatal(fund, err)
	}
	q.RequiredPermissions = nil
	if _, err = s.Create(ctx, *a, KindMaintenance, "v1", q); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("idempotency silently changed monetary boundary", err)
	}
	claimed, err := s.Store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{KindMaintenance})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != fund.ID {
		t.Fatal(claimed.ID)
	}
	fund, err = s.Store.CommitAnalysisBatch(ctx, "t", fund.ID, claimed.LeaseToken, model.AnalysisBatch{ID: "complete", Status: model.AnalysisSucceeded, Outputs: []model.AnalysisOutput{{ID: "money-derived-rank", Kind: "metrics", DeviceID: "a", Body: json.RawMessage(`{"rank":1,"money":500}`)}}, Evidence: []model.AnalysisEvidence{{ID: "cost-proof", DeviceID: "a", SourceKind: "cost", SourceID: "cost-1", Summary: json.RawMessage(`{}`)}}, Snapshot: &model.AnalysisSnapshot{ID: "money-snapshot", DataCutoff: 10000, Statistics: json.RawMessage(`{"money":500}`)}})
	if err != nil {
		t.Fatal(err)
	}
	q.IdempotencyKey = "ordinary"
	plain, err := s.Create(ctx, *a, KindMaintenance, "v1", q)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	a.Permissions = a.Permissions[:len(a.Permissions)-1]
	mu.Unlock() // unchanged stamp still rechecks actual permission
	if run, e := s.Get(ctx, *a, KindMaintenance, fund.ID); !errors.Is(e, ErrForbidden) || run.ID != "" {
		t.Fatal("financial body leaked", run, e)
	}
	if _, e := s.Snapshot(ctx, *a, KindMaintenance, fund.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, _, e := s.Outputs(ctx, *a, KindMaintenance, fund.ID, model.AnalysisFilter{}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, _, e := s.Evidence(ctx, *a, KindMaintenance, fund.ID, model.AnalysisFilter{}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Stop(ctx, *a, KindMaintenance, fund.ID, fund.Version); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	items, total, e := s.List(ctx, *a, KindMaintenance, model.AnalysisFilter{Limit: 1})
	if e != nil || total != 1 || len(items) != 1 || items[0].ID != plain.ID {
		t.Fatal("financial existence affected counts or page", items, total, e)
	}
	q.RequiredPermissions = []string{FinanceReadOperation}
	q.IdempotencyKey = "denied"
	if _, e = s.Create(ctx, *a, KindMaintenance, "v1", q); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	q.RequiredPermissions = []string{"POST /api/v1/rules"}
	if _, e = s.Create(ctx, *a, KindMaintenance, "v1", q); !errors.Is(e, model.ErrAnalysisInvalid) {
		t.Fatal(e)
	}
}

func TestFinancialQueuedWorkerCancelsAfterIndependentPermissionRevocation(t *testing.T) {
	ctx := context.Background()
	s, a, mu := serviceFixture()
	mu.Lock()
	a.Permissions = []string{"menu:devices", "menu:maintenance", CreateOperation(KindInvestment), FinanceReadOperation}
	mu.Unlock()
	called := false
	_ = s.Register(KindInvestment, func(context.Context, *Execution) error { called = true; return nil })
	q := serviceRequest("queued-finance", "a")
	q.RequiredPermissions = []string{FinanceReadOperation}
	run, err := s.Create(ctx, *a, KindInvestment, "v1", q)
	if err != nil {
		t.Fatal(err)
	}
	run, err = s.Store.ClaimAnalysisRun(ctx, "worker", time.Second, []string{KindInvestment})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	a.Permissions = a.Permissions[:len(a.Permissions)-1]
	mu.Unlock()
	s.execute(ctx, run, slog.New(slog.NewTextHandler(io.Discard, nil)))
	saved, err := s.Store.GetAnalysisRun(ctx, "t", run.ID)
	if err != nil || called || saved.Status != model.AnalysisCancelled {
		t.Fatal("revoked finance worker computed facts", saved, err)
	}
}

func TestQualityDerivedMaintenanceVersionInheritsWholeInputPermission(t *testing.T) {
	ctx := context.Background()
	s, a, mu := serviceFixture()
	mu.Lock()
	a.Permissions = []string{"menu:devices", "menu:maintenance", CreateOperation(KindMaintenance), QualityReadPermission}
	mu.Unlock()
	_ = s.Register(KindMaintenance, func(context.Context, *Execution) error { return nil })
	q := serviceRequest("quality-derived", "a")
	q.Parameters = json.RawMessage(`{"observation":{"qualityRunIds":["fixed-quality"]}}`)
	q.RequiredPermissions = []string{QualityReadPermission}
	run, err := s.Create(ctx, *a, KindMaintenance, "v1", q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, *a, KindMaintenance, run.ID); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	a.Permissions = a.Permissions[:len(a.Permissions)-1]
	mu.Unlock()
	if _, err = s.Get(ctx, *a, KindMaintenance, run.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("old quality-derived result readable without quality permission", err)
	}
	rows, total, err := s.List(ctx, *a, KindMaintenance, model.AnalysisFilter{})
	if err != nil || len(rows) != 0 || total != 0 {
		t.Fatal("quality derivation leaked through list metadata", rows, total, err)
	}
}
