package aiworkflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/aiprompt"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func newBusinessEngine(t *testing.T, answer func(ports.AIWorkflowRequest) (string, error)) (*testEngine, *memory.Repository, *aitest.Workflows) {
	t.Helper()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := wrap(core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))))
	workflows := &aitest.Workflows{Answer: answer}
	e.AIWorkflows, e.HarnessTokens = workflows, aitest.Tokens()
	e.KB = knowledge.NewLocal()
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
	ctx := ports.WithAIRunIdentity(context.Background(), ports.AIRunIdentity{TenantID: "t1", Username: "reader", ManagedUser: true, SessionVersion: 7, Scopes: []string{auth.ScopeQueryAlarmList}})
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

type businessKnowledgeIndex struct {
	*knowledge.Local
	mu       sync.Mutex
	requests []ports.KnowledgeSearchRequest
	err      error
	hits     []ports.KnowledgeHit
}

func (k *businessKnowledgeIndex) SearchKnowledge(ctx context.Context, request ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) {
	k.mu.Lock()
	k.requests = append(k.requests, request)
	k.mu.Unlock()
	if k.err != nil {
		return nil, k.err
	}
	if k.hits != nil {
		return k.hits, nil
	}
	return k.Local.SearchKnowledge(ctx, request)
}

func (k *businessKnowledgeIndex) Requests() []ports.KnowledgeSearchRequest {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]ports.KnowledgeSearchRequest(nil), k.requests...)
}

func installBusinessHarnessHTTP(t *testing.T, engine *testEngine, beforeRun func(ports.AIWorkflowRequest)) <-chan ports.AIWorkflowRequest {
	t.Helper()
	received := make(chan ports.AIWorkflowRequest, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/stream" || r.Method != http.MethodPost {
			t.Errorf("unexpected Harness request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var request ports.AIWorkflowRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		request.MCPToken = auth.Bearer(r.Header.Get("Authorization"))
		if beforeRun != nil {
			beforeRun(request)
		}
		received <- request
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(ports.AIWorkflowEvent{Type: "run.completed", RunID: request.RunID, Answer: "已接收知识证据"})
	}))
	t.Cleanup(server.Close)
	harness, err := aiadapter.NewHarness(server.URL, aitest.Secret, "https://platform.example/mcp/harness", "test-model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	engine.AIWorkflows = harness
	return received
}

func TestBusinessFirstHarnessRequestContainsScopedKnowledgeEvidence(t *testing.T) {
	for _, feature := range []struct {
		workflow string
		tools    []string
	}{
		{WorkflowHealthInspection, []string{"query_system_overview", "query_knowledge_base"}},
		{WorkflowOpsReport, []string{"query_alarm_list", "query_knowledge_base"}},
		{WorkflowProtocolAssist, []string{"query_knowledge_base"}},
		{WorkflowRuleDraft, []string{"query_system_overview"}},
	} {
		t.Run(feature.workflow, func(t *testing.T) {
			ctx := aitest.Context(context.Background())
			engine, repo, _ := newBusinessEngine(t, nil)
			index := &businessKnowledgeIndex{Local: knowledge.NewLocal()}
			engine.KB = index
			binding := model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: feature.workflow, RetrievalMode: "always", TopK: 3, MinScore: 0.1, NoMatchPolicy: "require-evidence", ProductIDs: []string{"p1"}, Categories: []string{"manual"}, Tags: []string{"sop"}}
			if err := repo.SaveWorkflowKnowledgeBinding(ctx, binding); err != nil {
				t.Fatal(err)
			}
			for _, doc := range []ports.KnowledgeIndexInput{
				{TenantID: "t1", WorkflowID: feature.workflow, ProductID: "p1", Category: "manual", Tags: []string{"sop"}, DocumentID: "allowed-doc", ChunkID: "allowed-chunk", Content: []byte("高温 核实 现场设备")},
				{TenantID: "t2", WorkflowID: feature.workflow, ProductID: "p1", Category: "manual", Tags: []string{"sop"}, DocumentID: "other-tenant", ChunkID: "other-tenant-chunk", Content: []byte("高温 核实 租户机密")},
				{TenantID: "t1", WorkflowID: "other-agent", ProductID: "p1", Category: "manual", Tags: []string{"sop"}, DocumentID: "other-agent", ChunkID: "other-agent-chunk", Content: []byte("高温 核实 其他智能体机密")},
				{TenantID: "t1", WorkflowID: feature.workflow, ProductID: "p2", Category: "manual", Tags: []string{"sop"}, DocumentID: "other-product", ChunkID: "other-product-chunk", Content: []byte("高温 核实 其他产品机密")},
			} {
				if err := index.IndexKnowledge(ctx, doc); err != nil {
					t.Fatal(err)
				}
			}
			received := installBusinessHarnessHTTP(t, engine, func(request ports.AIWorkflowRequest) {
				if len(index.Requests()) != 1 {
					t.Error("knowledge must be retrieved before the first HTTP Harness request")
				}
			})
			result, err := engine.runBusinessWorkflow(ctx, "t1", feature.workflow, "", "高温 核实", "高温 核实", feature.tools, 2048)
			if err != nil || result.Answer != "已接收知识证据" {
				t.Fatalf("run result: %#v err=%v", result, err)
			}
			request := <-received
			claims, err := aitest.Claims(request)
			if err != nil || claims.TenantID != "t1" || claims.Workflow != feature.workflow {
				t.Fatalf("unexpected run credentials: %#v err=%v", claims, err)
			}
			queries := index.Requests()
			if len(queries) != 1 || queries[0].TenantID != "t1" || queries[0].WorkflowID != feature.workflow || strings.Join(queries[0].ProductIDs, ",") != "p1" || strings.Join(queries[0].Categories, ",") != "manual" || strings.Join(queries[0].Tags, ",") != "sop" {
				t.Fatalf("retrieval must use the entire Agent binding: %#v", queries)
			}
			for _, text := range []string{"documentId=allowed-doc", "chunkId=allowed-chunk", "高温 核实 现场设备"} {
				if !strings.Contains(request.Question, text) {
					t.Fatalf("first Harness request is missing source evidence %q: %q", text, request.Question)
				}
			}
			if strings.Contains(request.Question, "机密") {
				t.Fatalf("a forbidden document reached the first Harness request: %q", request.Question)
			}
			if feature.workflow == WorkflowRuleDraft && (claims.HasScope(auth.ScopeQueryKnowledgeBase) || claims.Knowledge != nil) {
				t.Fatalf("prefetch must not expand the rule Agent tool whitelist: %#v", claims)
			}
		})
	}
}

func TestBusinessKnowledgePermissionAndRequiredEvidenceBeforeHarness(t *testing.T) {
	for _, policy := range []string{"allow-model", "require-evidence"} {
		t.Run(policy, func(t *testing.T) {
			engine, repo, _ := newBusinessEngine(t, nil)
			index := &businessKnowledgeIndex{Local: knowledge.NewLocal()}
			engine.KB = index
			ctx := ports.WithAIRunIdentity(context.Background(), ports.AIRunIdentity{Username: "reader", Scopes: []string{auth.ScopeQueryAlarmList}})
			if err := repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: WorkflowOpsReport, RetrievalMode: "always", NoMatchPolicy: policy}); err != nil {
				t.Fatal(err)
			}
			received := installBusinessHarnessHTTP(t, engine, nil)
			_, err := engine.runBusinessWorkflow(ctx, "t1", WorkflowOpsReport, "", "高温 核实", "高温 核实", []string{"query_alarm_list", "query_knowledge_base"}, 2048)
			if len(index.Requests()) != 0 {
				t.Fatal("a caller without knowledge permission must not prefetch")
			}
			if policy == "require-evidence" {
				if err == nil || len(received) != 0 {
					t.Fatalf("required evidence without permission must fail before Harness: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			request := <-received
			claims, _ := aitest.Claims(request)
			if claims.HasScope(auth.ScopeQueryKnowledgeBase) || claims.Knowledge != nil || !strings.Contains(request.Question, "未授权知识库") {
				t.Fatalf("the default always policy must keep unauthorized runs free of knowledge: %#v", request)
			}
		})
	}
}

func TestBusinessKnowledgeEvidenceStaysInsideHarnessInputBudget(t *testing.T) {
	engine, _, _ := newBusinessEngine(t, nil)
	index := &businessKnowledgeIndex{Local: knowledge.NewLocal(), hits: []ports.KnowledgeHit{{DocumentID: "manual", ChunkID: "manual-1", WorkflowID: WorkflowProtocolAssist, Content: strings.Repeat("知识", 6000)}}}
	engine.KB = index
	received := installBusinessHarnessHTTP(t, engine, nil)
	if _, err := engine.runBusinessWorkflow(aitest.Context(context.Background()), "t1", WorkflowProtocolAssist, "", strings.Repeat("上", 7000), "上", []string{"query_knowledge_base"}, 2048); err != nil {
		t.Fatal(err)
	}
	request := <-received
	if len(request.Question) > 30<<10 || !utf8.ValidString(request.Question) || !strings.Contains(request.Question, "documentId=manual chunkId=manual-1") {
		t.Fatalf("bounded evidence must reach Harness with valid text and source references: %d bytes", len(request.Question))
	}
}

func TestBusinessKnowledgeFailuresNeverReachHarness(t *testing.T) {
	for _, failure := range []string{"missing-evidence", "retrieval-error", "unscoped-index", "mismatched-identity", "mismatched-run-tenant", "managed-missing-tenant", "oversized-input"} {
		t.Run(failure, func(t *testing.T) {
			engine, repo, _ := newBusinessEngine(t, nil)
			index := &businessKnowledgeIndex{Local: knowledge.NewLocal()}
			engine.KB = index
			ctx := aitest.Context(context.Background())
			question := "高温 核实"
			if err := repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "t1", WorkflowID: WorkflowProtocolAssist, RetrievalMode: "always", TopK: 5, NoMatchPolicy: "require-evidence"}); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "retrieval-error":
				index.err = errors.New("embedding API unavailable")
			case "unscoped-index":
				engine.KB = unscopedKnowledgeBase{}
			case "mismatched-identity":
				ctx = auth.ContextWithClaims(ctx, auth.Claims{TenantID: "t2", Username: "aitest"})
			case "mismatched-run-tenant":
				ctx = ports.WithAIRunIdentity(ctx, ports.AIRunIdentity{TenantID: "t2", Username: "reader", Scopes: []string{auth.ScopeQueryKnowledgeBase}})
			case "managed-missing-tenant":
				ctx = ports.WithAIRunIdentity(ctx, ports.AIRunIdentity{Username: "reader", ManagedUser: true, Scopes: []string{auth.ScopeQueryKnowledgeBase}})
			case "oversized-input":
				question = strings.Repeat("文", 11000)
			}
			received := installBusinessHarnessHTTP(t, engine, nil)
			if _, err := engine.runBusinessWorkflow(ctx, "t1", WorkflowProtocolAssist, "", question, question, []string{"query_knowledge_base"}, 2048); err == nil || len(received) != 0 {
				t.Fatalf("%s must fail before the first Harness request: %v", failure, err)
			}
			if strings.Contains(failure, "mismatched") || failure == "managed-missing-tenant" {
				if len(index.Requests()) != 0 {
					t.Fatal("a mismatched tenant must fail before retrieval")
				}
			}
		})
	}
}

func TestBusinessMissingKnowledgeAllowModelAndAlarmPrefetchGuards(t *testing.T) {
	engine, _, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return "结论", nil })
	engine.KB = nil
	if _, err := engine.runBusinessWorkflow(aitest.Context(context.Background()), "t1", WorkflowOpsReport, "", "高温", "高温", []string{"query_knowledge_base"}, 2048); err != nil {
		t.Fatal(err)
	}
	claims, _ := aitest.Claims(workflows.Last())
	if claims.HasScope(auth.ScopeQueryKnowledgeBase) || !strings.Contains(workflows.Last().Question, "知识库不可用") {
		t.Fatal("missing optional knowledge must be explicit and grant no tool scope")
	}
	ctx := ports.WithAIRunIdentity(context.Background(), ports.AIRunIdentity{Username: "reader", Scopes: []string{auth.ScopeQueryAlarmList}})
	count := len(workflows.Requests())
	if _, err := engine.runAlarmAnalysisWorkflow(ctx, model.Alarm{TenantID: "t1", ID: "alarm"}, nil, []string{"知识机密"}, true); err == nil || len(workflows.Requests()) != count {
		t.Fatalf("unauthorized preloaded alarm evidence must never reach Harness: %v", err)
	}
	index := &businessKnowledgeIndex{Local: knowledge.NewLocal()}
	engine.KB = index
	workflows.Answer = func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil }
	if _, err := engine.runAlarmAnalysisWorkflow(aitest.Context(context.Background()), model.Alarm{TenantID: "t1", ID: "alarm"}, nil, []string{"已检索告警证据"}, true); err != nil || len(index.Requests()) != 0 {
		t.Fatalf("already prefetched alarm knowledge must not be searched twice: %v", err)
	}
}

func TestAlarmEventsDoNotStartAnalysis(t *testing.T) {
	e, repo, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.StartWith(ctx, core.AllComponents()); err != nil {
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
	if saved, err := repo.GetAIAnalysis(ctx, alarm.TenantID, alarm.ID, model.AIAnalysisScopeNone); err != nil || saved.Summary != "研判完成" || saved.Model != "aitest-model" {
		t.Fatalf("explicit analysis not saved from the workflow answer: %#v err=%v", saved, err)
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

const analysisAnswer = `{"summary":"研判完成","possibleReasons":["现场存在烟雾"],"suggestions":["核实现场"],"riskLevel":"HIGH","confidence":0.8}`

// lastPrompt returns the prompt of the latest alarm analysis run.
func lastPrompt(w *aitest.Workflows) string { return w.Last().Question }

func newAlarmKnowledgeEngine(t *testing.T) (*testEngine, *memory.Repository, *local.Realtime, *aitest.Workflows) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realtime := local.NewRealtime()
	e := wrap(core.New(repo, archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))))
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
	e := wrap(&core.Engine{Repo: repo})

	history := e.alarmPropertyHistory(context.Background(), model.Alarm{TenantID: "t1", DeviceID: "d1", LastTriggeredAt: end}, nil, nil)
	if repo.calls != len(defaultAlarmHistoryProperties) {
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
	data, err := core.RenderHealthInspectionPDF(model.DeviceHealthReport{
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

	data, err := core.RenderHealthInspectionPDF(report)
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
	items := make([]model.DeviceHealthItem, core.InspectionPDFMaxDevices+500)
	for i := range items {
		items[i] = model.DeviceHealthItem{DeviceID: "device-" + strconv.Itoa(i), ProductID: "smoke", BusinessStatus: "NEVER_SEEN", Severity: "HIGH", Findings: []string{"设备尚未收到有效上报"}}
	}
	bounded, err := core.RenderHealthInspectionPDF(model.DeviceHealthReport{GeneratedAt: 1700000000000, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	limit, err := core.RenderHealthInspectionPDF(model.DeviceHealthReport{GeneratedAt: 1700000000000, Items: items[:core.InspectionPDFMaxDevices]})
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

func TestKnowledgeEvidenceFitsCharacterAndEscapedJSONBudgets(t *testing.T) {
	hits := []ports.KnowledgeHit{{DocumentID: "doc", ChunkID: "chunk", Content: strings.Repeat("引文\"\n", 6000)}}
	for _, prompt := range []string{strings.Repeat("上", 7000), strings.Repeat("x", 18000), strings.Repeat("\"\n", 5000)} {
		combined, err := core.AppendKnowledgeEvidence(prompt, hits, 30<<10)
		if err != nil {
			t.Fatal(err)
		}
		if err = core.ValidateAIInput(combined, 30<<10); err != nil || !strings.Contains(combined, "documentId=doc") {
			t.Fatalf("invalid evidence payload: %v", err)
		}
		wire, _ := json.Marshal(combined)
		if len(wire) > 30<<10 {
			t.Fatalf("escaped question exceeds wire budget: %d", len(wire))
		}
	}
	for _, prompt := range []string{strings.Repeat("x", 20001), strings.Repeat("\x00", 6000), strings.Repeat("🔥", 10001)} {
		if core.ValidateAIInput(prompt, 30<<10) == nil {
			t.Fatal("invalid gateway input budget accepted")
		}
	}
}

// Business runs have no conversation history and their own time limit; the
// MCP credential must outlive waiting for a Harness slot plus the run.
func TestBusinessRunsAreOneShotWithTheirOwnTimeout(t *testing.T) {
	e, _, workflows := newBusinessEngine(t, nil)
	e.BusinessRunTimeout = 7 * time.Minute
	if _, err := e.runBusinessWorkflow(aitest.Context(context.Background()), "t1", WorkflowOpsReport, "", "高温", "高温", []string{"query_alarm_list"}, 2048); err != nil {
		t.Fatal(err)
	}
	request := workflows.Last()
	if !request.OneShot || request.Timeout != 7*time.Minute {
		t.Fatalf("business run request %+v", request)
	}
	claims, err := aitest.Claims(request)
	if err != nil {
		t.Fatal(err)
	}
	if left := time.Until(claims.ExpiresAt.Time); left < businessRunCapacityWait+7*time.Minute {
		t.Fatalf("credential expires in %s, before a waiting run could finish", left)
	}
}

// Business features retrieve with a short question about their topic. Their
// prompt carries data (device snapshots, statistics) that must not be sent to
// the vector service: it can exceed the model's input limit and dilutes the
// match. Keyword-only evidence is marked in the prompt.
func TestBusinessFeaturesRetrieveWithShortQuestions(t *testing.T) {
	ctx := aitest.Context(context.Background())
	e, repo, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return "结论", nil })
	index := &businessKnowledgeIndex{Local: knowledge.NewLocal(), hits: []ports.KnowledgeHit{{DocumentID: "sop", ChunkID: "c1", WorkflowID: WorkflowHealthInspection, Content: "烟感离线处置", KeywordOnly: true}}}
	e.KB = index
	for i := 0; i < 300; i++ {
		id := fmt.Sprintf("smoke-detector-with-a-long-device-identifier-%03d", i)
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: "t1", ProductID: "smoke", Name: id, AccessKey: "key-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := e.InspectDeviceHealth(ctx, "t1")
	if err != nil || report.AIAdvice != "结论" {
		t.Fatalf("inspection %+v %v", report.Warnings, err)
	}
	requests := index.Requests()
	if len(requests) != 1 {
		t.Fatalf("retrievals %d", len(requests))
	}
	q := requests[0].Question
	if utf8.RuneCountInString(q) > ports.MaxKnowledgeQueryRunes || strings.Contains(q, "{") || strings.Contains(q, "smoke-detector-with") || !strings.Contains(q, "smoke") {
		t.Fatalf("inspection retrieval question %q", q)
	}
	if !strings.Contains(workflows.Last().Question, "仅按关键词匹配") {
		t.Fatal("keyword-only evidence is not marked")
	}
}

// Every business run leaves a record with its prompt version, sizes, usage
// and outcome, and updates the labeled AI metrics.
func TestBusinessRunsAreRecorded(t *testing.T) {
	failing := false
	engine, _, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) {
		if failing {
			return "", ports.ErrAIWorkflowStopped
		}
		return "结论", nil
	})
	store := memory.NewRepository()
	registry := metrics.New()
	engine.AIRuns, engine.Metrics, engine.KB = store, registry, nil
	workflows.Usage = &model.AIUsage{InputTokens: 120, OutputTokens: 30, CacheReadTokens: 7}
	workflows.WorkflowVersion = "1.0.0"
	ctx := aitest.Context(context.Background())
	if _, err := engine.runBusinessWorkflow(ctx, "t1", WorkflowOpsReport, aiprompt.OpsReportVersion, "高温", "高温", []string{"query_alarm_list"}, 2048); err != nil {
		t.Fatal(err)
	}
	failing = true
	if _, err := engine.runBusinessWorkflow(ctx, "t1", WorkflowOpsReport, aiprompt.OpsReportVersion, "高温", "高温", []string{"query_alarm_list"}, 2048); err == nil {
		t.Fatal("stopped run succeeded")
	}
	runs, total, err := store.ListAIRuns(context.Background(), ports.AIRunFilter{TenantID: "t1"})
	if err != nil || total != 2 {
		t.Fatalf("runs=%v total=%d err=%v", runs, total, err)
	}
	statuses := map[string]model.AIRunRecord{}
	for _, run := range runs {
		statuses[run.Status] = run
	}
	ok := statuses[model.AIRunSucceeded]
	if ok.WorkflowID != WorkflowOpsReport || ok.PromptVersion != aiprompt.OpsReportVersion+"@1.0.0" || ok.Actor != "aitest" || ok.Model != "aitest-model" ||
		ok.Usage.InputTokens != 120 || !ok.UsageReported || ok.ToolCalls != 1 || ok.OutputBytes != len("结论") || ok.InputBytes == 0 || ok.RunID == "" {
		t.Fatalf("succeeded record %+v", ok)
	}
	if stopped := statuses[model.AIRunStopped]; stopped.Error == "" {
		t.Fatalf("stopped record %+v", stopped)
	}
	usage, err := store.AIRunUsage(context.Background(), ports.AIRunFilter{TenantID: "t1"})
	if err != nil || len(usage) != 1 || usage[0].Runs != 2 || usage[0].Failed != 1 || usage[0].Usage.InputTokens != 240 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	exposition := registry.Prometheus()
	for _, line := range []string{
		`ai_run_total{workflow="ops-assistant",status="SUCCEEDED"} 1`,
		`ai_run_total{workflow="ops-assistant",status="STOPPED"} 1`,
		`ai_tokens_total{workflow="ops-assistant",kind="input"} 240`,
		`ai_run_duration_seconds_count{workflow="ops-assistant"} 2`,
	} {
		if !strings.Contains(exposition, line) {
			t.Errorf("missing metric %s", line)
		}
	}
}

// Products with a thing model are analysed by their numeric properties, the
// rule's fields first, with unit, range and thresholds attached.
func TestAlarmPropertyHistoryUsesThingModel(t *testing.T) {
	end := int64(100 * alarmHistoryDayMs)
	point := func(v any) []map[string]any { return []map[string]any{{"timestamp": end, "value": v}} }
	repo := &countingHistoryRepo{Repository: memory.NewRepository(), rows: map[string][]map[string]any{"pressure": point(0.12), "level": point(3.0), "door": point(true), "label": point("x")}}
	e := wrap(&core.Engine{Repo: repo})
	high, low, minimum := 1.2, 0.2, 0.0
	product := &model.Product{ThingModel: &model.ThingModel{Properties: []model.ThingField{
		{Identifier: "level", DataType: "number", Unit: "m"},
		{Identifier: "label", DataType: "string"},
		{Identifier: "door", DataType: "boolean"},
		{Identifier: "pressure", Name: "管网压力", DataType: "number", Unit: "MPa", Min: &minimum, AlarmLow: &low, AlarmHigh: &high},
	}}}
	history := e.alarmPropertyHistory(context.Background(), model.Alarm{TenantID: "t1", DeviceID: "d1", LastTriggeredAt: end}, product, []string{"properties.door"})
	if repo.calls != 3 || len(history) != 3 {
		t.Fatalf("calls=%d history=%v", repo.calls, history)
	}
	if history[0]["property"] != "door" || history[1]["property"] != "pressure" || history[2]["property"] != "level" {
		t.Fatalf("rule fields first, then thresholds: %v", history)
	}
	if p := history[1]; p["unit"] != "MPa" || p["name"] != "管网压力" || p["alarmLow"] != 0.2 || p["alarmHigh"] != 1.2 || p["min"] != 0.0 {
		t.Fatalf("missing thing-model semantics: %v", p)
	}
}

// A run the requester cancelled is recorded as stopped, not failed.
func TestAIRunStatusTreatsCancellationAsStopped(t *testing.T) {
	if got := AIRunStatus(context.Canceled); got != model.AIRunStopped {
		t.Fatalf("cancelled run recorded as %q", got)
	}
}

// Chat follows the business run steps: the credential outlives the Harness
// limit, the run is recorded with the chat prompt version, and the Harness
// conversation is bound to the tenant and user.
func TestRunChatCredentialAndRecord(t *testing.T) {
	engine, _, workflows := newBusinessEngine(t, func(ports.AIWorkflowRequest) (string, error) { return "回答", nil })
	store := memory.NewRepository()
	engine.AIRuns, engine.KB = store, nil
	engine.ChatRunTimeout = 3 * time.Minute
	workflows.WorkflowVersion = "2.0.0"
	result, err := engine.RunChat(aitest.Context(context.Background()), ChatRequest{TenantID: "t1", WorkflowID: "ops-assistant", ConversationID: "c1", Question: " 设备状态？ "}, nil)
	if err != nil || result.Answer != "回答" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	request := workflows.Last()
	claims, err := aitest.Claims(request)
	if err != nil {
		t.Fatal(err)
	}
	if ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time); ttl != 4*time.Minute || claims.Workflow != "" || claims.RunID != result.RunID {
		t.Fatalf("chat credential ttl=%s claims=%+v", ttl, claims)
	}
	if request.ConversationID != ChatConversationID("t1", "aitest", "c1") || request.OneShot {
		t.Fatalf("conversation not bound to tenant and user: %+v", request)
	}
	runs, _, err := store.ListAIRuns(context.Background(), ports.AIRunFilter{TenantID: "t1"})
	if err != nil || len(runs) != 1 || runs[0].PromptVersion != aiprompt.ChatVersion+"@2.0.0" {
		t.Fatalf("chat run record %+v err=%v", runs, err)
	}
	if _, err = engine.RunChat(aitest.Context(context.Background()), ChatRequest{TenantID: "t1", Question: "  "}, nil); err == nil {
		t.Fatal("empty question accepted")
	}
}

func TestChatRefusesBusinessAgents(t *testing.T) {
	for _, id := range BusinessWorkflowIDs() {
		_, err := (&Service{}).RunChat(context.Background(), ChatRequest{TenantID: "t", WorkflowID: id, Question: "列出设备"}, nil)
		var rejected *ports.AIRequestError
		if !errors.As(err, &rejected) || rejected.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: chat must refuse business agents, got %v", id, err)
		}
	}
}
