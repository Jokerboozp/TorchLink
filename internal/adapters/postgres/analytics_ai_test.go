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
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
)

func sqlAIService(t *testing.T, r *Repository) (*analytics.AIService, analytics.Actor, model.AnalysisRun) {
	return sqlAIServiceKind(t, r, analytics.KindDataQuality)
}
func sqlAIServiceKind(t *testing.T, r *Repository, kind string) (*analytics.AIService, analytics.Actor, model.AnalysisRun) {
	t.Helper()
	ctx := context.Background()
	request := sqlAnalysisRun("facts")
	request.Kind = kind
	if _, err := r.CreateAnalysisRun(ctx, request, 100); err != nil {
		t.Fatal(err)
	}
	run, err := r.ClaimAnalysisRun(ctx, "facts", time.Minute, []string{request.Kind})
	if err != nil {
		t.Fatal(err)
	}
	outputs := []model.AnalysisOutput{{ID: "fact", Kind: "findings", DeviceID: "d1", Body: json.RawMessage(`{"unknown":true}`)}}
	if kind == analytics.KindMonitoring {
		outputs = append(outputs, model.AnalysisOutput{ID: "interval", Kind: "intervals", DeviceID: "d1", Body: json.RawMessage(`{"unknownMs":500}`)}, model.AnalysisOutput{ID: "group", Kind: "dependency-groups", Body: json.RawMessage(`{"visibleMembers":["d1","d2"]}`)}, model.AnalysisOutput{ID: "private", Kind: "input-manifest", Body: json.RawMessage(`{"private":true}`)})
	}
	run, err = r.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Outputs: outputs, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"unknown":2}`), Limitations: []string{"state seed unknown"}}})
	if err != nil {
		t.Fatal(err)
	}
	actor := analytics.Actor{TenantID: "t", Username: "operator", AccessVersion: "scope1", AllDevices: true, Permissions: []string{"*"}}
	facts := analytics.NewService(r, config.AnalyticsConfig{}, func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }, nil)
	return analytics.NewAIService(facts, nil), actor, run
}
func TestAnalyticsAISQLDuplicateReplicaClaimsStopAndUnknownRestart(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	service, a, run := sqlAIService(t, r)
	job, err := service.Create(ctx, a, run.Kind, run.ID, analytics.CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "first"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.Create(ctx, a, run.Kind, run.ID, analytics.CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "second"})
	if err != nil || again.ID != job.ID {
		t.Fatal(again, err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, r.pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	other := &Repository{pool: pool}
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := []model.AnalysisAIRevision{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := r
			if i%2 == 1 {
				target = other
			}
			v, e := target.ClaimAnalysisAIRevision(ctx, fmt.Sprint(i), time.Minute, 4*time.Minute, []string{analytics.WorkflowDataQuality})
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				claims = append(claims, v)
			} else if !errors.Is(e, model.ErrNotFound) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if len(claims) != 1 {
		t.Fatal("duplicate AI external claims", claims)
	}
	claimed := claims[0]
	if claimed.LeaseToken <= 0 || claimed.HarnessRunID == "" || claimed.Deadline <= claimed.StartedAt {
		t.Fatal(claimed)
	}
	input, err := service.BuildInput(ctx, claimed)
	if err != nil || !input.Coverage.SummaryProvided {
		t.Fatal(input, err)
	}
	if _, err = r.RecordAnalysisAIFacts(ctx, "t", job.ID, claimed.LeaseToken, []string{"foreign"}); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("foreign fact registered", err)
	}
	if _, err = r.pool.Exec(ctx, `UPDATE analysis_document SET body=jsonb_set(body,'{leaseExpiresAt}','1') WHERE tenant_id='t' AND kind='ai' AND id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = other.ClaimAnalysisAIRevision(ctx, "restart", time.Minute, 4*time.Minute, []string{analytics.WorkflowDataQuality}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("running external call invoked after restart", err)
	}
	failed, err := r.GetAnalysisAIRevision(ctx, "t", job.ID)
	if err != nil || failed.Status != model.AnalysisFailed || failed.LeaseToken <= claimed.LeaseToken {
		t.Fatal(failed, err)
	}
	if _, err = other.FinishAnalysisAIRevision(ctx, "t", job.ID, claimed.LeaseToken, model.AnalysisAIResult{}, "", "stale"); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal(err)
	}
	newJob, err := service.Create(ctx, a, run.Kind, run.ID, analytics.CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "retry", Reinterpret: true})
	if err != nil || newJob.ID == failed.ID {
		t.Fatal(newJob, err)
	}
	newJob, err = r.ClaimAnalysisAIRevision(ctx, "new", time.Minute, 4*time.Minute, []string{analytics.WorkflowDataQuality})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := service.Stop(ctx, a, run.Kind, run.ID, newJob.ID, newJob.Version)
	if err != nil || stopped.Status != model.AnalysisCancelled {
		t.Fatal(stopped, err)
	}
	if _, err = service.ReadBoundAnalysisFacts(ctx, analytics.AIIdentity(newJob), "summary", 20, 0); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("stopped MCP binding disclosed facts", err)
	}
	facts, err := r.GetAnalysisRun(ctx, "t", run.ID)
	if err != nil || facts.Status != model.AnalysisPartial || facts.Version != run.Version {
		t.Fatal("AI changed deterministic facts", facts, err)
	}
}

func TestAnalyticsAISQLLeaseExpiryDuringResultCommitRollsBack(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	service, a, run := sqlAIService(t, r)
	if _, err := r.pool.Exec(ctx, `CREATE FUNCTION analysis_ai_test_slow() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(1); RETURN NEW; END $$; CREATE TRIGGER analysis_ai_test_slow BEFORE UPDATE ON analysis_document FOR EACH ROW WHEN (NEW.kind='ai' AND NEW.status='SUCCEEDED') EXECUTE FUNCTION analysis_ai_test_slow();`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, a, run.Kind, run.ID, analytics.CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "job"}); err != nil {
		t.Fatal(err)
	}
	job, err := r.ClaimAnalysisAIRevision(ctx, "worker", 500*time.Millisecond, 4*time.Minute, []string{analytics.WorkflowDataQuality})
	if err != nil {
		t.Fatal(err)
	}
	input, err := service.BuildInput(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.GetAnalysisAIRevision(ctx, "t", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := analytics.DecodeAnalysisAIResult(`{"summary":"x","interpretations":[{"text":"未知状态待核实","factIds":["fact"]}],"suggestedVerification":[],"limitations":[]}`, current.SentFactIDs, current.DeviceIDs, input.Coverage)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err = r.FinishAnalysisAIRevision(ctx, "t", job.ID, job.LeaseToken, result, "test-model", ""); !errors.Is(err, model.ErrAnalysisLeaseLost) || time.Since(started) < 900*time.Millisecond {
		t.Fatal("expired AI result committed", err)
	}
	after, err := r.GetAnalysisAIRevision(ctx, "t", job.ID)
	if err != nil || after.Status != model.AnalysisRunning || len(after.Interpretation) > 0 {
		t.Fatal("expired inference result persisted", after, err)
	}
}
