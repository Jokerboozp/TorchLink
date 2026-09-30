package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func applicationAIAnswer(workflow, id string) string {
	body := map[string]any{"summary": "untrusted summary"}
	for _, field := range aiResultFieldNames(workflow) {
		body[field] = []model.AnalysisAIStatement{}
	}
	body[aiResultFieldNames(workflow)[0]] = []model.AnalysisAIStatement{{Text: "recorded limitation", FactIDs: []string{id}, DeviceIDs: []string{"d1"}}}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func TestApplicationAIWorkflowStrictSchemaAndBoundPublicCollections(t *testing.T) {
	for _, kind := range []string{KindResponse, KindMaintenance, KindInvestment} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store, s, a, run := aiFixtureKind(t, kind)
			createAI(t, s, a, run, "explicit")
			job := claimAI(t, store, s)
			input, err := s.BuildInput(ctx, job)
			if err != nil || !input.Coverage.SummaryProvided || !input.Coverage.Truncated {
				t.Fatal(input, err)
			}
			for _, output := range input.Outputs {
				if output.ID == "private" {
					t.Fatal("private checkpoint supplied")
				}
			}
			if _, err = s.ReadBoundAnalysisFacts(ctx, AIIdentity(job), "calculation-stage", 20, 0); !errors.Is(err, model.ErrAnalysisInvalid) {
				t.Fatal(err)
			}
			stored, _ := store.GetAnalysisAIRevision(ctx, "t", job.ID)
			answer := applicationAIAnswer(job.WorkflowID, "fact00")
			result, err := DecodeAIWorkflowResult(job.WorkflowID, answer, stored.SentFactIDs, job.DeviceIDs, input.Coverage)
			if err != nil || strings.Contains(result.Summary, "untrusted") {
				t.Fatal(result, err)
			}
			for _, mutation := range []func(map[string]any){
				func(b map[string]any) { b["candidateDraft"] = map[string]any{"enabled": false} },
				func(b map[string]any) { b["limitations"] = nil },
				func(b map[string]any) { delete(b, "limitations") },
				func(b map[string]any) { b["unknownField"] = true },
				func(b map[string]any) { b[aiResultFieldNames(job.WorkflowID)[0]] = true },
				func(b map[string]any) {
					b[aiResultFieldNames(job.WorkflowID)[0]] = []map[string]any{{"text": "not read", "factIds": []string{"fact24"}}}
				},
				func(b map[string]any) {
					b[aiResultFieldNames(job.WorkflowID)[0]] = []map[string]any{{"text": "outside scope", "factIds": []string{"fact00"}, "deviceIds": []string{"hidden"}}}
				},
			} {
				var b map[string]any
				_ = json.Unmarshal([]byte(answer), &b)
				mutation(b)
				raw, _ := json.Marshal(b)
				if _, err = DecodeAIWorkflowResult(job.WorkflowID, string(raw), stored.SentFactIDs, job.DeviceIDs, input.Coverage); err == nil {
					t.Fatal("invalid model structure accepted", string(raw))
				}
			}
			done, err := store.FinishAnalysisAIRevision(ctx, "t", job.ID, job.LeaseToken, result, "mock-harness", "")
			if err != nil || done.Status != model.AnalysisSucceeded {
				t.Fatal(done, err)
			}
			read, err := s.Get(ctx, a, kind, run.ID, job.ID)
			if err != nil || len(read.Interpretation) == 0 {
				t.Fatal(read, err)
			}
			if snapshot, err := s.Facts.Snapshot(ctx, a, kind, run.ID); err != nil || snapshot.ID != run.SnapshotID {
				t.Fatal("AI changed deterministic facts", snapshot, err)
			}
		})
	}
}

func TestApplicationAIFailuresAndFinanceRevocationKeepFacts(t *testing.T) {
	for _, scenario := range []string{"missing-key", "timeout", "429", "invalid-json", "unread-ref", "revoked-before-save", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store, s, a, run := aiFixtureKindOptions(t, KindInvestment, true)
			current := a
			current.Permissions = []string{"menu:devices", "menu:maintenance", AIStartOperation(KindInvestment), AIStopOperation(KindInvestment), FinanceReadOperation}
			var mu sync.Mutex
			s.Facts.Resolve = func(_ context.Context, identity Actor) (Actor, error) {
				mu.Lock()
				defer mu.Unlock()
				return current, nil
			}
			createAI(t, s, a, run, "fund-report")
			job := claimAI(t, store, s)
			s.Runner = func(ctx context.Context, j model.AnalysisAIRevision, in model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
				switch scenario {
				case "missing-key":
					return ports.AIWorkflowResult{}, errors.New("provider key unavailable")
				case "timeout":
					return ports.AIWorkflowResult{}, context.DeadlineExceeded
				case "429":
					return ports.AIWorkflowResult{}, errors.New("Harness HTTP 429")
				case "invalid-json":
					return ports.AIWorkflowResult{Answer: `{"priorityExplanations":false}`}, nil
				case "unread-ref":
					return ports.AIWorkflowResult{Answer: applicationAIAnswer(j.WorkflowID, "fact24")}, nil
				case "revoked-before-save":
					mu.Lock()
					current.Permissions = current.Permissions[:len(current.Permissions)-1]
					mu.Unlock()
				case "cancel":
					saved, _ := store.GetAnalysisAIRevision(ctx, "t", j.ID)
					_, err := store.StopAnalysisAIRevision(ctx, "t", j.ID, saved.Version)
					if err != nil {
						t.Fatal(err)
					}
				}
				return ports.AIWorkflowResult{Answer: applicationAIAnswer(j.WorkflowID, "fact00"), Model: "mock"}, nil
			}
			s.executeAI(ctx, job, slog.New(slog.NewTextHandler(io.Discard, nil)))
			result, err := store.GetAnalysisAIRevision(ctx, "t", job.ID)
			if err != nil || result.Status == model.AnalysisSucceeded || len(result.Interpretation) != 0 || !TerminalAnalysisStatus(result.Status) {
				t.Fatal("failed boundary saved successful body", result, err)
			}
			facts, err := store.GetAnalysisRun(ctx, "t", run.ID)
			if err != nil || facts.Status != model.AnalysisPartial || facts.SnapshotID != run.SnapshotID {
				t.Fatal("AI failure blocked fixed facts", facts, err)
			}
			if scenario == "revoked-before-save" {
				if _, err = s.Get(ctx, a, KindInvestment, run.ID, job.ID); !errors.Is(err, ErrForbidden) {
					t.Fatal("old fund AI readable after revoke", err)
				}
				if _, _, err = s.List(ctx, a, KindInvestment, run.ID, model.AnalysisFilter{}); !errors.Is(err, ErrForbidden) {
					t.Fatal(err)
				}
				if _, err = s.ReadBoundAnalysisFacts(ctx, AIIdentity(job), "summary", 1, 0); !errors.Is(err, ErrForbidden) {
					t.Fatal(err)
				}
			}
		})
	}
}
