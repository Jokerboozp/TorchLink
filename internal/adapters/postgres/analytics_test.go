package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/model"
)

func sqlAnalysisRun(id string) model.AnalysisRun {
	return model.AnalysisRun{ID: id, TenantID: "t", Kind: "quality", Creator: "operator", DeviceIDs: []string{"d1", "d2"}, PermissionsVersion: "scope1", Start: 1000, End: 2000, ConfigurationVersion: "1", AlgorithmVersion: "1", Parameters: json.RawMessage(`{}`), IdempotencyKey: id}
}

func TestAnalyticsSQLReplicasRestartAtomicCheckpointAndStop(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	// A separate pool/connection simulates a different process. Both use the same
	// isolated test schema and primary database, never a process-local mutex.
	pool, err := pgxpool.NewWithConfig(ctx, r.pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	replica := &Repository{pool: pool}
	for _, id := range []string{"one", "two"} {
		if _, err = r.CreateAnalysisRun(ctx, sqlAnalysisRun(id), 100); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	claims := []model.AnalysisRun{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := r
			if i%2 == 1 {
				target = replica
			}
			run, err := target.ClaimAnalysisRun(ctx, fmt.Sprint(i), time.Minute, []string{"quality"})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				claims = append(claims, run)
			} else if !errors.Is(err, model.ErrNotFound) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(claims) != 1 {
		t.Fatal("multiple replicas claimed same tenant+kind", claims)
	}
	run := claims[0]
	b := model.AnalysisBatch{ID: "batch1", Processed: 5, Checkpoint: json.RawMessage(`{"next":5}`), Outputs: []model.AnalysisOutput{{ID: "metric", Kind: "metric", DeviceID: "d1", Body: json.RawMessage(`{"count":5}`)}}}
	run, err = r.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, b)
	if err != nil {
		t.Fatal(err)
	}
	again, err := replica.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, b)
	if err != nil || again.Version != run.Version {
		t.Fatal("checkpoint replay", again, err)
	}
	bad := b
	bad.ID = "rolled-back"
	bad.Processed = 6
	bad.Outputs = []model.AnalysisOutput{{ID: "rolled-back", Kind: "metric", DeviceID: "d1", Body: json.RawMessage(`{}`)}, {ID: "forbidden", Kind: "metric", DeviceID: "unknown", Body: json.RawMessage(`{}`)}}
	if _, err = r.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, bad); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal(err)
	}
	outputs, n, err := r.ListAnalysisOutputs(ctx, "t", model.AnalysisFilter{RunID: run.ID})
	if err != nil || n != 1 || len(outputs) != 1 {
		t.Fatal("batch rollback left output", outputs, n, err)
	}
	restored, err := replica.GetAnalysisRun(ctx, "t", run.ID)
	if err != nil || restored.Processed != 5 || string(restored.Checkpoint) != `{"next": 5}` && string(restored.Checkpoint) != `{"next":5}` {
		t.Fatal("restart checkpoint", restored, err)
	}
	// Expiry is controlled in the durable fixture, avoiding timing-dependent sleep.
	if _, err = r.pool.Exec(ctx, `UPDATE analysis_document SET body=jsonb_set(body,'{leaseExpiresAt}','1') WHERE tenant_id='t' AND kind='run' AND id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := replica.ClaimAnalysisRun(ctx, "takeover", time.Minute, []string{"quality"})
	if err != nil || recovered.ID != run.ID || recovered.LeaseToken <= run.LeaseToken || recovered.Processed != 5 || recovered.StartedAt != run.StartedAt {
		t.Fatal(recovered, err)
	}
	b.ID = "stale"
	if _, err = r.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, b); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("stale owner committed", err)
	}
	stopped, err := r.StopAnalysisRun(ctx, "t", run.ID, recovered.Version)
	if err != nil || stopped.Status != model.AnalysisCancelled {
		t.Fatal(stopped, err)
	}
	if _, err = replica.RenewAnalysisLease(ctx, "t", run.ID, recovered.LeaseToken, time.Minute); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("stopped owner renewed", err)
	}
	next, err := replica.ClaimAnalysisRun(ctx, "next", time.Minute, []string{"quality"})
	if err != nil || next.ID == run.ID {
		t.Fatal(next, err)
	}
}

func TestAnalyticsSQLScopePaginationIdempotencyAndFrozenVersions(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	one, err := r.CreateAnalysisRun(ctx, sqlAnalysisRun("one"), 100)
	if err != nil {
		t.Fatal(err)
	}
	retry := sqlAnalysisRun("retry")
	retry.IdempotencyKey = "one"
	saved, err := r.CreateAnalysisRun(ctx, retry, 1)
	if err != nil || saved.ID != one.ID {
		t.Fatal(saved, err)
	}
	retry.End++
	if _, err = r.CreateAnalysisRun(ctx, retry, 100); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("conflicting duplicate", err)
	}
	if _, err = r.GetAnalysisRun(ctx, "other", one.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross tenant", err)
	}
	for _, ids := range [][]string{nil, {"d1"}} {
		rows, n, err := r.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{DeviceScopeSet: true, DeviceIDs: ids})
		if err != nil || n != 0 || len(rows) != 0 {
			t.Fatal("scope leaked IDs or hidden total", rows, n, err)
		}
	}
	for i := 0; i < 24; i++ {
		if _, err = r.CreateAnalysisRun(ctx, sqlAnalysisRun(fmt.Sprintf("page-%02d", i)), 100); err != nil {
			t.Fatal(err)
		}
	}
	rows, n, err := r.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{DeviceScopeSet: true, DeviceIDs: []string{"d1", "d2"}})
	if err != nil || len(rows) != 20 || n != 25 {
		t.Fatal(len(rows), n, err)
	}
	claimed, err := r.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{"quality"})
	if err != nil {
		t.Fatal(err)
	}
	sources := []model.AnalysisSourceCoverage{{Source: "state", Start: 1000, End: 2000, ReadAt: 2000, Version: "seed-unknown", Complete: false}}
	pinned, err := r.CommitAnalysisBatch(ctx, "t", claimed.ID, claimed.LeaseToken, model.AnalysisBatch{ID: "pin", Status: model.AnalysisPreparing, FreezeInputs: true, InputHashes: []string{"input1"}, Sources: sources, DataCutoff: 2000})
	if err != nil || !pinned.InputsFrozen {
		t.Fatal(pinned, err)
	}
	changed := model.AnalysisBatch{ID: "changed", Status: model.AnalysisPreparing, FreezeInputs: true, InputHashes: []string{"input2"}, Sources: sources, DataCutoff: 2000}
	if _, err = r.CommitAnalysisBatch(ctx, "t", claimed.ID, claimed.LeaseToken, changed); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("source manifest changed", err)
	}
	final := model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Processed: 1, Outputs: []model.AnalysisOutput{{ID: "fact", Kind: "metric", DeviceID: "d1", Body: json.RawMessage(`{"unknown":true}`)}}, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 2000, InputHashes: []string{"input1"}, Sources: sources, MissingSources: []string{"state-seed"}, UncomputableMetrics: []string{"state-availability"}, Limitations: []string{"pre-collection state unknown"}, Statistics: json.RawMessage(`{"unknown":1}`)}}
	done, err := r.CommitAnalysisBatch(ctx, "t", claimed.ID, claimed.LeaseToken, final)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := r.GetAnalysisSnapshot(ctx, "t", done.SnapshotID)
	if err != nil || snap.FactsHash == "" || snap.Start != 1000 || snap.Version != 1 {
		t.Fatal(snap, err)
	}
	if _, err = r.CommitAnalysisBatch(ctx, "t", done.ID, done.LeaseToken, model.AnalysisBatch{ID: "overwrite", Status: model.AnalysisSucceeded, Snapshot: &model.AnalysisSnapshot{ID: snap.ID}}); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("frozen snapshot editable", err)
	}
	cfg := model.AnalysisConfigRevision{ID: "cfg1", TenantID: "t", Kind: "profile", ResourceID: "pressure", DeviceIDs: []string{"d1"}, Scope: "SHARED", Creator: "operator", Body: json.RawMessage(`{"period":60}`)}
	if _, err = r.PutAnalysisConfig(ctx, cfg, 0); err != nil {
		t.Fatal(err)
	}
	cfg.ID = "cfg2"
	cfg.Body = json.RawMessage(`{"period":90}`)
	if v, err := r.PutAnalysisConfig(ctx, cfg, 1); err != nil || v.Version != 2 {
		t.Fatal(v, err)
	}
	cfg.ID = "stale"
	if _, err = r.PutAnalysisConfig(ctx, cfg, 1); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("version conflict not rejected", err)
	}
	history, total, err := r.ListAnalysisConfigs(ctx, "t", model.AnalysisFilter{Kind: "profile", ResourceID: "pressure"})
	if err != nil || total != 2 || len(history) != 2 {
		t.Fatal(history, total, err)
	}
}

func TestAnalyticsSQLLeaseExpiresDuringCommitRollsBackEntireBatch(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	// The database intentionally stalls the final run update. Validation at the
	// beginning alone would incorrectly accept this batch after lease expiry.
	_, err := r.pool.Exec(ctx, `CREATE FUNCTION analysis_test_slow_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(1); RETURN NEW; END $$;
CREATE TRIGGER analysis_test_slow_update BEFORE UPDATE ON analysis_document FOR EACH ROW WHEN (NEW.kind='run' AND NEW.status='RUNNING') EXECUTE FUNCTION analysis_test_slow_update();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.CreateAnalysisRun(ctx, sqlAnalysisRun("one"), 100); err != nil {
		t.Fatal(err)
	}
	run, err := r.ClaimAnalysisRun(ctx, "worker", 500*time.Millisecond, []string{"quality"})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = r.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "expired-commit", Processed: 1, Checkpoint: json.RawMessage(`{"next":1}`), Outputs: []model.AnalysisOutput{{ID: "rolled-back", Kind: "metric", DeviceID: "d1", Body: json.RawMessage(`{}`)}}})
	if !errors.Is(err, model.ErrAnalysisLeaseLost) || time.Since(started) < 900*time.Millisecond {
		t.Fatal("lease not checked at transaction commit", err)
	}
	rows, n, err := r.ListAnalysisOutputs(ctx, "t", model.AnalysisFilter{RunID: run.ID})
	if err != nil || n != 0 || len(rows) != 0 {
		t.Fatal("expired transaction persisted output", rows, n, err)
	}
	current, err := r.GetAnalysisRun(ctx, "t", run.ID)
	if err != nil || current.Version != run.Version || current.Processed != 0 || current.Status != model.AnalysisPreparing {
		t.Fatal("expired transaction persisted checkpoint", current, err)
	}
}
