package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func monitoringAnswer() string {
	return `{"summary":"模型不能覆盖实际覆盖说明","observedWeaknesses":[{"text":"已知缺口与未知时长需分别核实","factIds":["interval1"],"deviceIds":["d1"]}],"prioritizedChecks":[],"dependencyObservations":[{"text":"集中比例仅说明可见接入记录","factIds":["group1"],"deviceIds":["d1"]}],"limitations":[{"text":"窗口起点未知","factIds":["snapshot/summary"]}]}`
}

func TestMonitoringAIWorkflowStrictSchemaBoundScopeAndCoverage(t *testing.T) {
	ctx := context.Background()
	store, service, actor, facts := aiFixtureKind(t, KindMonitoring)
	actor.AllDevices, actor.DeviceIDs = false, []string{"d1", "d2"}
	actor.Permissions = []string{"menu:devices", "menu:monitoringGaps", "POST /api/v1/monitoring-gaps/runs/:id/ai-jobs", "POST /api/v1/monitoring-gaps/runs/:id/ai-jobs/:jobId/stop"}
	service.Facts.Resolve = func(context.Context, Actor) (Actor, error) { return actor, nil }
	queued := createAI(t, service, actor, facts, "c")
	if queued.WorkflowID != WorkflowMonitoring || queued.PromptVersion != MonitoringAIPromptVersion {
		t.Fatal(queued)
	}
	job := claimAI(t, store, service)
	input, err := service.BuildInput(ctx, job)
	if err != nil || input.Coverage.TotalOutputs != 37 || input.Coverage.OutputCount != 32 || input.WindowStart != facts.Start || input.WindowEnd != facts.End {
		t.Fatal(input, err)
	}
	current, _ := store.GetAnalysisAIRevision(ctx, actor.TenantID, job.ID)
	if len(current.SentFactIDs) == 0 || strings.Contains(strings.Join(current.SentFactIDs, ","), "private") {
		t.Fatal("private source manifest became a model fact", current)
	}
	for _, collection := range []string{"intervals", "dependency-groups"} {
		page, e := service.ReadBoundAnalysisFacts(ctx, AIIdentity(job), collection, 20, 0)
		if e != nil || len(page.Outputs) != 1 || page.HasMore {
			t.Fatal(collection, page, e)
		}
	}
	wrong := AIIdentity(job)
	wrong.AnalysisWorkflowID = WorkflowDataQuality
	if _, err = service.ReadBoundAnalysisFacts(ctx, wrong, "summary", 20, 0); !errors.Is(err, ErrForbidden) {
		t.Fatal("another workflow reused job proof", err)
	}
	if _, err = service.ReadBoundAnalysisFacts(ctx, AIIdentity(job), "input-manifest", 20, 0); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("private manifest collection exposed", err)
	}
	for _, answer := range []string{validAIAnswer("fact00"), strings.ReplaceAll(monitoringAnswer(), `"observedWeaknesses":[`, `"extra":[`), strings.ReplaceAll(monitoringAnswer(), `"prioritizedChecks":[]`, `"prioritizedChecks":null`), strings.ReplaceAll(monitoringAnswer(), `"group1"`, `"unread-group"`), strings.ReplaceAll(monitoringAnswer(), `"d1"`, `"hidden-parent"`)} {
		if _, err := DecodeAIWorkflowResult(WorkflowMonitoring, answer, current.SentFactIDs, current.DeviceIDs, input.Coverage); err == nil {
			t.Fatal("invalid monitoring result accepted", answer)
		}
	}
	// Another authorized device in the run cannot be explained using a fact
	// whose top-level device identity covers only d1. Keep the lease usable so
	// a rejected response cannot alter either the fixed facts or the job.
	wrongObject := strings.Replace(monitoringAnswer(), `"factIds":["interval1"],"deviceIds":["d1"]`, `"factIds":["interval1"],"deviceIds":["d2"]`, 1)
	decoded, err := DecodeAIWorkflowResult(WorkflowMonitoring, wrongObject, current.SentFactIDs, current.DeviceIDs, input.Coverage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.FinishAnalysisAIRevision(ctx, actor.TenantID, job.ID, job.LeaseToken, decoded, "mock-harness", ""); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("device scope widened by an unrelated cited fact", err)
	}
	afterRejected, err := store.GetAnalysisAIRevision(ctx, actor.TenantID, job.ID)
	if err != nil || afterRejected.Status != model.AnalysisRunning || len(afterRejected.Interpretation) != 0 {
		t.Fatal("rejected object reference committed a result", afterRejected, err)
	}
	service.Runner = func(ctx context.Context, job model.AnalysisAIRevision, _ model.AnalysisAIFacts) (ports.AIWorkflowResult, error) {
		identity, _ := ports.AIRunIdentityFrom(ctx)
		_, e := service.ReadBoundAnalysisFacts(ctx, identity, "findings", 5, 20)
		return ports.AIWorkflowResult{RunID: job.HarnessRunID, Model: "mock-harness", Answer: monitoringAnswer()}, e
	}
	service.executeAI(ctx, job, slog.New(slog.NewTextHandler(io.Discard, nil)))
	done, err := store.GetAnalysisAIRevision(ctx, actor.TenantID, job.ID)
	if err != nil || done.Status != model.AnalysisSucceeded {
		t.Fatal(done, err)
	}
	var body map[string]json.RawMessage
	if err = json.Unmarshal(done.Interpretation, &body); err != nil || len(body) != 6 || body["observedWeaknesses"] == nil || body["prioritizedChecks"] == nil || body["dependencyObservations"] == nil || body["interpretations"] != nil || !strings.Contains(string(body["summary"]), "37/37") {
		t.Fatal("stored monitoring schema or coverage changed", body, err)
	}
	// Historical body remains protected by the entire fixed snapshot scope.
	actor.DeviceIDs = []string{"d1"}
	if _, err = service.Get(ctx, actor, KindMonitoring, facts.ID, done.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("partial scope received cropped interpretation", err)
	}
	unchanged, _ := store.GetAnalysisRun(ctx, actor.TenantID, facts.ID)
	if unchanged.Version != facts.Version || unchanged.Status != model.AnalysisPartial {
		t.Fatal("interpretation changed monitoring facts", unchanged)
	}
}

func TestMonitoringAIUnsupportedWorkflowAndNoCrossSchemaExecution(t *testing.T) {
	_, service, _, _ := aiFixtureKind(t, KindMonitoring)
	if err := service.Register(AIWorkflowSpec{KindMonitoring, WorkflowDataQuality, AnalysisAIPromptVersion}); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("wrong application workflow registered", err)
	}
	if err := service.Register(AIWorkflowSpec{KindRuleLab, "unregistered", "v1"}); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("unknown workflow executed", err)
	}
	if _, err := DecodeAnalysisAIResult(monitoringAnswer(), []string{"interval1", "group1", "snapshot/summary"}, []string{"d1"}, model.AnalysisAICoverage{SummaryProvided: true}); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("monitoring result accepted by quality schema", err)
	}
}
