package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

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
			s.aiProblem(w, r, err)
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
		problem(w, http.StatusServiceUnavailable, aiworkflow.ErrAIWorkflowsUnavailable.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.failure(w, r, errors.New("response writer does not support streaming"), "streaming is not supported")
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
			failure := s.aiFailureOf(r, err)
			_ = stream.event(ports.AIWorkflowEvent{Type: "run.failed", RunID: result.RunID, Code: failure.code, Message: failure.message})
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

// runAIWorkflow runs one assistant turn as the caller; the orchestration
// lives in aiworkflow so chat and business runs follow the same steps.
func (s *Server) runAIWorkflow(ctx context.Context, c auth.Claims, question, workflowID, conversationID, modelName string, maxTokens int, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
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
	return s.ai.RunChat(aiRunContext(ctx, c), aiworkflow.ChatRequest{TenantID: c.TenantID, WorkflowID: workflowID, ConversationID: conversationID, Question: question, Model: modelName, MaxTokens: maxTokens}, emit)
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
		s.aiProblem(w, r, err)
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
		s.internalError(w, r, presentationErr)
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
		s.aiProblem(w, r, err)
		return
	}
	write(w, 200, map[string]any{"period": in.Period, "start": in.Start, "end": in.End, "report": report})
}

type aiFailure struct {
	status    int
	code      string
	message   string
	retryable bool
}

// aiFailureOf maps an AI run error to what the user sees: reasons the user
// can act on keep their message, a busy Harness asks to retry, and model or
// gateway failures get a generic message with a logged reference.
func (s *Server) aiFailureOf(r *http.Request, err error) aiFailure {
	var rejected *ports.AIRequestError
	switch {
	case errors.As(err, &rejected):
		return aiFailure{status: rejected.Status, code: "AI_REQUEST_REJECTED", message: rejected.Message}
	case errors.Is(err, ports.ErrAIWorkflowBusy):
		return aiFailure{status: http.StatusTooManyRequests, code: "AI_BUSY", message: "AI 工作流服务繁忙，请稍后重试", retryable: true}
	case errors.Is(err, aiworkflow.ErrAIWorkflowsUnavailable):
		return aiFailure{status: http.StatusServiceUnavailable, code: "AI_UNAVAILABLE", message: err.Error()}
	case errors.Is(err, core.ErrAIInputTooLarge):
		return aiFailure{status: http.StatusUnprocessableEntity, code: "AI_INPUT_TOO_LARGE", message: core.ErrAIInputTooLarge.Error()}
	case errors.Is(err, context.DeadlineExceeded):
		return aiFailure{status: http.StatusGatewayTimeout, code: "AI_TIMEOUT", message: "AI 工作流处理超时，请稍后重试", retryable: true}
	}
	reference := requestIDFrom(r.Context())
	if reference == "" {
		reference = randomHex(6)
	}
	if s.log != nil {
		s.log.ErrorContext(r.Context(), "AI workflow request failed", "reference", reference, "path", r.URL.Path, "error", err)
	}
	return aiFailure{status: http.StatusBadGateway, code: "AI_WORKFLOW_FAILED", message: "AI 工作流请求失败（编号 " + reference + "）", retryable: true}
}

func (s *Server) aiProblem(w http.ResponseWriter, r *http.Request, err error) {
	failure := s.aiFailureOf(r, err)
	if failure.status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "5")
	}
	write(w, failure.status, map[string]any{"type": "about:blank", "title": http.StatusText(failure.status), "status": failure.status, "code": failure.code, "detail": failure.message, "retryable": failure.retryable})
}
