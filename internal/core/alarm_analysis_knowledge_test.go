package core

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

const analysisAnswer = `{"summary":"研判完成","possibleReasons":["现场存在烟雾"],"suggestions":["核实现场"],"riskLevel":"HIGH","confidence":0.8}`

// lastPrompt returns the prompt of the latest alarm analysis run.
func lastPrompt(w *aitest.Workflows) string { return w.Last().Question }

func newAlarmKnowledgeEngine(t *testing.T) (*Engine, *memory.Repository, *local.Realtime, *aitest.Workflows) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realtime := local.NewRealtime()
	e := New(repo, archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ai := &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil }}
	e.AIWorkflows, e.HarnessTokens = ai, aitest.Tokens()
	kb := knowledge.NewLocal()
	e.KB = kb
	for _, doc := range []struct{ workflow, id, text string }{
		{model.AlarmAnalysisWorkflowID, "doc-alarm", "烟感告警处置 SOP 维修：先核实现场烟雾"},
		{"ops-assistant", "doc-ops", "运维助手专用处置 SOP 维修手册"},
	} {
		if err = kb.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: "t1", WorkflowID: doc.workflow, DocumentID: doc.id, ChunkID: doc.id + "-0", Content: []byte(doc.text)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-kb", TenantID: "t1", DeviceID: "device-1", AlarmType: "SMOKE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	return e, repo, realtime, ai
}

func TestAlarmAnalysisKnowledgeUsesOnlyAlarmAgentDocuments(t *testing.T) {
	ctx := aitest.Context(context.Background())
	e, repo, realtime, ai := newAlarmKnowledgeEngine(t)

	base, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", false)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := aitest.Claims(ai.Last())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lastPrompt(ai), "先核实现场烟雾") || claims.HasScope(auth.ScopeQueryKnowledgeBase) || claims.Knowledge != nil || base.KnowledgeScope != model.AIAnalysisScopeNone {
		t.Fatalf("analysis without a knowledge role must not retrieve knowledge: scopes=%v scope=%q", claims.Scopes, base.KnowledgeScope)
	}
	if ai.Last().WorkflowID != model.AlarmAnalysisWorkflowID || claims.Workflow != model.AlarmAnalysisWorkflowID || base.Summary != "研判完成" {
		t.Fatalf("alarm analysis must run the alarm-handler workflow: %#v %#v", ai.Last(), base)
	}
	published := len(realtime.Messages)

	scoped, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true)
	if err != nil {
		t.Fatal(err)
	}
	got := lastPrompt(ai)
	if !strings.Contains(got, "先核实现场烟雾") || strings.Contains(got, "运维助手专用") {
		t.Fatalf("alarm analysis must search only alarm-handler documents, got %q", got)
	}
	if claims, err = aitest.Claims(ai.Last()); err != nil || !claims.HasScope(auth.ScopeQueryKnowledgeBase) || claims.Knowledge == nil || claims.Knowledge.WorkflowID != model.AlarmAnalysisWorkflowID {
		t.Fatalf("knowledge run must bind the knowledge tool to alarm-handler: %#v err=%v", claims, err)
	}
	if scoped.KnowledgeScope != model.AlarmAnalysisWorkflowID || len(scoped.KnowledgeDocuments) != 1 || scoped.KnowledgeDocuments[0] != "doc-alarm" {
		t.Fatalf("unexpected knowledge metadata: %#v", scoped)
	}
	if len(realtime.Messages) != published {
		t.Fatalf("knowledge-based analysis must not be broadcast on the alarm topic")
	}
	for scope, want := range map[string]string{model.AIAnalysisScopeNone: "", model.AlarmAnalysisWorkflowID: "doc-alarm"} {
		saved, getErr := repo.GetAIAnalysis(ctx, "t1", "alarm-kb", scope)
		if getErr != nil {
			t.Fatalf("scope %q not stored separately: %v", scope, getErr)
		}
		if strings.Join(saved.KnowledgeDocuments, ",") != want {
			t.Fatalf("scope %q stored documents %v", scope, saved.KnowledgeDocuments)
		}
	}
}

func TestAlarmAnalysisKnowledgeHonorsAgentBinding(t *testing.T) {
	ctx := aitest.Context(context.Background())
	e, repo, _, ai := newAlarmKnowledgeEngine(t)

	if err := repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: model.AlarmAnalysisWorkflowID, RetrievalMode: "disabled", TopK: 5, MinScore: 0.25, NoMatchPolicy: "allow-model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true); err != nil {
		t.Fatal(err)
	}
	if claims, err := aitest.Claims(ai.Last()); err != nil || strings.Contains(lastPrompt(ai), "先核实现场烟雾") || claims.HasScope(auth.ScopeQueryKnowledgeBase) {
		t.Fatalf("disabled binding must skip retrieval and the knowledge tool: scopes=%v err=%v", claims.Scopes, err)
	}

	if err := repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: model.AlarmAnalysisWorkflowID, RetrievalMode: "auto", TopK: 5, MinScore: 1.1, NoMatchPolicy: "require-evidence"}); err != nil {
		t.Fatal(err)
	}
	calls := len(ai.Requests())
	analysis, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ai.Requests()) != calls || analysis.Error == "" || analysis.KnowledgeScope != model.AlarmAnalysisWorkflowID {
		t.Fatalf("required evidence without hits must fail instead of analysing without knowledge: %#v", analysis)
	}
}

type unscopedKnowledgeBase struct{}

func (unscopedKnowledgeBase) Index(context.Context, string, string, string, []byte) error { return nil }
func (unscopedKnowledgeBase) Search(context.Context, string, string, int) ([]string, error) {
	return []string{"租户全库内容"}, nil
}
func (unscopedKnowledgeBase) Health(context.Context) error { return nil }

func TestAlarmAnalysisKnowledgeRejectsUnscopedIndex(t *testing.T) {
	ctx := aitest.Context(context.Background())
	e, _, _, ai := newAlarmKnowledgeEngine(t)
	e.KB = unscopedKnowledgeBase{}
	analysis, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ai.Requests()) != 0 || analysis.Error == "" {
		t.Fatalf("an index without Agent filtering must not fall back to tenant-wide search: %#v", analysis)
	}
}
