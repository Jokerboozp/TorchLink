package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
)

func TestResponseSQLImmutableEvaluationConcurrentActionsAndReceiptRestart(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "response-test", ID: "d1", ProductID: "p1", Name: "隔离设备", AccessKey: "fixture-response-key"}); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, repo.pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	other := &Repository{pool: pool}
	actor := analytics.Actor{TenantID: "response-test", Username: "operator", AllDevices: true, Permissions: []string{"*"}, AccessVersion: "fixture"}
	makeService := func(r *Repository) *response.Service {
		base := analytics.NewService(r, config.AnalyticsConfig{Workers: 1, Poll: time.Millisecond, Lease: time.Second, BatchSize: 1}, func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }, func(ctx context.Context, tenant, id string) error {
			_, err := r.GetManagedDevice(ctx, tenant, id)
			return err
		})
		svc := response.NewService(base, r)
		svc.Catalog = r
		svc.ValidateStaff = func(context.Context, string, string, []string) error { return nil }
		svc.ResolveEvidence = svc.BusinessEvidence
		if err := svc.Register(); err != nil {
			t.Fatal(err)
		}
		return svc
	}
	s1, s2 := makeService(repo), makeService(other)
	request := func(id, key string, version int64, body any) response.RevisionRequest {
		data, _ := json.Marshal(body)
		return response.RevisionRequest{ResourceID: id, ExpectedVersion: version, DeviceIDs: []string{"d1"}, IdempotencyKey: key, Body: data}
	}
	procedure := model.ResponseProcedure{Name: "隔离流程", Scenario: "验证场景", Steps: []model.ResponseStep{{ID: "arrive", Name: "现场到达", Role: "检查员", Required: true, RequiredEvidence: []string{"MANUAL_RECORD"}, ClockStart: "RUN_START"}}}
	p, err := s1.SaveProcedure(ctx, actor, request("p", "save-procedure", 0, procedure))
	if err != nil {
		t.Fatal(err)
	}
	p, err = s1.PublishProcedure(ctx, actor, "p", response.ActionRequest{ExpectedVersion: p.Version, IdempotencyKey: "publish-procedure"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	plan := response.PlanBody{Name: "隔离演练", ProcedureRevisionID: p.ID, People: []model.ResponsePerson{{Username: "off-duty", Role: "检查员"}}, PlannedStart: now, PlannedEnd: now + 60000}
	exec, err := s1.CreateExecution(ctx, actor, request("drill", "create-drill", 0, plan), "DRILL")
	if err != nil {
		t.Fatal(err)
	}
	exec, err = s1.ExecutionAction(ctx, actor, "drill", response.ActionRequest{ExpectedVersion: exec.Version, IdempotencyKey: "publish-drill", Action: "PUBLISH"})
	if err != nil {
		t.Fatal(err)
	}
	startRequest := response.ActionRequest{ExpectedVersion: exec.Version, IdempotencyKey: "start-drill", Action: "START"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []model.AnalysisConfigRevision{}
	failures := []error{}
	for _, svc := range []*response.Service{s1, s2} {
		wg.Add(1)
		go func(svc *response.Service) {
			defer wg.Done()
			v, e := svc.ExecutionAction(ctx, actor, "drill", startRequest)
			mu.Lock()
			defer mu.Unlock()
			results = append(results, v)
			if e != nil {
				failures = append(failures, e)
			}
		}(svc)
	}
	wg.Wait()
	if len(failures) != 0 || results[0].ID != results[1].ID {
		t.Fatalf("same-key replica retry diverged %+v %v", results, failures)
	}
	exec = results[0]
	var running model.ResponseExecution
	_ = json.Unmarshal(exec.Body, &running)
	time.Sleep(3 * time.Millisecond)
	exec, err = s2.ExecutionAction(ctx, actor, "drill", response.ActionRequest{ExpectedVersion: exec.Version, IdempotencyKey: "end", Action: "END"})
	if err != nil {
		t.Fatal(err)
	}
	var ended model.ResponseExecution
	_ = json.Unmarshal(exec.Body, &ended)
	parameters, _ := json.Marshal(response.EvaluationParameters{ExecutionRevisionID: exec.ID})
	q := analytics.CreateRequest{DeviceIDs: []string{"d1"}, Start: ended.StartedAt, End: ended.EndedAt, IdempotencyKey: "evaluate-fixed", Parameters: parameters}
	if err = s2.ValidateCreate(ctx, actor, &q); err != nil {
		t.Fatal(err)
	}
	run, err := s2.Analysis.Create(ctx, actor, analytics.KindResponse, response.AlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s2.Analysis.RunWorkers(workerCtx, "response-fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err = s1.Analysis.Get(ctx, actor, analytics.KindResponse, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.Status != model.AnalysisSucceeded {
		t.Fatalf("evaluation failed %+v", run)
	}
	snap, err := s1.Analysis.Snapshot(ctx, actor, analytics.KindResponse, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	confirmation := response.ConfirmRequest{ExpectedVersion: exec.Version, ExpectedRunVersion: run.Version, IdempotencyKey: "confirm", FactsHash: snap.FactsHash, Conclusion: "缺现场记录只表示证据未登记"}
	exec, err = s1.ConfirmReview(ctx, actor, "drill", run.ID, confirmation)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s2.AppendMilestone(ctx, actor, "drill", response.MilestoneRequest{ExpectedVersion: exec.Version, IdempotencyKey: "late", StepID: "arrive", OccurredAt: ended.EndedAt - 1, Executor: "off-duty", Status: "CONFIRMED", Explanation: "事后补登记", Evidence: []model.ResponseEvidenceReference{{Kind: "MANUAL_RECORD", DeviceID: "d1", Description: "补登记现场依据"}}})
	if err != nil {
		t.Fatal(err)
	}
	old, err := s2.Analysis.Snapshot(ctx, actor, analytics.KindResponse, run.ID)
	if err != nil || old.FactsHash != snap.FactsHash || string(old.Statistics) != string(snap.Statistics) {
		t.Fatal("late evidence rewrote old evaluation", err)
	}
	restarted := makeService(other)
	retried, err := restarted.ExecutionAction(ctx, actor, "drill", startRequest)
	if err != nil || retried.ID != results[0].ID {
		t.Fatalf("restart lost original receipt %+v %v", retried, err)
	}
	startRequest.Action = "CANCEL"
	startRequest.Reason = "same-key altered"
	if _, err = restarted.ExecutionAction(ctx, actor, "drill", startRequest); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatalf("changed retry accepted %v", err)
	}
	var alarms int
	if err = repo.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_record WHERE tenant_id='response-test'`).Scan(&alarms); err != nil || alarms != 0 {
		t.Fatal("review wrote production", alarms, err)
	}
}
