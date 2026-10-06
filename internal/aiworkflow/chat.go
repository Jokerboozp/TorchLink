package aiworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/aiprompt"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// defaultChatRunTimeout matches the Harness client's default for one turn.
const defaultChatRunTimeout = 90 * time.Second

// ChatRequest is one assistant turn. ConversationID is chosen by the browser;
// RunChat binds it to the tenant, user and access version before use.
type ChatRequest struct {
	TenantID       string
	WorkflowID     string
	ConversationID string
	Question       string
	Model          string
	MaxTokens      int
}

// RunChat runs one assistant turn for the identity in ctx. It follows the same
// steps as business runs (knowledge binding, prefetched evidence, a second
// permission check, a scoped MCP credential, run record) but keeps the
// multi-turn conversation and reports the knowledge prefetch through emit.
func (e *Service) RunChat(ctx context.Context, req ChatRequest, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	tenantID, workflowID := strings.TrimSpace(req.TenantID), strings.TrimSpace(req.WorkflowID)
	// Business agents serve platform features with their own permissions and
	// purposes; the assistant cannot borrow their persona or knowledge.
	if slices.Contains(BusinessWorkflowIDs(), workflowID) {
		return ports.AIWorkflowResult{}, ports.AIRejected(http.StatusUnprocessableEntity, "该智能体用于平台业务功能，不能用于对话")
	}
	if !e.AIWorkflowsReady() {
		return ports.AIWorkflowResult{}, ErrAIWorkflowsUnavailable
	}
	ctx, err := e.authorize(ctx, tenantID, "")
	if err != nil {
		return ports.AIWorkflowResult{}, err
	}
	identity, ok := ports.AIRunIdentityFrom(ctx)
	if !ok || identity.TenantID != "" && identity.TenantID != tenantID || identity.ManagedUser && identity.TenantID == "" {
		return ports.AIWorkflowResult{}, errors.New("AI 运行身份与当前租户不符，拒绝执行")
	}
	if err = e.admit(ctx, tenantID); err != nil {
		return ports.AIWorkflowResult{}, err
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return ports.AIWorkflowResult{}, ports.AIRejected(http.StatusUnprocessableEntity, "请输入问题")
	}
	if len(question) > 8000 {
		return ports.AIWorkflowResult{}, ports.AIRejected(http.StatusUnprocessableEntity, "问题超过 8000 字节，请缩短后重试")
	}
	runID := id("ai_run")
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		conversationID = runID
	}
	if identity.ManagedUser {
		// A changed grant starts a new Harness conversation, so earlier turns
		// that saw more data never reach a narrower session.
		conversationID += "\x00" + identity.AccessVersion
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	maxTokens = min(maxTokens, 8192)
	failed := func(err error) (ports.AIWorkflowResult, error) {
		return ports.AIWorkflowResult{RunID: runID, WorkflowID: workflowID}, err
	}
	binding, err := e.engine.Repo.GetWorkflowKnowledgeBinding(ctx, tenantID, workflowID)
	if err != nil {
		return failed(fmt.Errorf("读取智能体知识策略失败：%w", err))
	}
	if binding.TenantID == "" && binding.WorkflowID == "" {
		binding = DefaultWorkflowKnowledgeBinding(tenantID, workflowID)
	}
	if binding.TenantID != tenantID || binding.WorkflowID != workflowID {
		return failed(ports.AIRejected(http.StatusForbidden, "知识策略与当前租户或智能体不符，拒绝执行"))
	}
	knowledgeScope := ports.MCPToolScope("query_knowledge_base")
	scopes := append([]string(nil), identity.Scopes...)
	if !hasScope(scopes, knowledgeScope) {
		if binding.RetrievalMode != "disabled" && binding.NoMatchPolicy == "require-evidence" {
			return failed(ports.AIRejected(http.StatusForbidden, "当前用户无此智能体所需的知识库访问权限"))
		}
		binding.RetrievalMode = "disabled"
	}
	prompt := question
	var knowledge *ports.AIKnowledgeRunScope
	if binding.RetrievalMode == "disabled" {
		scopes = withoutScope(scopes, knowledgeScope)
		prompt += aiprompt.KnowledgeDisabled
	} else {
		knowledge = &ports.AIKnowledgeRunScope{WorkflowID: binding.WorkflowID, TopK: binding.TopK, MinScore: binding.MinScore}
		prompt += KnowledgeBindingInstruction(binding)
		if e.engine.KB == nil {
			if binding.NoMatchPolicy == "require-evidence" {
				return failed(ports.AIRejected(http.StatusServiceUnavailable, "此智能体要求知识证据，但知识库不可用"))
			}
			prompt += aiprompt.KnowledgeUnavailable
		} else if binding.RetrievalMode == "auto" && binding.NoMatchPolicy != "require-evidence" {
			// On demand: the assistant calls the knowledge tool when the
			// question needs it instead of a search before every turn.
			prompt += aiprompt.KnowledgeOnDemand
		} else if prompt, err = e.prefetchChatKnowledge(ctx, tenantID, runID, workflowID, question, prompt, binding, emit); err != nil {
			return failed(err)
		}
	}
	// Retrieval may outlive a permission change: re-check before any evidence
	// or device data reaches the model.
	if ctx, err = e.authorize(ctx, tenantID, ""); err != nil {
		return failed(err)
	}
	if identity, ok = ports.AIRunIdentityFrom(ctx); !ok {
		return failed(errors.New("缺少 AI 运行身份，拒绝执行"))
	}
	if err = core.ValidateAIInput(prompt, 30<<10); err != nil {
		return failed(err)
	}
	token, err := e.engine.HarnessTokens.IssueChatRunToken(tenantID, identity, runID, scopes, knowledge, e.chatRunTimeout()+time.Minute)
	if err != nil {
		return failed(fmt.Errorf("签发 AI 运行凭据失败：%w", err))
	}
	started := time.Now()
	result, err := e.engine.AIWorkflows.StreamChat(ctx, ports.AIWorkflowRequest{
		TenantID: tenantID, Actor: identity.Username, RunID: runID, WorkflowID: workflowID,
		ConversationID: ChatConversationID(tenantID, identity.Username, conversationID),
		Question:       prompt, Model: strings.TrimSpace(req.Model), MaxTokens: maxTokens, MCPToken: token,
	}, emit)
	if result.RunID == "" {
		result.RunID = runID
	}
	e.RecordAIRun(AIRunMeta{TenantID: tenantID, Actor: identity.Username, WorkflowID: workflowID, PromptVersion: aiprompt.ChatVersion, Model: strings.TrimSpace(req.Model), InputBytes: len(prompt), StartedAt: started}, result, err)
	e.saveChatTurn(ctx, identity, req, question, started, result, err)
	return result, err
}

// saveChatTurn stores the question and the (possibly partial) answer in the
// user's conversation. Storing never fails the turn the user already saw.
func (e *Service) saveChatTurn(ctx context.Context, identity ports.AIRunIdentity, req ChatRequest, question string, started time.Time, result ports.AIWorkflowResult, runErr error) {
	conversationID := strings.TrimSpace(req.ConversationID)
	if e.engine.AIConversations == nil || !validConversationID(conversationID) {
		return
	}
	now := time.Now().UnixMilli()
	answer := result.Answer
	if runErr != nil && strings.TrimSpace(answer) == "" {
		answer = "运行未能完成。"
	}
	conversation := model.AIConversation{ID: conversationID, TenantID: strings.TrimSpace(req.TenantID), Actor: identity.Username, WorkflowID: strings.TrimSpace(req.WorkflowID),
		Title: truncateRunes(question, 40), AccessVersion: identity.AccessVersion, UpdatedAt: now}
	messages := []model.AIConversationMessage{
		{Role: "user", Text: question, Status: model.AIRunSucceeded, CreatedAt: started.UnixMilli()},
		{Role: "assistant", Text: truncateRunes(answer, 64<<10), RunID: result.RunID, Status: AIRunStatus(runErr), CreatedAt: now},
	}
	// The browser may stop the request; the turn is still saved.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := e.engine.AIConversations.AppendAIConversationTurn(saveCtx, conversation, messages); err != nil && e.engine.Log != nil {
		e.engine.Log.Warn("save assistant conversation", "conversation", conversationID, "error", err)
	}
}

// validConversationID accepts the browser's generated conversation ids.
func validConversationID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r)) {
			return false
		}
	}
	return true
}

// prefetchChatKnowledge appends the Agent's bound evidence to prompt and tells
// the browser, through emit, which documents were cited.
func (e *Service) prefetchChatKnowledge(ctx context.Context, tenantID, runID, workflowID, question, prompt string, binding model.WorkflowKnowledgeBinding, emit func(ports.AIWorkflowEvent) error) (string, error) {
	callID := id("knowledge_prefetch")
	if emit != nil {
		_ = emit(ports.AIWorkflowEvent{Type: "tool.started", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Data: map[string]any{"inputSummary": "按智能体知识策略预检知识库"}})
	}
	hits, err := SearchWorkflowKnowledge(ctx, e.engine.KB, tenantID, ports.BoundKnowledgeQuery(question), binding)
	success := err == nil
	if emit != nil {
		_ = emit(ports.AIWorkflowEvent{Type: "tool.completed", RunID: runID, WorkflowID: workflowID, Tool: "query_knowledge_base", CallID: callID, Success: &success, Data: map[string]any{"outputSummary": fmt.Sprintf("召回 %d 条绑定知识", len(hits)), "sources": KnowledgeSources(hits)}})
	}
	if err != nil {
		return prompt, fmt.Errorf("检索智能体绑定知识失败：%w", err)
	}
	if len(hits) == 0 && binding.NoMatchPolicy == "require-evidence" {
		return prompt, ports.AIRejected(http.StatusUnprocessableEntity, "此智能体要求匹配知识证据，但未检索到匹配内容")
	}
	if KeywordOnlyHits(hits) {
		prompt += aiprompt.KnowledgeKeywordOnly
	}
	if len(hits) == 0 {
		return prompt + aiprompt.KnowledgeNoMatch, nil
	}
	prompt, err = core.AppendKnowledgeEvidence(prompt, hits, 30<<10)
	if err != nil {
		return prompt, fmt.Errorf("知识证据超过 AI 输入预算：%w", err)
	}
	return prompt, nil
}

func (e *Service) chatRunTimeout() time.Duration {
	if e.engine.ChatRunTimeout > 0 {
		return e.engine.ChatRunTimeout
	}
	return defaultChatRunTimeout
}

// ChatConversationID derives the Harness conversation from the browser's id
// so one user's conversation can never be resumed by another tenant or user.
func ChatConversationID(tenantID, username, conversationID string) string {
	sum := sha256.Sum256([]byte(tenantID + "\x00" + username + "\x00" + conversationID))
	return "conv_" + hex.EncodeToString(sum[:])
}

// KnowledgeBindingInstruction tells the model how the Agent's knowledge
// policy applies to this turn.
func KnowledgeBindingInstruction(binding model.WorkflowKnowledgeBinding) string {
	payload, _ := json.Marshal(map[string]any{"mode": binding.RetrievalMode, "workflowId": binding.WorkflowID, "topK": binding.TopK, "minScore": binding.MinScore, "noMatchPolicy": binding.NoMatchPolicy})
	return aiprompt.KnowledgeBinding(payload)
}

// KnowledgeSources lists where prefetched evidence came from so the browser
// can cite it; passage text stays with the model.
func KnowledgeSources(hits []ports.KnowledgeHit) []any {
	sources := make([]any, 0, len(hits))
	for _, hit := range hits {
		sources = append(sources, map[string]any{"documentId": hit.DocumentID, "filename": hit.Filename, "chunkIndex": hit.ChunkIndex, "score": math.Round(hit.Score*100) / 100})
	}
	return sources
}

func hasScope(scopes []string, scope string) bool {
	for _, value := range scopes {
		if value == scope {
			return true
		}
	}
	return false
}

func withoutScope(scopes []string, scope string) []string {
	out := make([]string, 0, len(scopes))
	for _, value := range scopes {
		if value != scope {
			out = append(out, value)
		}
	}
	return out
}
