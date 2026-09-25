package core

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func newBusinessEngine(t *testing.T, answer func(ports.AIWorkflowRequest) (string, error)) (*Engine, *memory.Repository, *aitest.Workflows) {
	t.Helper()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	workflows := &aitest.Workflows{Answer: answer}
	e.AIWorkflows, e.HarnessTokens = workflows, aitest.Tokens()
	return e, repo, workflows
}

func sortedScopes(c auth.Claims) string {
	scopes := append([]string(nil), c.Scopes...)
	sort.Strings(scopes)
	return strings.Join(scopes, ",")
}

// Every business feature runs its own Harness workflow with only the tool
// scopes that feature needs.
func TestBusinessFeaturesRunTheirHarnessWorkflows(t *testing.T) {
	ctx := aitest.Context(context.Background())
	e, repo, workflows := newBusinessEngine(t, func(req ports.AIWorkflowRequest) (string, error) {
		switch req.WorkflowID {
		case WorkflowRuleDraft:
			return `{"name":"高温","alarmType":"high_temperature","level":"high","match":"all","conditions":[{"field":"temperature","operator":"gt","value":80}],"actions":[]}`, nil
		default:
			return "结论正文", nil
		}
	})
	if err := repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: WorkflowOpsReport, RetrievalMode: "disabled"}); err != nil {
		t.Fatal(err)
	}

	rule, err := e.DraftRule(ctx, "t1", "温度超过 80 度时报警")
	if err != nil || rule.Enabled || rule.AlarmType != "HIGH_TEMPERATURE" || rule.TenantID != "t1" {
		t.Fatalf("rule draft: %#v err=%v", rule, err)
	}
	report, err := e.GenerateReport(ctx, "t1", "日报", 1, time.Now().UnixMilli())
	if err != nil || report != "结论正文" {
		t.Fatalf("report: %q err=%v", report, err)
	}
	inspection, err := e.InspectDeviceHealth(ctx, "t1")
	if err != nil || inspection.AIAdvice != "结论正文" {
		t.Fatalf("inspection: %#v err=%v", inspection, err)
	}

	want := map[string]string{
		WorkflowRuleDraft:        auth.ScopeQuerySystemOverview,
		WorkflowOpsReport:        strings.Join([]string{auth.ScopeQueryAlarmList, auth.ScopeQueryDeviceLatest, auth.ScopeQueryPropertyHistory, auth.ScopeQuerySimilarAlarms}, ","),
		WorkflowHealthInspection: strings.Join([]string{auth.ScopeQueryAlarmList, auth.ScopeQueryDeviceLatest, auth.ScopeQueryKnowledgeBase, auth.ScopeQueryPropertyHistory, auth.ScopeQuerySystemOverview}, ","),
	}
	for _, req := range workflows.Requests() {
		claims, err := aitest.Claims(req)
		if err != nil {
			t.Fatal(err)
		}
		if claims.Workflow != req.WorkflowID || claims.TokenUse != "harness" || claims.TenantID != "t1" {
			t.Fatalf("%s token is not bound to its workflow: %#v", req.WorkflowID, claims)
		}
		if got := sortedScopes(claims); got != want[req.WorkflowID] {
			t.Fatalf("%s scopes = %s, want %s", req.WorkflowID, got, want[req.WorkflowID])
		}
		if claims.HasScope(auth.ScopeCreateRuleDraft) {
			t.Fatalf("%s must not be able to save rule drafts", req.WorkflowID)
		}
	}
	if len(workflows.Requests()) != 3 {
		t.Fatalf("expected three business runs, got %d", len(workflows.Requests()))
	}
	// Knowledge follows each Agent's binding: disabled for the report, bound to
	// the inspector's own documents otherwise.
	for _, req := range workflows.Requests() {
		claims, _ := aitest.Claims(req)
		if req.WorkflowID == WorkflowHealthInspection && (claims.Knowledge == nil || claims.Knowledge.WorkflowID != WorkflowHealthInspection) {
			t.Fatalf("inspection knowledge must be bound to its Agent: %#v", claims.Knowledge)
		}
		if req.WorkflowID == WorkflowOpsReport && !strings.Contains(req.Question, "未授权知识库") {
			t.Fatal("a run without knowledge access must be told not to use the knowledge tool")
		}
	}
}

// The caller's permissions cap the run: a caller without a tool scope cannot
// hand it to the workflow.
func TestBusinessRunNeverExceedsCallerScopes(t *testing.T) {
	e, _, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return "结论", nil })
	ctx := ports.WithAIRunIdentity(context.Background(), ports.AIRunIdentity{Username: "reader", ManagedUser: true, SessionVersion: 7, Scopes: []string{auth.ScopeQueryAlarmList}})
	if _, err := e.GenerateReport(ctx, "t1", "日报", 1, 2); err != nil {
		t.Fatal(err)
	}
	claims, err := aitest.Claims(workflows.Last())
	if err != nil {
		t.Fatal(err)
	}
	if sortedScopes(claims) != auth.ScopeQueryAlarmList || !claims.ManagedUser || claims.SessionVersion != 7 || claims.Username != "reader" {
		t.Fatalf("run must keep the managed caller and its scopes: %#v", claims)
	}
}

func TestBusinessRunRequiresIdentityAndHarness(t *testing.T) {
	e, _, workflows := newBusinessEngine(t, nil)
	if _, err := e.DraftRule(context.Background(), "t1", "高温报警"); err == nil || len(workflows.Requests()) != 0 {
		t.Fatalf("a run without identity must be refused, err=%v", err)
	}
	e.AIWorkflows = nil
	if _, err := e.DraftRule(aitest.Context(context.Background()), "t1", "高温报警"); !errors.Is(err, ErrAIWorkflowsUnavailable) {
		t.Fatalf("missing Harness must be reported, got %v", err)
	}
}

// Automatic analysis has no user: it runs as a system identity limited to
// alarm reading tools and never receives knowledge.
func TestAutomaticAlarmAnalysisUsesRestrictedSystemIdentity(t *testing.T) {
	e, repo, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil })
	alarm := model.Alarm{ID: "alarm-auto", TenantID: "t1", DeviceID: "device-1", AlarmType: "SMOKE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}
	if _, _, err := repo.UpsertAlarm(context.Background(), alarm); err != nil {
		t.Fatal(err)
	}
	if err := e.handleAI(context.Background(), mustJSON(alarm)); err != nil {
		t.Fatal(err)
	}
	claims, err := aitest.Claims(workflows.Last())
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{auth.ScopeQueryAlarmList, auth.ScopeQueryPropertyHistory, auth.ScopeQuerySimilarAlarms}, ",")
	if claims.Username != "system:alarm-analysis" || claims.ManagedUser || sortedScopes(claims) != want || claims.Knowledge != nil || claims.Workflow != WorkflowAlarmAnalysis {
		t.Fatalf("automatic analysis identity: %#v", claims)
	}
	if saved, err := repo.GetAIAnalysis(context.Background(), "t1", "alarm-auto", model.AIAnalysisScopeNone); err != nil || saved.Summary != "研判完成" || saved.Model != "aitest-model" {
		t.Fatalf("automatic analysis not saved from the workflow answer: %#v err=%v", saved, err)
	}
}
