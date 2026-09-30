package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/aitest"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/duty"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type dutyControlledHarness struct {
	Calls   atomic.Int32
	Started chan ports.AIWorkflowRequest
	Gate    chan struct{}
}

func (h *dutyControlledHarness) ListWorkflows(context.Context) ([]ports.AIWorkflowPlugin, error) {
	return nil, nil
}
func (h *dutyControlledHarness) Health(context.Context) error { return nil }
func (h *dutyControlledHarness) StreamChat(ctx context.Context, req ports.AIWorkflowRequest, _ func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	h.Calls.Add(1)
	if h.Started != nil {
		select {
		case h.Started <- req:
		case <-ctx.Done():
			return ports.AIWorkflowResult{}, ctx.Err()
		}
	}
	if h.Gate != nil {
		select {
		case <-h.Gate:
		case <-ctx.Done():
			return ports.AIWorkflowResult{}, ctx.Err()
		}
	}
	var input core.DutyAIInput
	idx := strings.LastIndex(req.Question, "\n{")
	if idx < 0 {
		return ports.AIWorkflowResult{}, errors.New("missing frozen JSON input")
	}
	if e := json.NewDecoder(strings.NewReader(req.Question[idx+1:])).Decode(&input); e != nil {
		return ports.AIWorkflowResult{}, e
	}
	result := model.DutyAIResult{Summary: "测试整理"}
	if len(input.Evidence) > 0 {
		result.Highlights = []model.DutyAIStatement{{Text: "请核对本班记录与接续事项", EvidenceIDs: []string{input.Evidence[0].ID}}}
	}
	raw, _ := json.Marshal(result)
	return ports.AIWorkflowResult{RunID: req.RunID, WorkflowID: req.WorkflowID, Model: "duty-test-model", Answer: string(raw)}, nil
}
func (f *dutyHTTPFixture) aiJob() map[string]any {
	f.t.Helper()
	h := f.command("POST", "/api/v1/duty/handovers", f.day, 0, map[string]any{"runId": f.id(f.dayRun), "nextRosterId": f.id(f.nightRoster), "humanNotes": "人工交接说明保持原文"}, 200)
	return f.command("POST", "/api/v1/duty/handovers/"+f.id(h)+"/start-ai", f.day, f.version(h), map[string]any{"useKnowledge": false}, 200)
}
func (f *dutyHTTPFixture) waitJob(id string, statuses ...string) model.DutyDocument {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var doc model.DutyDocument
		e := f.repo.DutyRead(context.Background(), f.tenant, func(tx ports.DutyTx) error { v, e := tx.Get(model.DutyAIJobKind, id); doc = v; return e })
		if e != nil {
			f.t.Fatal(e)
		}
		j, e := model.DutyBody[model.DutyAIJob](doc)
		if e != nil {
			f.t.Fatal(e)
		}
		for _, status := range statuses {
			if j.Status == status {
				return doc
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatalf("job %s did not reach %v", id, statuses)
	return model.DutyDocument{}
}
func (f *dutyHTTPFixture) startWorkers(h *dutyControlledHarness) (context.CancelFunc, <-chan struct{}) {
	f.t.Helper()
	if f.api.engine.AIWorkflows == nil {
		f.api.engine.AIWorkflows = h
		f.api.engine.HarnessTokens = aitest.Tokens()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.api.RunDutyWorkers(ctx) }()
	f.t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			f.t.Error("worker scan did not stop")
		}
	})
	return cancel, done
}
func waitDutyHarness(t *testing.T, h *dutyControlledHarness) ports.AIWorkflowRequest {
	t.Helper()
	select {
	case req := <-h.Started:
		return req
	case <-time.After(10 * time.Second):
		t.Fatal("Harness workflow did not start")
	}
	return ports.AIWorkflowRequest{}
}

func TestDutyAIWorkersCoordinateReplicasAndPreserveHumanRevision(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_worker_success", "duty-http-test-password")
	job := f.aiJob()
	h := &dutyControlledHarness{Started: make(chan ports.AIWorkflowRequest, 4), Gate: make(chan struct{})}
	cancel1, _ := f.startWorkers(h)
	defer cancel1()
	req := waitDutyHarness(t, h)
	cancel2, _ := f.startWorkers(h)
	defer cancel2()
	claims, e := aitest.Claims(req)
	if e != nil {
		t.Fatal(e)
	}
	if req.WorkflowID != core.WorkflowDutyHandover || claims.DutyJobID != f.id(job) || claims.DutyRevisionID == "" || claims.DutyLeaseOwner == "" || !claims.HasScope(auth.ScopeQueryDutySnapshot) || claims.HasScope(auth.ScopeQueryKnowledgeBase) {
		t.Fatal("workflow identity scope binding incorrect", req.WorkflowID, claims)
	}
	close(h.Gate)
	done := f.waitJob(f.id(job), "SUCCEEDED", "FAILED")
	j, _ := model.DutyBody[model.DutyAIJob](done)
	if j.Status != "SUCCEEDED" {
		t.Fatal("worker failed", j.Error)
	}
	if h.Calls.Load() != 1 {
		t.Fatal("two replicas generated same version", h.Calls.Load())
	}
	handover := f.req("GET", "/api/v1/duty/handovers/"+j.HandoverID, f.day, nil, 200)
	rev := f.req("GET", "/api/v1/duty/revisions/"+dutyHTTPBody(handover)["currentRevisionId"].(string), f.day, nil, 200)
	if dutyHTTPBody(rev)["humanNotes"] != "人工交接说明保持原文" || dutyHTTPBody(rev)["ai"] == nil {
		t.Fatal("AI replaced human notes", rev)
	}
	original := f.req("GET", "/api/v1/duty/revisions/"+j.RevisionID, f.day, nil, 200)
	if dutyHTTPBody(original)["ai"] != nil {
		t.Fatal("worker changed frozen source")
	}
}

func TestDutyAIWorkerStopsPersistedJobWithoutApplyingOutput(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_worker_stop", "duty-http-test-password")
	job := f.aiJob()
	h := &dutyControlledHarness{Started: make(chan ports.AIWorkflowRequest, 1), Gate: make(chan struct{})}
	cancel, _ := f.startWorkers(h)
	defer cancel()
	waitDutyHarness(t, h)
	running := f.waitJob(f.id(job), "RUNNING")
	f.command("POST", "/api/v1/duty/ai-jobs/"+f.id(job)+"/stop", f.day, running.Version, nil, 200)
	done := f.waitJob(f.id(job), "CANCELLED")
	j, _ := model.DutyBody[model.DutyAIJob](done)
	handover := f.req("GET", "/api/v1/duty/handovers/"+j.HandoverID, f.day, nil, 200)
	if dutyHTTPBody(handover)["currentRevisionId"] != j.RevisionID {
		t.Fatal("stopped job attached output")
	}
}

func TestDutyAIWorkerRechecksPermissionBeforeApplyingResult(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_worker_revoke", "duty-http-test-password")
	job := f.aiJob()
	h := &dutyControlledHarness{Started: make(chan ports.AIWorkflowRequest, 1), Gate: make(chan struct{})}
	cancel, _ := f.startWorkers(h)
	defer cancel()
	waitDutyHarness(t, h)
	state, e := f.repo.LoadAccessState(context.Background(), f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	for i := range state.Users {
		if state.Users[i].Username == "day" {
			state.Users[i].DeviceIDs = []string{}
			state.Users[i].DeviceScope = "selected"
		}
	}
	if ok, e := f.repo.SaveAccessState(context.Background(), f.tenant, state); e != nil || !ok {
		t.Fatal(ok, e)
	}
	close(h.Gate)
	done := f.waitJob(f.id(job), "FAILED")
	j, _ := model.DutyBody[model.DutyAIJob](done)
	handover := f.req("GET", "/api/v1/duty/handovers/"+j.HandoverID, f.admin, nil, 200)
	if dutyHTTPBody(handover)["currentRevisionId"] != j.RevisionID || j.Result != nil {
		t.Fatal("revoked job attached output", j)
	}
}

func TestDutyAIWorkerRecoversLeaseAfterProcessShutdown(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_worker_restart", "duty-http-test-password")
	job := f.aiJob()
	h := &dutyControlledHarness{Started: make(chan ports.AIWorkflowRequest, 4), Gate: make(chan struct{})}
	cancel, scanDone := f.startWorkers(h)
	waitDutyHarness(t, h)
	running := f.waitJob(f.id(job), "RUNNING")
	cancel()
	select {
	case <-scanDone:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not stop scan")
	}
	if e := f.repo.DutyTransaction(context.Background(), f.tenant, func(tx ports.DutyTx) error {
		doc, e := tx.Get(model.DutyAIJobKind, f.id(job))
		if e != nil {
			return e
		}
		j, e := model.DutyBody[model.DutyAIJob](doc)
		if e != nil {
			return e
		}
		if j.Status != "RUNNING" {
			return errors.New("shutdown finalized a recoverable generation")
		}
		j.LeaseUntil = time.Now().Add(-time.Second).UnixMilli()
		raw, _ := json.Marshal(j)
		doc.Body = raw
		_, e = tx.Put(doc, doc.Version)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	close(h.Gate)
	restart, _ := f.startWorkers(h)
	defer restart()
	waitDutyHarness(t, h)
	done := f.waitJob(f.id(job), "SUCCEEDED", "FAILED")
	j, _ := model.DutyBody[model.DutyAIJob](done)
	old, _ := model.DutyBody[model.DutyAIJob](running)
	if j.Status != "SUCCEEDED" || j.Attempt != old.Attempt+1 || j.LeaseOwner == old.LeaseOwner {
		t.Fatal("generation lease not reclaimed safely", j)
	}
	if h.Calls.Load() != 2 {
		t.Fatal("generation restarted unexpected times", h.Calls.Load())
	}
}

func TestDutyAIWorkerCannotAdoptReplacementLeaseFromStaleClaim(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_worker_fencing", "duty-http-test-password")
	job := f.aiJob()
	h := &dutyControlledHarness{}
	f.api.engine.AIWorkflows, f.api.engine.HarnessTokens = h, aitest.Tokens()
	worker := duty.New(f.repo, nil)
	oldClaim, claimed, err := worker.ClaimJob(context.Background(), f.tenant, f.id(job), "expired-owner", time.Minute)
	if err != nil || !claimed {
		t.Fatal("initial claim failed", claimed, err)
	}
	if err = f.repo.DutyTransaction(context.Background(), f.tenant, func(tx ports.DutyTx) error {
		doc, err := tx.Get(model.DutyAIJobKind, f.id(job))
		if err != nil {
			return err
		}
		body, err := model.DutyBody[model.DutyAIJob](doc)
		if err != nil {
			return err
		}
		body.LeaseUntil = time.Now().Add(-time.Second).UnixMilli()
		doc.Body, err = json.Marshal(body)
		if err != nil {
			return err
		}
		_, err = tx.Put(doc, doc.Version)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	newClaim, claimed, err := worker.ClaimJob(context.Background(), f.tenant, f.id(job), "replacement-owner", time.Minute)
	if err != nil || !claimed {
		t.Fatal("replacement claim failed", claimed, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	f.api.runDutyAIJob(ctx, f.tenant, oldClaim)
	if h.Calls.Load() != 0 {
		t.Fatal("stale claimant adopted the replacement lease and called the model")
	}
	unchanged := f.waitJob(f.id(job), "RUNNING")
	current, err := model.DutyBody[model.DutyAIJob](unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Version != newClaim.Version || current.LeaseOwner != "replacement-owner" || current.Result != nil || current.Error != "" {
		t.Fatal("stale claimant changed the replacement owner's job", current)
	}
	handover := f.req("GET", "/api/v1/duty/handovers/"+current.HandoverID, f.day, nil, 200)
	if dutyHTTPBody(handover)["currentRevisionId"] != current.RevisionID {
		t.Fatal("stale claimant attached a new handover revision")
	}
	// The exact claimed document for the current owner still works normally.
	f.api.runDutyAIJob(ctx, f.tenant, newClaim)
	finished := f.waitJob(f.id(job), "SUCCEEDED", "FAILED")
	result, err := model.DutyBody[model.DutyAIJob](finished)
	if err != nil || result.Status != "SUCCEEDED" || result.LeaseOwner != "replacement-owner" || h.Calls.Load() != 1 {
		t.Fatal("replacement owner could not finish its own claimed generation", result, err)
	}
}
