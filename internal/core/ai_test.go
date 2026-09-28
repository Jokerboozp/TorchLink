package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ledongthuc/pdf"

	"iot-platform/internal/adapters/knowledge"
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

func TestAlarmEventsDoNotStartAnalysis(t *testing.T) {
	for name, components := range map[string]Components{"combined": AllComponents(), "ai": {AI: true}} {
		t.Run(name, func(t *testing.T) {
			e, repo, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := e.StartWith(ctx, components); err != nil {
				t.Fatal(err)
			}
			alarm := model.Alarm{ID: "manual-only", TenantID: "t1", DeviceID: "device-1", Status: "ACTIVE", AlarmLevel: "HIGH"}
			if _, _, err := repo.UpsertAlarm(ctx, alarm); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := e.Bus.Publish(ctx, model.TopicAlarmRaised, alarm.ID, mustJSON(alarm)); err != nil {
					t.Fatal(err)
				}
			}
			if len(workflows.Requests()) != 0 {
				t.Fatal("alarm events must not start a workflow")
			}
			if _, err := repo.GetAIAnalysis(ctx, alarm.TenantID, alarm.ID, model.AIAnalysisScopeNone); !errors.Is(err, model.ErrNotFound) {
				t.Fatalf("alarm events must not persist analysis: %v", err)
			}
			if _, err := e.AnalyzeAlarm(aitest.Context(ctx), alarm.TenantID, alarm.ID, false); err != nil || len(workflows.Requests()) != 1 {
				t.Fatalf("explicit analysis must still run once: %v", err)
			}
		})
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

// A busy Harness makes background runs wait for a free slot instead of
// failing; the wait is bounded.
func TestBusinessRunWaitsWhileHarnessIsBusy(t *testing.T) {
	originalWait, originalDelay := businessRunCapacityWait, businessRunRetryDelay
	businessRunCapacityWait, businessRunRetryDelay = time.Second, 10*time.Millisecond
	defer func() { businessRunCapacityWait, businessRunRetryDelay = originalWait, originalDelay }()
	busy := 2
	e, _, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) {
		if busy > 0 {
			busy--
			return "", ports.ErrAIWorkflowBusy
		}
		return "结论", nil
	})
	if report, err := e.GenerateReport(aitest.Context(context.Background()), "t1", "日报", 1, 2); err != nil || report != "结论" {
		t.Fatalf("run must succeed after the Harness frees a slot: %q %v", report, err)
	}
	if len(workflows.Requests()) != 3 {
		t.Fatalf("expected two busy attempts and one success, got %d", len(workflows.Requests()))
	}

	e, _, _ = newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return "", ports.ErrAIWorkflowBusy })
	started := time.Now()
	if _, err := e.GenerateReport(aitest.Context(context.Background()), "t1", "日报", 1, 2); !errors.Is(err, ports.ErrAIWorkflowBusy) {
		t.Fatalf("a Harness that stays busy must fail after the wait, got %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("waiting for capacity must be bounded")
	}
}

// Queued automatic analyses skip alarms resolved while waiting and alarms that
// a redelivered event already analysed.
func TestAutomaticAnalysisSkipsResolvedAndAnalysedAlarms(t *testing.T) {
	e, repo, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil })
	ctx := context.Background()
	resolved := model.Alarm{ID: "alarm-resolved", TenantID: "t1", DeviceID: "d1", AlarmType: "SMOKE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}
	if _, _, err := repo.UpsertAlarm(ctx, resolved); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetAlarm(ctx, "t1", "alarm-resolved")
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = "RECOVERED"
	if err = repo.UpdateAlarm(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if err = e.handleAI(ctx, mustJSON(resolved)); err != nil || len(workflows.Requests()) != 0 {
		t.Fatalf("a recovered alarm must not be analysed: runs=%d err=%v", len(workflows.Requests()), err)
	}
	active := model.Alarm{ID: "alarm-active", TenantID: "t1", DeviceID: "d1", AlarmType: "SMOKE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}
	if _, _, err = repo.UpsertAlarm(ctx, active); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = e.handleAI(ctx, mustJSON(active)); err != nil {
			t.Fatal(err)
		}
	}
	if len(workflows.Requests()) != 1 {
		t.Fatalf("a redelivered alarm must be analysed once, got %d runs", len(workflows.Requests()))
	}
}

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

type countingHistoryRepo struct {
	*memory.Repository
	calls int
	rows  map[string][]map[string]any
}

func (r *countingHistoryRepo) PropertyHistory(_ context.Context, _, _, property string, _, _ int64, _ int) ([]map[string]any, error) {
	r.calls++
	return r.rows[property], nil
}

func TestAlarmPropertyHistoryQueriesOncePerPropertyAndCondenses(t *testing.T) {
	end := int64(100 * alarmHistoryDayMs)
	temperature := []map[string]any{}
	for i := 0; i < 120; i++ { // one point per minute for two hours, newest first like the stores
		temperature = append(temperature, map[string]any{"timestamp": end - int64(i)*60*1000, "value": float64(20 + i%10)})
	}
	smoke := []map[string]any{{"timestamp": end, "value": true}, {"timestamp": end - 1000, "value": false}, {"timestamp": end - 2000, "value": true}}
	repo := &countingHistoryRepo{Repository: memory.NewRepository(), rows: map[string][]map[string]any{"temperature": temperature, "smoke": smoke}}
	e := &Engine{Repo: repo}

	history := e.alarmPropertyHistory(context.Background(), model.Alarm{TenantID: "t1", DeviceID: "d1", LastTriggeredAt: end})
	if repo.calls != len(alarmHistoryProperties) {
		t.Fatalf("expected one query per property, got %d", repo.calls)
	}
	if len(history) != 2 {
		t.Fatalf("only properties with data are included, got %d", len(history))
	}
	temp := history[0]
	if recent := temp["recent10m"].([]map[string]any); len(recent) != 11 || recent[len(recent)-1]["timestamp"] != end {
		t.Fatalf("recent points must cover the last 10 minutes in time order: %v", recent)
	}
	stats := temp["stats1h"].(map[string]any)
	if stats["count"] != 61 || stats["min"] != 20.0 || stats["max"] != 29.0 {
		t.Fatalf("unexpected 1h stats: %v", stats)
	}
	if day := temp["stats24h"].(map[string]any); day["count"] != 120 {
		t.Fatalf("unexpected 24h stats: %v", day)
	}
	smokeStats := history[1]["stats24h"].(map[string]any)["valueCounts"].(map[string]int)
	if smokeStats["true"] != 2 || smokeStats["false"] != 1 {
		t.Fatalf("non-numeric values must be counted: %v", smokeStats)
	}

	many := []map[string]any{}
	for i := 0; i < alarmHistoryLimit; i++ {
		many = append(many, map[string]any{"timestamp": end - int64(i)*1000, "value": 1.0})
	}
	summary := summarizePropertyHistory("temperature", many, end)
	if len(summary["recent10m"].([]map[string]any)) != alarmHistoryRecentLimit || summary["truncated"] == nil {
		t.Fatalf("dense history must stay bounded and report truncation: %v", summary["truncated"])
	}
}

func TestInspectionPromptSnapshotIsBounded(t *testing.T) {
	items := []model.DeviceHealthItem{}
	for i := 0; i < inspectionPromptLimit+15; i++ {
		items = append(items, model.DeviceHealthItem{DeviceID: "bad", Severity: "HIGH"})
	}
	for i := 0; i < 500; i++ {
		items = append(items, model.DeviceHealthItem{DeviceID: "ok", Severity: "INFO"})
	}
	snapshot := inspectionPromptSnapshot(1, map[string]int{"total": len(items)}, items)
	if listed := snapshot["devicesNeedingAttention"].([]model.DeviceHealthItem); len(listed) != inspectionPromptLimit {
		t.Fatalf("prompt lists %d devices, want %d", len(listed), inspectionPromptLimit)
	}
	if snapshot["omittedAttentionDevices"] != 15 {
		t.Fatalf("omitted devices must be reported: %v", snapshot["omittedAttentionDevices"])
	}
}

func TestRenderHealthInspectionPDF(t *testing.T) {
	data, err := RenderHealthInspectionPDF(model.DeviceHealthReport{
		GeneratedAt: 1700000000000,
		Summary:     "共检查 1 个设备。",
		Counts:      map[string]int{"total": 1, "healthy": 0, "attention": 1, "critical": 0, "offline": 1, "activeAlarms": 1},
		Items: []model.DeviceHealthItem{{
			DeviceID: "device-001", DeviceName: "一号烟感", ProductID: "smoke", BusinessStatus: "OFFLINE", DataStatus: "STALE", ActiveAlarmCount: 1, Severity: "HIGH", Findings: []string{"设备已离线或疑似离线"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range [][]byte{[]byte("%PDF-1.4"), []byte("/STSong-Light"), []byte("/Type /Page"), []byte("xref"), []byte("%%EOF")} {
		if !bytes.Contains(data, marker) {
			t.Fatalf("PDF is missing marker %q", marker)
		}
	}
	if len(data) < 500 {
		t.Fatalf("PDF is unexpectedly small: %d bytes", len(data))
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("generated PDF cannot be parsed: %v", err)
	}
	if reader.NumPage() < 1 {
		t.Fatalf("generated PDF has no pages")
	}
}

func TestRenderHealthInspectionPDFStandardLayout(t *testing.T) {
	report := model.DeviceHealthReport{
		TenantID:    "tenant-001",
		GeneratedAt: 1700000000000,
		Summary:     "本次巡检完成设备健康、数据新鲜度和活动告警核查。",
		AIAdvice:    "总体判断：设备整体可用。\n建议动作：优先处理离线设备并核查活动告警。\n数据局限：本报告基于平台最近一次上报快照。",
		Counts:      map[string]int{"total": 12, "healthy": 8, "attention": 4, "critical": 1, "offline": 2, "activeAlarms": 3},
		Warnings:    []string{"部分设备需要现场复核。"},
	}
	for index := 0; index < 12; index++ {
		report.Items = append(report.Items, model.DeviceHealthItem{
			DeviceID:         "device-" + strconv.Itoa(index),
			DeviceName:       "测试设备",
			ProductID:        "smoke-detector",
			BusinessStatus:   "ONLINE",
			DataStatus:       "FRESH",
			LastSeenAt:       1700000000000,
			ActiveAlarmCount: 0,
			Severity:         "INFO",
			Findings:         []string{"最近状态正常"},
		})
	}

	data, err := RenderHealthInspectionPDF(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range [][]byte{[]byte("/BaseFont /Helvetica"), []byte("/BaseFont /Helvetica-Bold"), []byte(" rg"), []byte(" re f")} {
		if !bytes.Contains(data, marker) {
			t.Fatalf("standard PDF is missing layout marker %q", marker)
		}
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("standard PDF cannot be parsed: %v", err)
	}
	if reader.NumPage() < 2 {
		t.Fatalf("standard PDF should contain overview and detail pages, got %d", reader.NumPage())
	}
}

// A tenant with more devices than the PDF lists gets the most severe rows and
// a note of how many were left out, not thousands of pages.
func TestRenderHealthInspectionPDFBoundsDeviceRows(t *testing.T) {
	items := make([]model.DeviceHealthItem, InspectionPDFMaxDevices+500)
	for i := range items {
		items[i] = model.DeviceHealthItem{DeviceID: "device-" + strconv.Itoa(i), ProductID: "smoke", BusinessStatus: "NEVER_SEEN", Severity: "HIGH", Findings: []string{"设备尚未收到有效上报"}}
	}
	bounded, err := RenderHealthInspectionPDF(model.DeviceHealthReport{GeneratedAt: 1700000000000, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	limit, err := RenderHealthInspectionPDF(model.DeviceHealthReport{GeneratedAt: 1700000000000, Items: items[:InspectionPDFMaxDevices]})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := pdf.NewReader(bytes.NewReader(bounded), int64(len(bounded)))
	if err != nil {
		t.Fatal(err)
	}
	limitReader, err := pdf.NewReader(bytes.NewReader(limit), int64(len(limit)))
	if err != nil {
		t.Fatal(err)
	}
	if reader.NumPage() > limitReader.NumPage()+1 {
		t.Fatalf("rows beyond the limit were rendered: %d pages vs %d", reader.NumPage(), limitReader.NumPage())
	}
}

func TestOpsReportBoundsContextAtLargeDevicePopulation(t *testing.T) {
	e, repo, workflows := newBusinessEngine(t, func(req ports.AIWorkflowRequest) (string, error) {
		if len(req.Question) > 20000 {
			t.Errorf("ops report exceeds Harness input budget: %d bytes", len(req.Question))
		}
		return "报告", nil
	})
	ctx := aitest.Context(context.Background())
	for i := 0; i < 500; i++ {
		_ = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t1", DeviceID: strconv.Itoa(i), BusinessStatus: "ONLINE", Reason: strings.Repeat("设备详情", 100)})
	}
	if _, err := e.GenerateReport(ctx, "t1", "日报", 1, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if len(workflows.Requests()) != 1 {
		t.Fatal("report did not run")
	}
}

type cancellableAI struct {
	aitest.Workflows
	started chan struct{}
}

func (w *cancellableAI) StreamChat(ctx context.Context, req ports.AIWorkflowRequest, _ func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	close(w.started)
	<-ctx.Done()
	return ports.AIWorkflowResult{}, ctx.Err()
}
func TestAutomaticAnalysisCancelsResolvedInFlightAlarm(t *testing.T) {
	e, repo, _ := newBusinessEngine(t, nil)
	runtime := &cancellableAI{started: make(chan struct{})}
	e.AIWorkflows = runtime
	alarm := model.Alarm{TenantID: "t", ID: "a", DeviceID: "d", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}
	_, _, _ = repo.UpsertAlarm(context.Background(), alarm)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- e.handleAI(ctx, mustJSON(alarm)) }()
	<-runtime.started
	alarm.Status = "RECOVERED"
	_ = repo.UpdateAlarm(context.Background(), alarm)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2200 * time.Millisecond):
		t.Fatal("resolved alarm continues consuming model capacity")
	}
}

func TestAutomaticAnalysisBudgetsAndOutcomes(t *testing.T) {
	for _, kind := range []string{"expired", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			e, repo, w := newBusinessEngine(t, nil)
			alarm := model.Alarm{TenantID: "t", ID: "a", DeviceID: "d", Status: "ACTIVE", LastTriggeredAt: time.Now().Add(-time.Minute).UnixMilli()}
			_, _, _ = repo.UpsertAlarm(context.Background(), alarm)
			if kind == "expired" {
				e.ConfigureAutomaticAnalysis(time.Second, time.Second, 12)
			} else {
				e.ConfigureAutomaticAnalysis(30*time.Millisecond, 0, 0)
				e.AIWorkflows = &cancellableAI{started: make(chan struct{})}
			}
			if err := e.handleAI(context.Background(), mustJSON(alarm)); err != nil {
				t.Fatal(err)
			}
			saved, err := repo.GetAIAnalysis(context.Background(), "t", "a", model.AIAnalysisScopeNone)
			want := "skipped"
			if kind == "timeout" {
				want = "failed"
			}
			if err != nil || saved.Status != want || saved.Error == "" {
				t.Fatal(saved, err)
			}
			if kind == "expired" && len(w.Requests()) != 0 {
				t.Fatal("expired work called model")
			}
		})
	}
	e, _, _ := newBusinessEngine(t, nil)
	e.ConfigureAutomaticAnalysis(time.Second, 0, 600)
	ctx := context.Background()
	alarm := model.Alarm{}
	if err := e.waitAutomaticBudget(ctx, alarm); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := e.waitAutomaticBudget(ctx, alarm); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 80*time.Millisecond {
		t.Fatal("request rate budget was not applied")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := e.waitAutomaticBudget(cancelled, alarm); !errors.Is(err, context.Canceled) {
		t.Fatal("rate wait ignored cancellation", err)
	}
}
