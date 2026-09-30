package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func TestMonitoringAISQLSchemaReplicasAndUnknownRestart(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if err := r.MigrateAnalytics(ctx); err != nil {
		t.Fatal(err)
	}
	service, actor, facts := sqlAIServiceKind(t, r, analytics.KindMonitoring)
	queued, err := service.Create(ctx, actor, facts.Kind, facts.ID, analytics.CreateAIRequest{ExpectedVersion: facts.Version, IdempotencyKey: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ClaimAnalysisAIRevision(ctx, "wrong-workflow", time.Minute, 4*time.Minute, []string{analytics.WorkflowDataQuality}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("another workflow claimed monitoring job", err)
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
			job, err := target.ClaimAnalysisAIRevision(ctx, "replica", time.Minute, 4*time.Minute, []string{analytics.WorkflowMonitoring})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				claims = append(claims, job)
			} else if !errors.Is(err, model.ErrNotFound) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(claims) != 1 || claims[0].ID != queued.ID {
		t.Fatal(claims)
	}
	job := claims[0]
	input, err := service.BuildInput(ctx, job)
	if err != nil || input.Coverage.TotalOutputs != 3 || input.Coverage.OutputCount != 3 || input.Coverage.Truncated {
		t.Fatal(input, err)
	}
	current, _ := r.GetAnalysisAIRevision(ctx, actor.TenantID, job.ID)
	answer := `{"summary":"x","observedWeaknesses":[{"text":"未知区间须核实","factIds":["interval"],"deviceIds":["d1"]}],"prioritizedChecks":[],"dependencyObservations":[{"text":"仅包括可见接入成员","factIds":["group"]}],"limitations":[{"text":"起点未知","factIds":["snapshot/summary"]}]}`
	result, err := analytics.DecodeAIWorkflowResult(analytics.WorkflowMonitoring, answer, current.SentFactIDs, current.DeviceIDs, input.Coverage)
	if err != nil {
		t.Fatal(err)
	}
	done, err := other.FinishAnalysisAIRevision(ctx, "t", job.ID, job.LeaseToken, result, "mock-harness", "")
	if err != nil || done.Status != model.AnalysisSucceeded || strings.Contains(string(done.Interpretation), "interpretations") {
		t.Fatal(done, err)
	}
	var decoded map[string]json.RawMessage
	if err = json.Unmarshal(done.Interpretation, &decoded); err != nil || len(decoded) != 6 || decoded["prioritizedChecks"] == nil {
		t.Fatal(decoded, err)
	}
	newJob, err := service.Create(ctx, actor, facts.Kind, facts.ID, analytics.CreateAIRequest{ExpectedVersion: facts.Version, IdempotencyKey: "retry", Reinterpret: true})
	if err != nil || newJob.ID == job.ID {
		t.Fatal(newJob, err)
	}
	claimed, err := r.ClaimAnalysisAIRevision(ctx, "before-restart", time.Minute, 4*time.Minute, []string{analytics.WorkflowMonitoring})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, `UPDATE analysis_document SET body=jsonb_set(body,'{leaseExpiresAt}','1') WHERE tenant_id='t' AND kind='ai' AND id=$1`, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = other.ClaimAnalysisAIRevision(ctx, "after-restart", time.Minute, 4*time.Minute, []string{analytics.WorkflowMonitoring}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("unknown call was automatically repeated", err)
	}
	failed, err := other.GetAnalysisAIRevision(ctx, actor.TenantID, newJob.ID)
	if err != nil || failed.Status != model.AnalysisFailed || failed.LeaseToken <= claimed.LeaseToken || len(failed.Interpretation) > 0 {
		t.Fatal(failed, err)
	}
	unchanged, _ := other.GetAnalysisRun(ctx, actor.TenantID, facts.ID)
	if unchanged.Version != facts.Version || unchanged.Status != model.AnalysisPartial {
		t.Fatal("AI changed monitoring facts", unchanged)
	}
}
