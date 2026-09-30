package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func aiFixture(t *testing.T) (*Store, *AIService, Actor, model.AnalysisRun) {
	return aiFixtureKind(t, KindDataQuality)
}
func aiFixtureKind(t *testing.T, kind string) (*Store, *AIService, Actor, model.AnalysisRun) {
	return aiFixtureKindOptions(t, kind, false)
}
func aiFixtureKindOptions(t *testing.T, kind string, finance bool) (*Store, *AIService, Actor, model.AnalysisRun) {
	t.Helper()
	ctx := context.Background()
	store := NewMemoryStore()
	actor := Actor{TenantID: "t", Username: "operator", AccessVersion: "scope1", AllDevices: true, Permissions: []string{"*"}}
	facts := NewService(store, config.AnalyticsConfig{}, func(_ context.Context, a Actor) (Actor, error) {
		a.AllDevices = actor.AllDevices
		a.Permissions = actor.Permissions
		a.AccessVersion = actor.AccessVersion
		a.DeviceIDs = actor.DeviceIDs
		return a, nil
	}, func(context.Context, string, string) error { return nil })
	request := storedRun("one", kind)
	request.Creator = actor.Username
	if kind == KindResponse || kind == KindMaintenance || kind == KindInvestment {
		body := json.RawMessage(`{"record":"fixed"}`)
		if kind == KindInvestment {
			body, _ = json.Marshal(map[string]any{"record": "fixed", "useFinance": finance})
		}
		sourceKind := "RESPONSE_EXECUTION"
		if kind == KindMaintenance {
			sourceKind = "MAINTENANCE_RECORD"
		}
		if kind == KindInvestment {
			sourceKind = "INVESTMENT_SCENARIO"
		}
		source, err := store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: "source", TenantID: "t", Kind: sourceKind, ResourceID: "business-one", Scope: "SHARED", Creator: actor.Username, DeviceIDs: request.DeviceIDs, Body: body}, 0)
		if err != nil {
			t.Fatal(err)
		}
		request.ConfigurationVersion = source.Hash
		p := map[string]any{"useFinance": finance}
		if kind == KindResponse {
			p = map[string]any{"executionRevisionId": source.ID, "cutoff": 2000}
		}
		if kind == KindMaintenance {
			p["observation"] = map[string]any{"interventionRevisionId": source.ID}
		}
		if kind == KindInvestment {
			p["scenarioRevisionId"] = source.ID
		}
		request.Parameters, _ = json.Marshal(p)
		if finance {
			request.RequiredPermissions = []string{FinanceReadOperation}
		}
	}
	if kind == KindRuleLab {
		body, _ := json.Marshal(model.RuleLabExperiment{DatasetID: "dataset", CandidateRuleID: "policy", Candidate: model.AlarmRule{ID: "policy", TenantID: "t", ProductID: "p", Enabled: false}, CandidateEnabled: true, Hypothesis: "手算固定实验"})
		if _, e := store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: "experiment", TenantID: "t", Kind: model.RuleLabExperimentKind, ResourceID: "experiment-policy", Scope: "PERSONAL", Creator: actor.Username, DeviceIDs: request.DeviceIDs, Body: body}, 0); e != nil {
			t.Fatal(e)
		}
		request.Parameters = json.RawMessage(`{"phase":"EXPERIMENT","datasetId":"dataset","experimentRevisionId":"experiment"}`)
	}

	if _, err := store.CreateAnalysisRun(ctx, request, 100); err != nil {
		t.Fatal(err)
	}
	run, err := store.ClaimAnalysisRun(ctx, "facts", time.Minute, []string{kind})
	if err != nil {
		t.Fatal(err)
	}
	outputs := []model.AnalysisOutput{}
	for i := 0; i < 35; i++ {
		kind := "findings"
		if i >= 25 {
			kind = "metrics"
		}
		outputs = append(outputs, model.AnalysisOutput{ID: fmt.Sprintf("fact%02d", i), DeviceID: "d1", Kind: kind, Body: json.RawMessage(`{"unknown":true}`)})
	}
	category := "devices"
	if kind == KindMonitoring {
		category = "monitoring-evidence"
		outputs = append(outputs, model.AnalysisOutput{ID: "interval1", DeviceID: "d1", Kind: "intervals", Body: json.RawMessage(`{"start":1000,"end":2000,"knownUnavailableMs":500,"unknownMs":500}`)}, model.AnalysisOutput{ID: "group1", Kind: "dependency-groups", Body: json.RawMessage(`{"visibleMembers":["d1","d2"],"concentration":1}`)}, model.AnalysisOutput{ID: "private", Kind: "input-manifest", Body: json.RawMessage(`{"internalMembers":["private-source"]}`)})
	}
	if kind == KindRuleLab {
		category = "rule-lab-evidence"
		outputs = append(outputs, model.AnalysisOutput{ID: "difference", Kind: "diffs", DeviceID: "d1", Body: json.RawMessage(`{"kind":"CANDIDATE_ONLY"}`)}, model.AnalysisOutput{ID: "cycle", Kind: "outcomes", DeviceID: "d1", Body: json.RawMessage(`{"branch":"CANDIDATE"}`)}, model.AnalysisOutput{ID: "label", Kind: "labels", DeviceID: "d1", Body: json.RawMessage(`{"status":"DRAFT"}`)}, model.AnalysisOutput{ID: "private", Kind: "comparison-stage", Body: json.RawMessage(`{"private":true}`)})
	}
	if kind == KindResponse {
		category = "response"
	}
	if kind == KindMaintenance || kind == KindInvestment {
		category = "maintenance-evidence"
		public := []string{"observations", "change-metrics"}
		if kind == KindInvestment {
			public = []string{"investment-priorities", "budget-lines"}
		}
		for _, collection := range public {
			outputs = append(outputs, model.AnalysisOutput{ID: collection + "-one", Kind: collection, DeviceID: "d1", Body: json.RawMessage(`{"known":false}`)})
		}
		outputs = append(outputs, model.AnalysisOutput{ID: "private", Kind: "calculation-stage", Body: json.RawMessage(`{"private":true}`)})
	}
	run, err = store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Outputs: outputs, Evidence: []model.AnalysisEvidence{{ID: "proof", SourceKind: "raw", SourceID: "raw1", DeviceID: "d1", Summary: json.RawMessage(`{"parse":"unknown"}`), PermissionCategory: category, OriginalAvailability: "EXPIRED"}}, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"missing":4,"unknown":2}`), Limitations: []string{"起点状态未知"}}})
	if err != nil {
		t.Fatal(err)
	}
	return store, NewAIService(facts, nil), actor, run
}
func createAI(t *testing.T, s *AIService, a Actor, run model.AnalysisRun, key string) model.AnalysisAIRevision {
	t.Helper()
	job, err := s.Create(context.Background(), a, run.Kind, run.ID, CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return job
}
func claimAI(t *testing.T, store *Store, s *AIService) model.AnalysisAIRevision {
	t.Helper()
	job, err := store.ClaimAnalysisAIRevision(context.Background(), "worker", time.Minute, 4*time.Minute, s.workflowIDs())
	if err != nil {
		t.Fatal(err)
	}
	return job
}
func validAIAnswer(id string) string {
	return fmt.Sprintf(`{"summary":"模型不能在summary添加结论","interpretations":[{"text":"存在待核实的数据缺口","factIds":[%q],"deviceIds":["d1"]}],"suggestedVerification":[],"limitations":[]}`, id)
}

func TestAnalysisAIBoundedInputReferencesAndReinterpret(t *testing.T) {
	ctx := context.Background()
	store, s, a, run := aiFixture(t)
	queued := createAI(t, s, a, run, "first")
	again := createAI(t, s, a, run, "second")
	if queued.ID != again.ID {
		t.Fatal("duplicate click created job")
	}
	job := claimAI(t, store, s)
	input, err := s.BuildInput(ctx, job)
	if err != nil || !input.Coverage.SummaryProvided || input.Coverage.OutputCount != 30 || input.Coverage.TotalOutputs != 35 || !input.Coverage.Truncated {
		t.Fatal(input, err)
	}
	current, err := store.GetAnalysisAIRevision(ctx, "t", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeAnalysisAIResult(validAIAnswer("fact24"), current.SentFactIDs, job.DeviceIDs, input.Coverage); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("unprovided fact reference accepted", err)
	}
	page, err := s.ReadBoundAnalysisFacts(ctx, AIIdentity(job), "findings", 5, 20)
	if err != nil || len(page.Outputs) != 5 || page.Total != 25 || page.NextOffset != 25 || page.HasMore || !page.Complete {
		t.Fatal(page, err)
	}
	current, _ = store.GetAnalysisAIRevision(ctx, "t", job.ID)
	coverage, err := s.coverage(ctx, current)
	if err != nil || coverage.OutputCount != 35 || coverage.Truncated {
		t.Fatal(coverage, err)
	}
	result, err := DecodeAnalysisAIResult(validAIAnswer("fact24"), current.SentFactIDs, job.DeviceIDs, coverage)
	if err != nil || strings.Contains(result.Summary, "模型不能") {
		t.Fatal(result, err)
	}
	done, err := store.FinishAnalysisAIRevision(ctx, "t", job.ID, job.LeaseToken, result, "test-model", "")
	if err != nil || done.Status != model.AnalysisSucceeded {
		t.Fatal(done, err)
	}
	defaultJob := createAI(t, s, a, run, "third")
	if defaultJob.ID != job.ID {
		t.Fatal("default interpretation repeated model")
	}
	newJob, err := s.Create(ctx, a, run.Kind, run.ID, CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "explicit", Reinterpret: true})
	if err != nil || newJob.ID == done.ID {
		t.Fatal("explicit reinterpret failed", newJob, err)
	}
	alias, err := s.Create(ctx, a, run.Kind, run.ID, CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "explicit-alias", Reinterpret: true})
	if err != nil || alias.ID != newJob.ID {
		t.Fatal(alias, err)
	}
	if after, err := store.GetAnalysisRun(ctx, "t", run.ID); err != nil || after.Version != run.Version || after.Status != model.AnalysisPartial {
		t.Fatal("AI changed facts", after, err)
	}
}

func TestAnalysisAIWorkerSuccessAndLateResponseFences(t *testing.T) {
	for _, tc := range []struct{ name, status string }{{"success", model.AnalysisSucceeded}, {"stop", model.AnalysisCancelled}, {"revoke", model.AnalysisCancelled}, {"wrong-object", model.AnalysisFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, service, actor, facts := aiFixture(t)
			var revoked atomic.Bool
			service.Facts.Resolve = func(context.Context, Actor) (Actor, error) {
				current := actor
				if revoked.Load() {
					current.AccessVersion = "revoked-before-response"
				}
				return current, nil
			}
			createAI(t, service, actor, facts, "job")
			job := claimAI(t, store, service)
			service.Runner = func(ctx context.Context, job model.AnalysisAIRevision, input model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
				identity, ok := ports.AIRunIdentityFrom(ctx)
				if !ok || identity.AnalysisLeaseToken != job.LeaseToken || input.Coverage.OutputCount != 30 {
					return ports.AIWorkflowResult{}, errors.New("unbound runner input")
				}
				answer := validAIAnswer("fact00")
				switch tc.name {
				case "success":
					if _, err := service.ReadBoundAnalysisFacts(ctx, identity, "findings", 5, 20); err != nil {
						return ports.AIWorkflowResult{}, err
					}
				case "stop":
					current, err := store.GetAnalysisAIRevision(ctx, actor.TenantID, job.ID)
					if err == nil {
						_, err = store.StopAnalysisAIRevision(ctx, actor.TenantID, job.ID, current.Version)
					}
					if err != nil {
						return ports.AIWorkflowResult{}, err
					}
				case "revoke":
					revoked.Store(true)
				case "wrong-object":
					answer = strings.ReplaceAll(answer, `"deviceIds":["d1"]`, `"deviceIds":["d2"]`)
				}
				return ports.AIWorkflowResult{RunID: job.HarnessRunID, Answer: answer, Model: "mock-harness"}, nil
			}
			service.executeAI(ctx, job, slog.New(slog.NewTextHandler(io.Discard, nil)))
			after, err := store.GetAnalysisAIRevision(ctx, actor.TenantID, job.ID)
			if err != nil || after.Status != tc.status {
				t.Fatal(after, err)
			}
			if tc.status == model.AnalysisSucceeded {
				var result model.AnalysisAIResult
				if err = json.Unmarshal(after.Interpretation, &result); err != nil || result.Coverage.OutputCount != 35 || result.Coverage.Truncated || !strings.Contains(result.Summary, "35/35") {
					t.Fatal(result, err)
				}
			} else if len(after.Interpretation) > 0 || len(after.FactIDs) > 0 {
				t.Fatal("late or invalid response became a successful interpretation", after)
			}
			unchanged, err := store.GetAnalysisRun(ctx, actor.TenantID, facts.ID)
			if err != nil || unchanged.Version != facts.Version || unchanged.Status != model.AnalysisPartial {
				t.Fatal("AI changed deterministic facts", unchanged, err)
			}
		})
	}
}

func TestAnalysisAIWorkerFailuresDoNotChangeFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		runner AIRunner
	}{{"no-key", func(context.Context, model.AnalysisAIRevision, model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
		return ports.AIWorkflowResult{}, errors.New("API_KEY_REQUIRED")
	}}, {"429", func(context.Context, model.AnalysisAIRevision, model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
		return ports.AIWorkflowResult{}, ports.ErrAIWorkflowBusy
	}}, {"invalid-structure", func(_ context.Context, j model.AnalysisAIRevision, _ model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
		return ports.AIWorkflowResult{RunID: j.HarnessRunID, Answer: `{"summary":"bad","interpretations":[{"text":"false","factIds":["missing"]}],"suggestedVerification":[],"limitations":[]}`}, nil
	}}, {"timeout", func(ctx context.Context, _ model.AnalysisAIRevision, _ model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
		<-ctx.Done()
		return ports.AIWorkflowResult{}, ctx.Err()
	}}, {"panic", func(context.Context, model.AnalysisAIRevision, model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
		panic("unexpected")
	}}} {
		t.Run(tc.name, func(t *testing.T) {
			store, s, a, run := aiFixture(t)
			s.Runner = tc.runner
			createAI(t, s, a, run, "job")
			job := claimAI(t, store, s)
			if tc.name == "timeout" {
				job.Deadline = time.Now().Add(15 * time.Millisecond).UnixMilli()
			}
			s.executeAI(context.Background(), job, slog.New(slog.NewTextHandler(io.Discard, nil)))
			after, err := store.GetAnalysisAIRevision(context.Background(), "t", job.ID)
			if err != nil || after.Status != model.AnalysisFailed || len(after.Interpretation) > 0 {
				t.Fatal(after, err)
			}
			facts, err := store.GetAnalysisRun(context.Background(), "t", run.ID)
			if err != nil || facts.Status != model.AnalysisPartial || facts.Version != run.Version {
				t.Fatal("AI failure modified facts", facts, err)
			}
		})
	}
}

func TestAnalysisAIStopRevocationScopeAndProof(t *testing.T) {
	ctx := context.Background()
	store, s, a, run := aiFixture(t)
	createAI(t, s, a, run, "job")
	job := claimAI(t, store, s)
	identity := AIIdentity(job)
	wrong := identity
	wrong.AnalysisSnapshotVersion++
	if _, err := s.ReadBoundAnalysisFacts(ctx, wrong, "summary", 20, 0); !errors.Is(err, ErrForbidden) {
		t.Fatal("wrong version read", err)
	}
	wrong = identity
	wrong.TenantID = "other"
	if _, err := s.ReadBoundAnalysisFacts(ctx, wrong, "summary", 20, 0); !errors.Is(err, ErrForbidden) {
		t.Fatal("cross tenant read", err)
	}
	s.Facts.Resolve = func(context.Context, Actor) (Actor, error) {
		changed := a
		changed.AccessVersion = "revoked"
		changed.AllDevices = false
		changed.DeviceIDs = []string{"d1"}
		return changed, nil
	}
	if _, err := s.ReadBoundAnalysisFacts(ctx, identity, "summary", 20, 0); !errors.Is(err, ErrForbidden) {
		t.Fatal("revoked snapshot was cropped and disclosed", err)
	}
	s.cancelJob(ctx, job)
	if _, err := store.RecordAnalysisAIFacts(ctx, "t", job.ID, job.LeaseToken, []string{"fact00"}); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("revoked worker wrote", err)
	}
	if _, err := s.Get(ctx, a, run.Kind, run.ID, job.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("revoked historical AI body read", err)
	}
}

func TestAnalysisAIConcurrentClaimAndUnknownRestart(t *testing.T) {
	ctx := context.Background()
	store, s, a, run := aiFixture(t)
	createAI(t, s, a, run, "job")
	var mu sync.Mutex
	claimed := []model.AnalysisAIRevision{}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job, err := store.ClaimAnalysisAIRevision(ctx, fmt.Sprint(i), time.Minute, 4*time.Minute, s.workflowIDs())
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				claimed = append(claimed, job)
			} else if !errors.Is(err, model.ErrNotFound) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if len(claimed) != 1 {
		t.Fatal(claimed)
	}
	job := claimed[0]
	backend := store.backend.(*memoryBackend)
	backend.mu.Lock()
	d := backend.docs[memoryKey("t", "ai", job.ID)]
	var expired model.AnalysisAIRevision
	if err := json.Unmarshal(d.Body, &expired); err != nil {
		t.Fatal(err)
	}
	expired.LeaseExpiresAt = 1
	d.Body, _ = json.Marshal(expired)
	backend.docs[memoryKey("t", "ai", job.ID)] = d
	backend.mu.Unlock()
	if _, err := store.ClaimAnalysisAIRevision(ctx, "restart", time.Minute, 4*time.Minute, s.workflowIDs()); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("unknown external call replayed", err)
	}
	after, err := store.GetAnalysisAIRevision(ctx, "t", job.ID)
	if err != nil || after.Status != model.AnalysisFailed || after.LeaseToken <= job.LeaseToken {
		t.Fatal(after, err)
	}
	if _, err = store.FinishAnalysisAIRevision(ctx, "t", job.ID, job.LeaseToken, model.AnalysisAIResult{}, "", "old failure"); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("old worker after restart accepted", err)
	}
}
