package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func TestAnalyticsSQLFinancialFilteringBeforeCountAndPagination(t *testing.T) {
	ctx, r := context.Background(), testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"fund1", "plain1", "fund2", "plain2"} {
		run := sqlAnalysisRun(id)
		run.Kind = analytics.KindInvestment
		if id[:4] == "fund" {
			run.RequiredPermissions = []string{analytics.FinanceReadOperation}
		}
		if _, err := r.CreateAnalysisRun(ctx, run, 100); err != nil {
			t.Fatal(err)
		}
	}
	for offset, expected := range []string{"plain1", "plain2"} {
		items, total, err := r.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{Kind: analytics.KindInvestment, RequiredScopeSet: true, Limit: 1, Offset: offset})
		if err != nil || total != 2 || len(items) != 1 || items[0].ID != expected {
			t.Fatal("fund metadata affected ordinary page", items, total, err)
		}
	}
	items, total, err := r.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{Kind: analytics.KindInvestment, RequiredScopeSet: true, AllowedRequiredPermissions: []string{analytics.FinanceReadOperation}, Limit: 100})
	if err != nil || total != 4 || len(items) != 4 {
		t.Fatal(items, total, err)
	}
	changed := sqlAnalysisRun("newid")
	changed.Kind = analytics.KindInvestment
	changed.IdempotencyKey = "fund1"
	if _, err = r.CreateAnalysisRun(ctx, changed, 100); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("financial boundary lost in persisted idempotency hash", err)
	}
}

func TestAnalyticsSQLConfigBatchAtomicRollbackAndReplicaSlotCAS(t *testing.T) {
	ctx, r := context.Background(), testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, r.pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	replica := &Repository{pool: pool}
	revision := func(id, kind, resource string) model.AnalysisConfigRevision {
		return model.AnalysisConfigRevision{ID: id, TenantID: "t", Kind: kind, ResourceID: resource, Creator: "operator", Scope: "SHARED", DeviceIDs: []string{"d1"}, Body: json.RawMessage(`{"fixed":true}`)}
	}
	if _, err = r.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{revision("slot0", "SLOT", "d1"), revision("asset0", "ASSET", "physical0")}, []int64{0, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err = replica.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{revision("slot-orphan", "SLOT", "d1"), revision("asset-stale", "ASSET", "physical0")}, []int64{1, 0}); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal(err)
	}
	if _, err = r.GetAnalysisConfig(ctx, "t", "slot-orphan"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("first write survived second-member failure", err)
	}
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := r
			if i%2 == 1 {
				target = replica
			}
			_, e := target.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{revision(fmt.Sprintf("slot-%d", i), "SLOT", "d1"), revision(fmt.Sprintf("asset-%d", i), "ASSET", fmt.Sprintf("physical-%d", i))}, []int64{1, 0})
			if e == nil {
				success.Add(1)
			} else if !errors.Is(e, model.ErrAnalysisConflict) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	_, total, err := r.ListAnalysisConfigs(ctx, "t", model.AnalysisFilter{Kind: "ASSET", Limit: 100})
	if err != nil || success.Load() != 1 || total != 2 {
		t.Fatal("replica overlap or orphan asset", success.Load(), total, err)
	}
}
