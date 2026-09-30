package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func rulePolicyAnswer(draft bool) string {
	s := `{"summary":"模型覆盖说明不可信","behaviorDifferences":[{"text":"候选多出一周期，须核对固定容差和未确认标签","factIds":["difference"],"deviceIds":["d1"]}],"verificationSuggestions":[],"limitations":[{"text":"未提供独立留出样本","factIds":["snapshot/summary"]}]`
	if draft {
		s += `,"candidateDraft":{"enabled":false,"name":"待复核候选","durationSeconds":5}`
	}
	return s + `}`
}

func TestRulePolicyAIStrictFactsAndAtomicDisabledCandidate(t *testing.T) {
	for _, mode := range []string{"success", "stop", "permission", "pointer-conflict", "invalid-candidate"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			store, service, actor, facts := aiFixtureKind(t, KindRuleLab)
			actor.Permissions = []string{"menu:devices", "menu:ruleLab", AIStartOperation(KindRuleLab), AIStopOperation(KindRuleLab)}
			service.Facts.Resolve = func(context.Context, Actor) (Actor, error) { return actor, nil }
			queued := createAI(t, service, actor, facts, "policy")
			if queued.WorkflowID != WorkflowRulePolicy || queued.PromptVersion != RulePolicyAIPromptVersion {
				t.Fatal(queued)
			}
			job := claimAI(t, store, service)
			input, err := service.BuildInput(ctx, job)
			if err != nil || input.Coverage.TotalOutputs != 38 || input.Coverage.OutputCount != 33 {
				t.Fatal(input, err)
			}
			current, _ := store.GetAnalysisAIRevision(ctx, "t", job.ID)
			if slices.Contains(current.SentFactIDs, "private") {
				t.Fatal("private comparison stage sent")
			}
			for _, collection := range []string{"outcomes", "diffs", "labels"} {
				page, e := service.ReadBoundAnalysisFacts(ctx, AIIdentity(job), collection, 20, 0)
				if e != nil || len(page.Outputs) != 1 {
					t.Fatal(collection, page, e)
				}
			}
			for _, answer := range []string{validAIAnswer("fact00"), strings.ReplaceAll(rulePolicyAnswer(false), `"verificationSuggestions":[]`, `"verificationSuggestions":null`), strings.ReplaceAll(rulePolicyAnswer(false), `"difference"`, `"unread"`), strings.ReplaceAll(rulePolicyAnswer(false), `"d1"`, `"outside"`), strings.ReplaceAll(rulePolicyAnswer(true), `"enabled":false`, `"enabled":true`), strings.ReplaceAll(rulePolicyAnswer(true), `"durationSeconds":5`, `"shell":"run"`)} {
				if _, e := DecodeAIWorkflowResult(WorkflowRulePolicy, answer, current.SentFactIDs, job.DeviceIDs, input.Coverage); e == nil {
					t.Fatal("invalid schema accepted", answer)
				}
			}
			prepared := false
			service.PrepareCandidate = func(_ context.Context, a Actor, id string, rule model.AlarmRule) (model.AnalysisConfigRevision, int64, error) {
				prepared = true
				if id != "experiment" || a.Username != actor.Username {
					t.Fatal(a, id)
				}
				if mode == "invalid-candidate" {
					return model.AnalysisConfigRevision{}, 0, model.ErrAnalysisInvalid
				}
				v, e := store.GetAnalysisConfig(ctx, "t", id)
				if e != nil {
					return v, 0, e
				}
				var body model.RuleLabExperiment
				_ = json.Unmarshal(v.Body, &body)
				rule.ID, rule.TenantID, rule.ProductID = body.CandidateRuleID, "t", body.Candidate.ProductID
				body.Candidate, body.CandidateEnabled = rule, false
				v.ID, v.Creator = "candidate", a.Username
				v.Body, _ = json.Marshal(body)
				return v, 1, nil
			}
			service.Runner = func(context.Context, model.AnalysisAIRevision, model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
				switch mode {
				case "stop":
					latest, _ := store.GetAnalysisAIRevision(ctx, "t", job.ID)
					_, e := store.StopAnalysisAIRevision(ctx, "t", job.ID, latest.Version)
					if e != nil {
						t.Fatal(e)
					}
				case "permission":
					actor.Permissions = nil
				case "pointer-conflict":
					v, _ := store.GetAnalysisConfig(ctx, "t", "experiment")
					v.ID = "independent-edit"
					if _, e := store.PutAnalysisConfig(ctx, v, 1); e != nil {
						t.Fatal(e)
					}
				}
				return ports.AIWorkflowResult{RunID: job.HarnessRunID, Model: "mock-harness", Answer: rulePolicyAnswer(true)}, nil
			}
			service.executeAI(ctx, job, slog.New(slog.NewTextHandler(io.Discard, nil)))
			done, e := store.GetAnalysisAIRevision(ctx, "t", job.ID)
			if e != nil {
				t.Fatal(e)
			}
			candidate, e := store.GetAnalysisConfig(ctx, "t", "candidate")
			if mode == "success" {
				if !prepared || e != nil || done.Status != model.AnalysisSucceeded || candidate.Version != 2 {
					t.Fatal(done, candidate, e)
				}
				var body model.RuleLabExperiment
				_ = json.Unmarshal(candidate.Body, &body)
				if body.Candidate.Enabled || body.CandidateEnabled || !strings.Contains(string(done.Interpretation), `"candidateRevisionId":"candidate"`) || strings.Contains(string(done.Interpretation), `"interpretations"`) {
					t.Fatal(body, string(done.Interpretation))
				}
			} else {
				if !errors.Is(e, model.ErrNotFound) || done.Status == model.AnalysisSucceeded {
					t.Fatal("late/invalid draft persisted", mode, done, candidate, e)
				}
				if (mode == "stop" || mode == "permission") && prepared {
					t.Fatal("draft validation ran after revoked binding")
				}
			}
			unchanged, _ := store.GetAnalysisRun(ctx, "t", facts.ID)
			if unchanged.Version != facts.Version || unchanged.SnapshotID != facts.SnapshotID {
				t.Fatal("AI changed fixed facts", unchanged)
			}
		})
	}
}

func TestRulePolicyAIRejectsDatasetPhaseAtPersistentBoundary(t *testing.T) {
	ctx := context.Background()
	store, s, a, _ := aiFixtureKind(t, KindRuleLab)
	request := storedRun("dataset-only", KindRuleLab)
	request.Creator = a.Username
	request.Parameters = json.RawMessage(`{"phase":"DATASET","datasetId":"dataset"}`)
	if _, e := store.CreateAnalysisRun(ctx, request, 100); e != nil {
		t.Fatal(e)
	}
	run, e := store.ClaimAnalysisRun(ctx, "worker", time.Minute, []string{KindRuleLab})
	if e != nil {
		t.Fatal(e)
	}
	run, e = store.CommitAnalysisBatch(ctx, "t", run.ID, run.LeaseToken, model.AnalysisBatch{ID: "done", Status: model.AnalysisSucceeded, Snapshot: &model.AnalysisSnapshot{ID: "dataset-snapshot", DataCutoff: 2000, Statistics: json.RawMessage(`{"inputs":1}`)}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, a, KindRuleLab, run.ID, CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "dataset-ai"}); !errors.Is(e, model.ErrAnalysisInvalid) {
		t.Fatal("dataset model execution enabled", e)
	}
}
