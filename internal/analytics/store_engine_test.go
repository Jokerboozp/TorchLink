package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func storedRun(id, kind string) model.AnalysisRun {
	return model.AnalysisRun{ID: id, TenantID: "t", Kind: kind, Creator: "operator", DeviceIDs: []string{"d1", "d2"}, PermissionsVersion: "scope1", Start: 1000, End: 2000, ConfigurationVersion: "config1", AlgorithmVersion: "algorithm1", Parameters: json.RawMessage(`{"threshold":3}`), IdempotencyKey: id}
}
func createStoredRun(t *testing.T, s *Store, id, kind string) model.AnalysisRun {
	t.Helper()
	r, err := s.CreateAnalysisRun(context.Background(), storedRun(id, kind), 100)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAnalysisStoreAtomicCheckpointAndFencing(t *testing.T) {
	ctx := context.Background()
	now := time.UnixMilli(10000)
	s := NewMemoryStoreWithClock(func() time.Time { return now })
	createStoredRun(t, s, "one", "quality")
	createStoredRun(t, s, "two", "quality")
	r, err := s.ClaimAnalysisRun(ctx, "w1", time.Minute, []string{"quality"})
	if err != nil || r.ID != "one" {
		t.Fatal(r, err)
	}
	if _, err = s.ClaimAnalysisRun(ctx, "w2", time.Minute, []string{"quality"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("same kind ran concurrently", err)
	}
	b := model.AnalysisBatch{ID: "batch1", Processed: 2, Checkpoint: json.RawMessage(`{"cursor":2}`), Outputs: []model.AnalysisOutput{{ID: "metric", Kind: "metric", DeviceID: "d1", Body: json.RawMessage(`{"count":2}`)}}}
	r, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, b)
	if err != nil || r.Processed != 2 || string(r.Checkpoint) != `{"cursor":2}` {
		t.Fatal(r, err)
	}
	if again, err := s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, b); err != nil || again.Version != r.Version {
		t.Fatal("checkpoint replay changed run", again, err)
	}
	bad := b
	bad.Processed = 3
	if _, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, bad); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("conflicting replay", err)
	}
	bad = b
	bad.ID = "fail"
	bad.Outputs = append(bad.Outputs, model.AnalysisOutput{ID: "leak", Kind: "metric", DeviceID: "forbidden", Body: json.RawMessage(`{}`)})
	if _, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, bad); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal(err)
	}
	outputs, total, err := s.ListAnalysisOutputs(ctx, "t", model.AnalysisFilter{RunID: r.ID})
	if err != nil || total != 1 || len(outputs) != 1 {
		t.Fatal("output/checkpoint failed rollback", outputs, total, err)
	}
	now = now.Add(2 * time.Minute)
	fresh, err := s.ClaimAnalysisRun(ctx, "w2", time.Minute, []string{"quality"})
	if err != nil || fresh.ID != r.ID || fresh.LeaseToken <= r.LeaseToken || fresh.Processed != 2 {
		t.Fatal("restart lost checkpoint", fresh, err)
	}
	b.ID = "stale"
	if _, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, b); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("stale worker committed", err)
	}
	fresh, err = s.StopAnalysisRun(ctx, "t", fresh.ID, fresh.Version)
	if err != nil || fresh.Status != model.AnalysisCancelled {
		t.Fatal(fresh, err)
	}
	if _, err = s.CommitAnalysisBatch(ctx, "t", fresh.ID, fresh.LeaseToken-1, b); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("stopped worker wrote", err)
	}
	next, err := s.ClaimAnalysisRun(ctx, "w3", time.Minute, []string{"quality"})
	if err != nil || next.ID != "two" {
		t.Fatal(next, err)
	}
}

func TestAnalysisCreateIdempotencyQueueAndExplicitScope(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	r := createStoredRun(t, s, "one", "quality")
	request := storedRun("different-id", "quality")
	request.IdempotencyKey = "one"
	again, err := s.CreateAnalysisRun(ctx, request, 1)
	if err != nil || again.ID != r.ID {
		t.Fatal(again, err)
	}
	request.DeviceIDs = []string{"d1"}
	if _, err = s.CreateAnalysisRun(ctx, request, 100); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("same key changed scope", err)
	}
	if _, err = s.CreateAnalysisRun(ctx, storedRun("two", "quality"), 1); !errors.Is(err, model.ErrAnalysisQueueFull) {
		t.Fatal("queue overflow accepted", err)
	}
	if _, err = s.GetAnalysisRun(ctx, "other", r.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("tenant leak", err)
	}
	for _, ids := range [][]string{nil, {"d1"}} {
		rows, n, err := s.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{DeviceScopeSet: true, DeviceIDs: ids})
		if err != nil || n != 0 || len(rows) != 0 {
			t.Fatal("partial scope revealed run", rows, n, err)
		}
	}
	rows, n, err := s.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{DeviceScopeSet: true, DeviceIDs: []string{"d1", "d2"}})
	if err != nil || n != 1 || len(rows) != 1 {
		t.Fatal(rows, n, err)
	}
}

func TestAnalysisConcurrentClaimAndBacklogBeyondFirstPage(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 101; i++ {
		r := storedRun(fmt.Sprintf("a%03d", i), "quality")
		if _, err := s.CreateAnalysisRun(ctx, r, 200); err != nil {
			t.Fatal(err)
		}
	}
	createStoredRun(t, s, "z", "monitoring")
	var mu sync.Mutex
	claims := []model.AnalysisRun{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := s.ClaimAnalysisRun(ctx, fmt.Sprint(i), time.Minute, []string{"quality", "monitoring"})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				claims = append(claims, r)
			} else if !errors.Is(err, model.ErrNotFound) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(claims) != 2 || claims[0].Kind == claims[1].Kind {
		t.Fatal("kind exclusion or full backlog scan failed", claims)
	}
}

func TestAnalysisSnapshotsConfigReviewsAndAI(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	createStoredRun(t, s, "one", "quality")
	r, err := s.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{"quality"})
	if err != nil {
		t.Fatal(err)
	}
	b := model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Processed: 3, Outputs: []model.AnalysisOutput{{ID: "finding", Kind: "finding", DeviceID: "d1", Body: json.RawMessage(`{"count":3}`)}}, Evidence: []model.AnalysisEvidence{{ID: "proof", SourceKind: "raw", SourceID: "raw1", DeviceID: "d1", ResourceVersion: "1", Summary: json.RawMessage(`{"parsed":false}`), OriginalAvailability: "EXPIRED"}}, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 2000, MissingSources: []string{"state history"}, UncomputableMetrics: []string{"online ratio"}, Limitations: []string{"state seed unavailable"}, Statistics: json.RawMessage(`{"unknown":1}`)}}
	r, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, b)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.GetAnalysisSnapshot(ctx, "t", r.SnapshotID)
	if err != nil || snap.FactsHash == "" || snap.Version != 1 || snap.Start != 1000 || len(snap.DeviceIDs) != 2 {
		t.Fatal(snap, err)
	}
	if _, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, model.AnalysisBatch{ID: "edit", Status: model.AnalysisSucceeded, Snapshot: &model.AnalysisSnapshot{ID: snap.ID}}); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("frozen run changed", err)
	}
	cfg := model.AnalysisConfigRevision{ID: "config1", TenantID: "t", Kind: "quality-profile", ResourceID: "pressure", DeviceIDs: []string{"d1"}, Scope: "SHARED", Creator: "operator", Body: json.RawMessage(`{"period":60}`)}
	one, err := s.PutAnalysisConfig(ctx, cfg, 0)
	if err != nil || one.Version != 1 {
		t.Fatal(one, err)
	}
	cfg.ID = "config2"
	cfg.Body = json.RawMessage(`{"period":90}`)
	two, err := s.PutAnalysisConfig(ctx, cfg, 1)
	if err != nil || two.Version != 2 {
		t.Fatal(two, err)
	}
	cfg.ID = "stale"
	if _, err = s.PutAnalysisConfig(ctx, cfg, 1); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal(err)
	}
	old, err := s.GetAnalysisConfig(ctx, "t", "config1")
	if err != nil || string(old.Body) != `{"period":60}` {
		t.Fatal("history overwritten", old, err)
	}
	review := model.AnalysisReview{ID: "review", TenantID: "t", RunID: r.ID, ResourceID: "finding", ResourceVersion: 1, Result: "CONFIRMED", Reviewer: "operator", Explanation: "现场已核对", IdempotencyKey: "review-key"}
	saved, err := s.AppendAnalysisReview(ctx, review, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	review.ID = "retry"
	again, err := s.AppendAnalysisReview(ctx, review, r.Version)
	if err != nil || again.ID != saved.ID {
		t.Fatal("review replay", again, err)
	}
	ai := model.AnalysisAIRevision{ID: "ai1", TenantID: "t", RunID: r.ID, SnapshotID: snap.ID, SnapshotVersion: snap.Version, WorkflowID: "quality-analyst", Model: "configured", PromptVersion: "1", Status: model.AnalysisQueued, PermissionVersion: "scope1"}
	queued, err := s.PutAnalysisAIRevision(ctx, ai, 0)
	if err != nil {
		t.Fatal(err)
	}
	ai.ID = "duplicate"
	againAI, err := s.PutAnalysisAIRevision(ctx, ai, 0)
	if err != nil || againAI.ID != queued.ID {
		t.Fatal("duplicate AI call", againAI, err)
	}
	queued.Status = model.AnalysisSucceeded
	queued.Interpretation = json.RawMessage(`{"summary":"证据不足"}`)
	queued.FactIDs = []string{"unknown"}
	if _, err = s.PutAnalysisAIRevision(ctx, queued, queued.Version); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("unknown AI reference accepted", err)
	}
	queued.FactIDs = []string{"finding", "proof"}
	complete, err := s.PutAnalysisAIRevision(ctx, queued, queued.Version)
	if err != nil || complete.CompletedAt == 0 {
		t.Fatal(complete, err)
	}
	if after, err := s.GetAnalysisRun(ctx, "t", r.ID); err != nil || after.Status != model.AnalysisPartial {
		t.Fatal("AI changed fact status", after, err)
	}
}

func TestAnalysisPaginationAndCanonicalHash(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	for i := 0; i < 110; i++ {
		r := storedRun(fmt.Sprint(i), "quality")
		if _, err := s.CreateAnalysisRun(ctx, r, 200); err != nil {
			t.Fatal(err)
		}
	}
	rows, n, err := s.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{})
	if err != nil || len(rows) != 20 || n != 110 {
		t.Fatal(len(rows), n, err)
	}
	rows, n, err = s.ListAnalysisRuns(ctx, "t", model.AnalysisFilter{Limit: 200})
	if err != nil || len(rows) != 100 || n != 110 {
		t.Fatal(len(rows), n, err)
	}
	a, _ := AnalysisHash(json.RawMessage(`{"a":9007199254740992,"b":2}`))
	b, _ := AnalysisHash(json.RawMessage(`{ "b":2,"a":9007199254740992 }`))
	c, _ := AnalysisHash(json.RawMessage(`{"a":9007199254740993,"b":2}`))
	if a != b || a == c {
		t.Fatal("noncanonical hash or precision loss")
	}
}

func TestAnalysisFixedInputManifestRejectsChangedSourceAndSnapshot(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	createStoredRun(t, s, "one", "quality")
	r, err := s.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{"quality"})
	if err != nil {
		t.Fatal(err)
	}
	sources := []model.AnalysisSourceCoverage{{Source: "measurements", Start: 1000, End: 2000, ReadAt: 5000, Complete: true, Version: "source-v1"}}
	prepare := model.AnalysisBatch{ID: "pin", Status: model.AnalysisPreparing, FreezeInputs: true, InputHashes: []string{"chunk-v1"}, Sources: sources, DataCutoff: 5000, Checkpoint: json.RawMessage(`{"inputPinned":true}`)}
	r, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, prepare)
	if err != nil || !r.InputsFrozen || r.DataCutoff != 5000 {
		t.Fatal(r, err)
	}
	mutated := prepare
	mutated.ID = "changed-input"
	mutated.InputHashes = []string{"chunk-v2"}
	if _, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, mutated); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("fixed inputs changed", err)
	}
	final := model.AnalysisBatch{ID: "final", Status: model.AnalysisSucceeded, Processed: 1, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", InputHashes: []string{"chunk-v1"}, Sources: slicesCloneSources(sources), DataCutoff: 5000, Statistics: json.RawMessage(`{}`)}}
	final.Snapshot.Sources[0].ReadAt++
	if _, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, final); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("snapshot source point differs from pinned source", err)
	}
	if _, err = s.GetAnalysisSnapshot(ctx, "t", "snapshot"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("failed final commit persisted snapshot", err)
	}
	final.Snapshot.Sources[0].ReadAt--
	r, err = s.CommitAnalysisBatch(ctx, "t", r.ID, r.LeaseToken, final)
	if err != nil || r.Status != model.AnalysisSucceeded {
		t.Fatal(r, err)
	}
}

func slicesCloneSources(v []model.AnalysisSourceCoverage) []model.AnalysisSourceCoverage {
	return append([]model.AnalysisSourceCoverage(nil), v...)
}
