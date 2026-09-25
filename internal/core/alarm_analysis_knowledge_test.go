package core

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type knowledgeCaptureAI struct {
	mu        sync.Mutex
	knowledge [][]string
}

func (a *knowledgeCaptureAI) AnalyzeAlarm(_ context.Context, alarm model.Alarm, _ []map[string]any, knowledge []string) (model.AIAnalysis, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.knowledge = append(a.knowledge, append([]string(nil), knowledge...))
	return model.AIAnalysis{AlarmID: alarm.ID, Summary: "研判完成", RiskLevel: alarm.AlarmLevel, CreatedAt: time.Now().UnixMilli()}, nil
}
func (a *knowledgeCaptureAI) Chat(context.Context, string, string) (string, error) { return "", nil }
func (a *knowledgeCaptureAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, nil
}
func (a *knowledgeCaptureAI) Health(context.Context) error { return nil }
func (a *knowledgeCaptureAI) last() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.knowledge[len(a.knowledge)-1]
}

func newAlarmKnowledgeEngine(t *testing.T) (*Engine, *memory.Repository, *local.Realtime, *knowledgeCaptureAI) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realtime := local.NewRealtime()
	e := New(repo, archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ai := &knowledgeCaptureAI{}
	e.AI = ai
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
	ctx := context.Background()
	e, repo, realtime, ai := newAlarmKnowledgeEngine(t)

	base, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ai.last()) != 0 || base.KnowledgeScope != model.AIAnalysisScopeNone {
		t.Fatalf("analysis without a knowledge role must not retrieve knowledge: knowledge=%v scope=%q", ai.last(), base.KnowledgeScope)
	}
	published := len(realtime.Messages)

	scoped, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ai.last(), "\n")
	if !strings.Contains(got, "先核实现场烟雾") || strings.Contains(got, "运维助手专用") {
		t.Fatalf("alarm analysis must search only alarm-handler documents, got %q", got)
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
	ctx := context.Background()
	e, repo, _, ai := newAlarmKnowledgeEngine(t)

	if err := repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: model.AlarmAnalysisWorkflowID, RetrievalMode: "disabled", TopK: 5, MinScore: 0.25, NoMatchPolicy: "allow-model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true); err != nil {
		t.Fatal(err)
	}
	if len(ai.last()) != 0 {
		t.Fatalf("disabled binding must skip retrieval, got %v", ai.last())
	}

	if err := repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: model.AlarmAnalysisWorkflowID, RetrievalMode: "auto", TopK: 5, MinScore: 1.1, NoMatchPolicy: "require-evidence"}); err != nil {
		t.Fatal(err)
	}
	calls := len(ai.knowledge)
	analysis, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ai.knowledge) != calls || analysis.Error == "" || analysis.KnowledgeScope != model.AlarmAnalysisWorkflowID {
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
	ctx := context.Background()
	e, _, _, ai := newAlarmKnowledgeEngine(t)
	e.KB = unscopedKnowledgeBase{}
	analysis, err := e.AnalyzeAlarm(ctx, "t1", "alarm-kb", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ai.knowledge) != 0 || analysis.Error == "" {
		t.Fatalf("an index without Agent filtering must not fall back to tenant-wide search: %#v", analysis)
	}
}
