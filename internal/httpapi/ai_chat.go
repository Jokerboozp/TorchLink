package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/aiprompt"
	"iot-platform/internal/aiworkflow"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// aiAnalysis returns the newest analysis variant the caller's role may read.
// Knowledge-based variants stay hidden from roles without knowledge access.
func (s *Server) aiAnalysis(w http.ResponseWriter, r *http.Request) {
	var latest model.AIAnalysis
	found := false
	for _, scope := range alarmAnalysisViewScopes(r.Context()) {
		v, err := s.engine.Repo.GetAIAnalysis(r.Context(), claims(r).TenantID, r.PathValue("alarmId"), scope)
		if errors.Is(err, model.ErrNotFound) {
			continue
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if !found || v.CreatedAt > latest.CreatedAt {
			latest, found = v, true
		}
	}
	if !found {
		problem(w, 404, "analysis not found or still pending")
		return
	}
	write(w, 200, latest)
}

func (s *Server) aiChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question       string `json:"question"`
		Workflow       string `json:"workflow,omitempty"`
		WorkflowID     string `json:"workflowId,omitempty"`
		ConversationID string `json:"conversationId,omitempty"`
		Model          string `json:"model,omitempty"`
		MaxTokens      int    `json:"maxTokens,omitempty"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if s.engine.AIWorkflows != nil {
		workflowID := in.WorkflowID
		if workflowID == "" {
			workflowID = in.Workflow
		}
		result, err := s.runAIWorkflow(r.Context(), claims(r), in.Question, workflowID, in.ConversationID, in.Model, in.MaxTokens, nil)
		if err != nil {
			if s.log != nil {
				s.log.Warn("run AI workflow failed", "error", err)
			}
			problem(w, 502, "AI workflow request failed")
			return
		}
		write(w, 200, result)
		return
	}
	problem(w, http.StatusServiceUnavailable, aiworkflow.ErrAIWorkflowsUnavailable.Error())
}

func (s *Server) aiChatStream(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question       string `json:"question"`
		Workflow       string `json:"workflow,omitempty"`
		WorkflowID     string `json:"workflowId,omitempty"`
		ConversationID string `json:"conversationId,omitempty"`
		Model          string `json:"model,omitempty"`
		MaxTokens      int    `json:"maxTokens,omitempty"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if s.engine.AIWorkflows == nil {
		problem(w, http.StatusServiceUnavailable, "未配置 AI 工作流服务（Harness），智能助手问答暂不可用")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	workflowID := in.WorkflowID
	if workflowID == "" {
		workflowID = in.Workflow
	}
	stream := newSSEWriter(w, flusher)
	defer stream.close()
	go stream.heartbeat(r.Context(), sseHeartbeatInterval)
	terminal := false
	result, err := s.runAIWorkflow(r.Context(), claims(r), in.Question, workflowID, in.ConversationID, in.Model, in.MaxTokens, func(event ports.AIWorkflowEvent) error {
		if event.Type == "run.completed" || event.Type == "run.failed" {
			terminal = true
		}
		if err := stream.event(sanitizeWorkflowEvent(event)); err != nil {
			return err
		}
		return r.Context().Err()
	})
	if err != nil {
		if r.Context().Err() == nil && !terminal {
			_ = stream.event(ports.AIWorkflowEvent{Type: "run.failed", RunID: result.RunID, Code: "workflow_failed", Message: "AI workflow request failed"})
		}
		return
	}
	if !terminal {
		_ = stream.event(ports.AIWorkflowEvent{Type: "run.completed", RunID: result.RunID, WorkflowID: result.WorkflowID, Model: result.Model, Answer: result.Answer})
	}
}

// sseHeartbeatInterval keeps proxies from closing a stream while the model
// is still thinking and no event has been sent yet.
var sseHeartbeatInterval = 20 * time.Second

// sseWriter serialises workflow events and heartbeat comments on one stream.
// close waits for the heartbeat so nothing writes after the handler returns.
type sseWriter struct {
	mu      sync.Mutex
	w       io.Writer
	flusher http.Flusher
	done    chan struct{}
	stopped chan struct{}
}

func newSSEWriter(w io.Writer, flusher http.Flusher) *sseWriter {
	return &sseWriter{w: w, flusher: flusher, done: make(chan struct{}), stopped: make(chan struct{})}
}

func (s *sseWriter) event(event ports.AIWorkflowEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeWorkflowSSE(s.w, event); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *sseWriter) heartbeat(ctx context.Context, every time.Duration) {
	defer close(s.stopped)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			_, err := io.WriteString(s.w, ": keepalive\n\n")
			if err == nil {
				s.flusher.Flush()
			}
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (s *sseWriter) close() {
	close(s.done)
	<-s.stopped
}

func (s *Server) runAIWorkflow(ctx context.Context, c auth.Claims, question, workflowID, conversationID, modelName string, maxTokens int, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	ctx = aiRunContext(ctx, c)
	var authErr error
	ctx, authErr = s.authorizeAIRun(ctx, c.TenantID, "")
	if authErr != nil {
		return ports.AIWorkflowResult{}, authErr
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return ports.AIWorkflowResult{}, errors.New("question is required")
	}
	if len(question) > 8000 {
		return ports.AIWorkflowResult{}, errors.New("question exceeds 8000 bytes")
	}
	if s.aiProviderRuntime != nil {
		// The selected Provider owns the model for every AI surface. Keep the
		// browser's run form from sending a stale per-workflow model override.
		config := s.aiProviderRuntime.CurrentConfig()
		if configuredModel := strings.TrimSpace(config.Model); configuredModel != "" {
			modelName = configuredModel
		}
		if maxTokens <= 0 {
			maxTokens = effectiveAIMaxTokens(config.MaxTokens)
		}
	}
	knowledgeQuestion := question
	runID := "ai_run_" + randomHex(10)
	if conversationID == "" {
		conversationID = runID
	}
	if c.TokenUse == "user" {
		conversationID += "\x00" + requestAccessVersion(ctx, c)
	}
	conversationID = harnessConversationID(c.TenantID, c.Username, conversationID)
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	if maxTokens > 8192 {
		maxTokens = 8192
	}
	binding, err := s.engine.Repo.GetWorkflowKnowledgeBinding(ctx, c.TenantID, strings.TrimSpace(workflowID))
	if err != nil {
		return ports.AIWorkflowResult{RunID: runID}, fmt.Errorf("load workflow knowledge binding: %w", err)
	}
	if binding.WorkflowID == "" {
		binding = defaultWorkflowKnowledgeBinding(c.TenantID, strings.TrimSpace(workflowID))
	}
	scopes := workflowScopes(ctx)
	if len(intersectScopes(scopes, []string{auth.ScopeQueryKnowledgeBase})) == 0 {
		if binding.RetrievalMode != "disabled" && binding.NoMatchPolicy == "require-evidence" {
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("当前用户无此工作流所需的知识库访问权限")
		}
		binding.RetrievalMode = "disabled"
	}
	var knowledgeScope *auth.KnowledgeScope
	if binding.RetrievalMode == "disabled" {
		filteredScopes := make([]string, 0, len(scopes))
		for _, scope := range scopes {
			if scope != auth.ScopeQueryKnowledgeBase {
				filteredScopes = append(filteredScopes, scope)
			}
		}
		scopes = filteredScopes
		question += aiprompt.KnowledgeDisabled
	} else {
		knowledgeScope = &auth.KnowledgeScope{WorkflowID: binding.WorkflowID, TopK: binding.TopK, MinScore: binding.MinScore}
		question += workflowKnowledgeInstruction(binding)
	}
	if binding.RetrievalMode != "disabled" && s.engine.KB == nil {
		if binding.NoMatchPolicy == "require-evidence" {
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("workflow knowledge base is unavailable")
		}
		question += aiprompt.KnowledgeUnavailable
	}
	if binding.RetrievalMode != "disabled" && s.engine.KB != nil {
		callID := "knowledge_prefetch_" + randomHex(6)
		if emit != nil {
			_ = emit(ports.AIWorkflowEvent{Type: "tool.started", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Data: map[string]any{"inputSummary": "按工作流绑定策略预检知识库"}})
		}
		hits, searchErr := s.searchWorkflowKnowledge(ctx, c.TenantID, knowledgeQuestion, binding)
		success := searchErr == nil
		if emit != nil {
			_ = emit(ports.AIWorkflowEvent{Type: "tool.completed", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Success: &success, Data: map[string]any{"outputSummary": fmt.Sprintf("召回 %d 条绑定知识", len(hits)), "sources": knowledgeSources(hits)}})
		}
		if searchErr != nil {
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, fmt.Errorf("prefetch workflow knowledge: %w", searchErr)
		}
		if len(hits) == 0 && binding.NoMatchPolicy == "require-evidence" {
			return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, errors.New("workflow requires matching knowledge evidence")
		}
		if aiworkflow.KeywordOnlyHits(hits) {
			question += aiprompt.KnowledgeKeywordOnly
		}
		if len(hits) > 0 {
			question, err = core.AppendKnowledgeEvidence(question, hits, 30<<10)
			if err != nil {
				return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, err
			}
		} else {
			question += aiprompt.KnowledgeNoMatch
		}
	}
	ctx, err = s.authorizeAIRun(ctx, c.TenantID, "")
	if err != nil {
		return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, err
	}
	if err = core.ValidateAIInput(question, 30<<10); err != nil {
		return ports.AIWorkflowResult{RunID: runID}, err
	}
	mcpToken, err := s.auth.IssueHarnessForIdentity(c, runID, scopes, knowledgeScope, 2*time.Minute)
	if err != nil {
		return ports.AIWorkflowResult{RunID: runID}, fmt.Errorf("issue harness token: %w", err)
	}
	started := time.Now()
	result, err := s.engine.AIWorkflows.StreamChat(ctx, ports.AIWorkflowRequest{TenantID: c.TenantID, Actor: c.Username, RunID: runID, ConversationID: strings.TrimSpace(conversationID), WorkflowID: strings.TrimSpace(workflowID), Question: question, Model: strings.TrimSpace(modelName), MaxTokens: maxTokens, MCPToken: mcpToken}, emit)
	if result.RunID == "" {
		result.RunID = runID
	}
	s.ai.RecordAIRun(aiworkflow.AIRunMeta{TenantID: c.TenantID, Actor: c.Username, WorkflowID: strings.TrimSpace(workflowID), Model: strings.TrimSpace(modelName), InputBytes: len(question), StartedAt: started}, result, err)
	return result, err
}

func harnessConversationID(tenantID, username, conversationID string) string {
	sum := sha256.Sum256([]byte(tenantID + "\x00" + username + "\x00" + conversationID))
	return "conv_" + hex.EncodeToString(sum[:])
}

func sanitizeWorkflowEvent(event ports.AIWorkflowEvent) ports.AIWorkflowEvent {
	event.Data = sanitizeWorkflowData(event.Data)
	return event
}

func sanitizeWorkflowData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		canonicalKey := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
		if canonicalKey == "conversationid" || canonicalKey == "sessionid" || canonicalKey == "authorization" || canonicalKey == "apikey" ||
			strings.HasSuffix(canonicalKey, "token") || strings.Contains(canonicalKey, "secret") || strings.Contains(canonicalKey, "password") ||
			strings.Contains(canonicalKey, "credential") || strings.Contains(canonicalKey, "cookie") {
			continue
		}
		out[key] = sanitizeWorkflowValue(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeWorkflowValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeWorkflowData(typed)
	case []any:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = sanitizeWorkflowValue(item)
		}
		return items
	default:
		return value
	}
}

func writeWorkflowSSE(w io.Writer, event ports.AIWorkflowEvent) error {
	if !allowedWorkflowEvent(event.Type) {
		return fmt.Errorf("unsupported workflow event type %q", event.Type)
	}
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(b) > 64<<10 {
		return errors.New("workflow event exceeds 64 KiB")
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, b)
	return err
}

func allowedWorkflowEvent(eventType string) bool {
	switch eventType {
	case "run.started", "text.delta", "tool.started", "tool.completed", "run.completed", "run.failed":
		return true
	default:
		return false
	}
}

func (s *Server) aiRuleDraft(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	rule, err := s.ai.DraftRule(aiRunContext(r.Context(), claims(r)), claims(r).TenantID, in.Text) /* 由 rule-drafter 工作流生成。 */
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	c := claims(r)
	rule.TenantID = c.TenantID
	rule.Enabled = false
	// AI drafts always start from the executable JSON condition form. A model
	// response must not smuggle an already-active Gengine expression into the
	// editor; the generated alternative is shown as a commented placeholder.
	rule.Expression = ""
	if rule.ID == "" {
		rule.ID = "rule_draft_" + randomHex(8)
	}
	if rule.Match == "" {
		rule.Match = "all"
	}
	if rule.Version == 0 {
		rule.Version = 1
	}
	if rule.Level == "" {
		rule.Level = "MEDIUM"
	}
	warnings, conflicts, validationErr := s.engine.ValidateRuleDraft(r.Context(), rule)
	if validationErr != nil {
		_ = s.engine.Repo.SaveAIToolCall(r.Context(), model.AIToolCallLog{ID: "tool_" + randomHex(8), TenantID: c.TenantID, Actor: c.Username, Tool: "ai.rule_draft.validate", Input: map[string]any{"text": in.Text}, Output: rule, Success: false, Error: validationErr.Error(), CreatedAt: time.Now().UnixMilli()})
		problem(w, 422, validationErr.Error())
		return
	}
	presentation, presentationErr := core.PresentRule(rule)
	if presentationErr != nil {
		problem(w, http.StatusInternalServerError, presentationErr.Error())
		return
	}
	s.engine.RecordAudit(r.Context(), model.AuditLog{ID: fmt.Sprintf("audit_%d", time.Now().UnixNano()), TenantID: c.TenantID, Actor: c.Username, Action: "ai.rule_draft", TargetType: "rule", TargetID: rule.ID, Details: map[string]any{"success": true}, CreatedAt: time.Now().UnixMilli()})
	_ = s.engine.Repo.SaveAIToolCall(r.Context(), model.AIToolCallLog{ID: "tool_" + randomHex(8), TenantID: c.TenantID, Actor: c.Username, Tool: "ai.rule_draft", Input: map[string]any{"text": in.Text}, Output: rule, Success: true, CreatedAt: time.Now().UnixMilli()})
	write(w, 200, map[string]any{"draft": rule, "presentation": presentation, "requiresHumanApproval": true, "schemaValid": true, "warnings": warnings, "conflicts": conflicts})
}

func (s *Server) aiReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Period string `json:"period"`
		Start  int64  `json:"start"`
		End    int64  `json:"end"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if in.Period == "" {
		in.Period = "日报"
	}
	report, err := s.ai.GenerateReport(aiRunContext(r.Context(), claims(r)), claims(r).TenantID, in.Period, in.Start, in.End)
	if err != nil {
		problem(w, 502, err.Error())
		return
	}
	write(w, 200, map[string]any{"period": in.Period, "start": in.Start, "end": in.End, "report": report})
}
