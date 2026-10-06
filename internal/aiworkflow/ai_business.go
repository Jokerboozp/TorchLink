package aiworkflow

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/aiprompt"
	"iot-platform/internal/core"
	"strings"
	"time"

	"iot-platform/internal/aioutput"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Business AI features run as Harness workflows so every model call shares the
// same Agent persona, tool policy, MCP permission checks and run records.
const (
	WorkflowAlarmAnalysis    = model.AlarmAnalysisWorkflowID
	WorkflowHealthInspection = "device-health-inspector"
	WorkflowOpsReport        = "ops-assistant"
	WorkflowProtocolAssist   = "protocol-assistant"
	WorkflowRuleDraft        = "rule-drafter"

	// defaultBusinessRunTimeout applies when the engine sets none; business
	// runs produce longer outputs than interactive chat.
	defaultBusinessRunTimeout = 4 * time.Minute
)

// ErrAIWorkflowsUnavailable is returned when the required Harness is missing.
var ErrAIWorkflowsUnavailable = errors.New("AI 工作流服务（Harness）未配置，无法执行智能功能")

// BusinessWorkflowIDs lists the non-chat Agents used by platform features.
func BusinessWorkflowIDs() []string {
	return []string{WorkflowAlarmAnalysis, WorkflowHealthInspection, WorkflowProtocolAssist, WorkflowRuleDraft}
}

// AIWorkflowsReady reports whether business AI features can run.
func (e *Service) AIWorkflowsReady() bool {
	return e.engine.AIWorkflows != nil && e.engine.HarnessTokens != nil
}

// runBusinessWorkflow runs one business Agent for the identity in ctx. The
// token carries only the requested tool scopes the identity may use; the
// knowledge tool follows the Agent's knowledge binding like chat. query is the
// short retrieval question for prefetched evidence; the prompt itself carries
// data (snapshots, statistics, samples) and is never used as one.
func (e *Service) runBusinessWorkflow(ctx context.Context, tenantID, workflowID, promptVersion, prompt, query string, tools []string, maxTokens int) (ports.AIWorkflowResult, error) {
	if !e.AIWorkflowsReady() {
		return ports.AIWorkflowResult{}, ErrAIWorkflowsUnavailable
	}
	if e.Authorizer != nil {
		var err error
		ctx, err = e.authorize(ctx, tenantID, workflowID)
		if err != nil {
			return ports.AIWorkflowResult{}, err
		}
	}
	identity, ok := ports.AIRunIdentityFrom(ctx)
	if !ok {
		return ports.AIWorkflowResult{}, errors.New("缺少 AI 运行身份，拒绝执行")
	}
	if identity.TenantID != "" && identity.TenantID != tenantID || identity.ManagedUser && identity.TenantID == "" {
		return ports.AIWorkflowResult{}, errors.New("AI 运行身份与当前租户不符，拒绝执行")
	}
	if err := core.ValidateAIInput(prompt, 30<<10); err != nil {
		return ports.AIWorkflowResult{}, err
	}
	if err := e.admit(ctx, tenantID); err != nil {
		return ports.AIWorkflowResult{}, err
	}
	if claims, present := auth.ClaimsFromContext(ctx); present && (claims.TenantID != tenantID || claims.Username != identity.Username || identity.ManagedUser && claims.SessionVersion != identity.SessionVersion) {
		return ports.AIWorkflowResult{}, errors.New("AI 运行身份与当前租户或会话不符，拒绝执行")
	}
	allowed := map[string]bool{}
	for _, scope := range identity.Scopes {
		allowed[scope] = true
	}
	scopes := []string{}
	for _, tool := range tools {
		if scope := ports.MCPToolScope(tool); allowed[scope] {
			scopes = append(scopes, scope)
		}
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(workflowID) == "" {
		return ports.AIWorkflowResult{}, errors.New("缺少 AI 租户或工作流，拒绝执行")
	}
	binding, err := e.engine.Repo.GetWorkflowKnowledgeBinding(ctx, tenantID, workflowID)
	if err != nil {
		return ports.AIWorkflowResult{}, fmt.Errorf("读取 %s 知识策略失败：%w", workflowID, err)
	}
	if binding.TenantID == "" && binding.WorkflowID == "" {
		binding = DefaultWorkflowKnowledgeBinding(tenantID, workflowID)
	}
	if binding.TenantID != tenantID || binding.WorkflowID != workflowID {
		return ports.AIWorkflowResult{}, errors.New("知识策略与当前租户或工作流不符，拒绝执行")
	}
	knowledgeScope := ports.MCPToolScope("query_knowledge_base")
	knowledgeToolRequested := false
	for _, scope := range scopes {
		if scope == knowledgeScope {
			knowledgeToolRequested = true
			break
		}
	}
	// Platform prefetch does not add tools to the Agent manifest. In particular,
	// rule drafts may receive authorized evidence without gaining a knowledge tool.
	useKnowledge := binding.RetrievalMode != "disabled" && allowed[knowledgeScope] && (workflowID != WorkflowAlarmAnalysis || knowledgeToolRequested)
	if !useKnowledge && binding.NoMatchPolicy == "require-evidence" {
		return ports.AIWorkflowResult{}, errors.New("当前运行未获此工作流所需的知识库访问权限，拒绝无证据执行")
	}
	if useKnowledge && e.engine.KB == nil {
		if binding.NoMatchPolicy == "require-evidence" {
			return ports.AIWorkflowResult{}, errors.New("此工作流要求知识证据，但知识库不可用")
		}
		useKnowledge = false
		prompt += aiprompt.KnowledgeUnavailable
	}
	var knowledge *ports.AIKnowledgeRunScope
	if useKnowledge {
		if query = ports.BoundKnowledgeQuery(query); workflowID != WorkflowAlarmAnalysis && query != "" {
			hits, err := SearchWorkflowKnowledge(ctx, e.engine.KB, tenantID, query, binding)
			if err != nil {
				return ports.AIWorkflowResult{}, fmt.Errorf("检索 %s 绑定知识失败：%w", workflowID, err)
			}
			if KeywordOnlyHits(hits) {
				prompt += aiprompt.KnowledgeKeywordOnly
			}
			if len(hits) == 0 && binding.NoMatchPolicy == "require-evidence" {
				return ports.AIWorkflowResult{}, errors.New("此工作流要求匹配知识证据，但未检索到匹配内容")
			}
			if len(hits) > 0 {
				prompt, err = core.AppendKnowledgeEvidence(prompt, hits, 30<<10)
				if err != nil {
					return ports.AIWorkflowResult{}, fmt.Errorf("知识证据超过 AI 输入预算：%w", err)
				}
			} else {
				prompt += aiprompt.KnowledgeNoMatch
			}
		}
		if knowledgeToolRequested {
			knowledge = &ports.AIKnowledgeRunScope{WorkflowID: workflowID, TopK: binding.TopK, MinScore: binding.MinScore}
		}
	}
	if knowledge == nil {
		filtered := scopes[:0]
		for _, scope := range scopes {
			if scope != knowledgeScope {
				filtered = append(filtered, scope)
			}
		}
		scopes = filtered
	}
	runID := id("ai_run")
	// Retrieval may outlive a permission change. Reject its entire prompt before
	// sending evidence or device data to the model if the account grant changed.
	if e.Authorizer != nil {
		ctx, err = e.authorize(ctx, tenantID, workflowID)
		if err != nil {
			return ports.AIWorkflowResult{RunID: runID}, err
		}
		identity, _ = ports.AIRunIdentityFrom(ctx)
	}
	timeout := e.businessRunTimeout()
	// The MCP credential must outlive waiting for a Harness slot and the run.
	token, err := e.engine.HarnessTokens.IssueBusinessRunToken(tenantID, identity, runID, workflowID, scopes, knowledge, businessRunCapacityWait+timeout+time.Minute)
	if err != nil {
		return ports.AIWorkflowResult{RunID: runID}, fmt.Errorf("签发 AI 工作流凭据失败：%w", err)
	}
	if knowledge == nil {
		if useKnowledge {
			prompt += aiprompt.KnowledgeEvidenceNoTool
		} else {
			prompt += aiprompt.KnowledgeNotAuthorized
		}
	}
	request := ports.AIWorkflowRequest{TenantID: tenantID, Actor: identity.Username, RunID: runID, ConversationID: runID, WorkflowID: workflowID, Question: prompt, MaxTokens: maxTokens, MCPToken: token, OneShot: true, Timeout: timeout}
	if err := core.ValidateAIInput(prompt, 30<<10); err != nil {
		return ports.AIWorkflowResult{RunID: runID}, err
	}
	result, started, err := e.streamWhenAvailable(ctx, request)
	if result.RunID == "" {
		result.RunID = runID
	}
	if err != nil {
		err = fmt.Errorf("AI 工作流 %s 执行失败：%w", workflowID, err)
	} else if strings.TrimSpace(result.Answer) == "" {
		err = fmt.Errorf("AI 工作流 %s 未返回内容", workflowID)
	}
	e.RecordAIRun(AIRunMeta{TenantID: tenantID, Actor: identity.Username, WorkflowID: workflowID, PromptVersion: promptVersion, InputBytes: len(prompt), StartedAt: started}, result, err)
	return result, err
}

// KeywordOnlyHits reports whether retrieval fell back to keyword matching
// because the vector service was unavailable.
func KeywordOnlyHits(hits []ports.KnowledgeHit) bool {
	for _, hit := range hits {
		if hit.KeywordOnly {
			return true
		}
	}
	return false
}

// retrievalQuery joins distinct non-empty terms after a fixed topic.
func retrievalQuery(topic string, terms []string) string {
	seen := map[string]bool{}
	parts := []string{topic}
	for _, term := range terms {
		if term = strings.TrimSpace(term); term != "" && !seen[term] {
			seen[term] = true
			parts = append(parts, term)
		}
	}
	return ports.BoundKnowledgeQuery(strings.Join(parts, " "))
}

// Alarm context is fitted below the business input limit, leaving room for
// knowledge evidence, which is then appended within the full budget.
const (
	alarmPromptBytes = 22 << 10
	alarmPromptUnits = 15000
)

func (e *Service) runAlarmAnalysisWorkflow(ctx context.Context, alarm model.Alarm, analysisContext alarmContext, knowledge []ports.KnowledgeHit, withKnowledge bool) (model.AIAnalysis, error) {
	if withKnowledge {
		identity, ok := ports.AIRunIdentityFrom(ctx)
		if !ok {
			return model.AIAnalysis{}, errors.New("缺少 AI 运行身份，拒绝执行")
		}
		allowed := false
		for _, scope := range identity.Scopes {
			if scope == ports.MCPToolScope("query_knowledge_base") {
				allowed = true
				break
			}
		}
		if !allowed {
			return model.AIAnalysis{}, errors.New("当前用户无知识库访问权限，拒绝发送知识内容")
		}
	}
	prompt := analysisContext.fitPrompt(func(history []map[string]any) string {
		return aiprompt.AlarmAnalysis(mustJSON(map[string]any{"context": history}))
	}, alarmPromptBytes, alarmPromptUnits)
	if withKnowledge && len(knowledge) > 0 {
		// The same cited, budgeted evidence format as every other workflow,
		// marked as data rather than instructions.
		withEvidence, evidenceErr := core.AppendKnowledgeEvidence(prompt, knowledge, 30<<10)
		if evidenceErr != nil {
			return model.AIAnalysis{}, evidenceErr
		}
		prompt = withEvidence
		if KeywordOnlyHits(knowledge) {
			prompt += aiprompt.KnowledgeKeywordOnly
		}
	}
	tools := []string{"query_alarm_list", "query_alarm_detail", "query_property_history", "query_similar_alarms"}
	if withKnowledge {
		tools = append(tools, "query_knowledge_base")
	}
	// Alarm analysis retrieves its own evidence (alarmAnalysisKnowledge).
	result, err := e.runBusinessWorkflow(ctx, alarm.TenantID, WorkflowAlarmAnalysis, aiprompt.AlarmAnalysisVersion, prompt, "", tools, 4096)
	if err != nil {
		return model.AIAnalysis{}, err
	}
	analysis, err := aioutput.DecodeAlarmAnalysis(result.Answer, alarm.ID, result.Model)
	if err != nil {
		return analysis, fmt.Errorf("AI 告警研判结果格式无效：%w", err)
	}
	analysis.PromptVersion = aiprompt.AlarmAnalysisVersion
	return analysis, nil
}

// DraftRule turns a natural-language requirement into a disabled rule draft
// through the rule-drafter workflow. The draft is not saved here.
func (e *Service) DraftRule(ctx context.Context, tenantID, text string) (model.AlarmRule, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return model.AlarmRule{}, errors.New("规则描述不能为空")
	}
	prompt := aiprompt.RuleDraft(text)
	result, err := e.runBusinessWorkflow(ctx, tenantID, WorkflowRuleDraft, aiprompt.RuleDraftVersion, prompt, retrievalQuery("告警规则", []string{text}), []string{"query_system_overview"}, 4096)
	if err != nil {
		return model.AlarmRule{}, err
	}
	rule, err := aioutput.DecodeRuleDraft(result.Answer)
	if err != nil {
		return rule, fmt.Errorf("AI 规则草稿格式无效：%w", err)
	}
	now := e.engine.Clock.Now().UnixMilli()
	rule.TenantID, rule.Enabled, rule.Version = tenantID, false, 1
	rule.CreatedAt, rule.UpdatedAt = now, now
	return rule, nil
}

func (e *Service) businessRunTimeout() time.Duration {
	if e.engine.BusinessRunTimeout > 0 {
		return e.engine.BusinessRunTimeout
	}
	return defaultBusinessRunTimeout
}

// BusinessRunBudget is the longest a business run can take: waiting for a
// free Harness slot plus the run itself. Callers that bound a run with their
// own context derive the deadline from it so the inner budget is reachable.
func (e *Service) BusinessRunBudget() time.Duration {
	return businessRunCapacityWait + e.businessRunTimeout()
}

// businessRunCapacityWait bounds how long a business run waits for a free
// Harness slot; interactive chat is not retried and keeps its own slot.
var (
	businessRunCapacityWait = 2 * time.Minute
	businessRunRetryDelay   = time.Second
)

// streamWhenAvailable waits with backoff while the Harness reports it is at
// its concurrency limit, instead of failing background work immediately. The
// MCP token stays valid because it outlives the wait. started is when the
// final attempt began, so run durations exclude the wait for a slot.
func (e *Service) streamWhenAvailable(ctx context.Context, request ports.AIWorkflowRequest) (ports.AIWorkflowResult, time.Time, error) {
	deadline := time.Now().Add(businessRunCapacityWait)
	delay := businessRunRetryDelay
	for {
		started := time.Now()
		result, err := e.engine.AIWorkflows.StreamChat(ctx, request, nil)
		if !errors.Is(err, ports.ErrAIWorkflowBusy) || time.Now().Add(delay).After(deadline) {
			return result, started, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, started, ctx.Err()
		case <-timer.C:
		}
		if delay < 8*businessRunRetryDelay {
			delay *= 2
		}
	}
}

// AnalyzeAlarm runs analysis explicitly requested by an operator. withKnowledge must be
// decided by the caller's role: knowledge-based results are stored separately
// and only shown to roles allowed to query the knowledge base.
func (e *Service) AnalyzeAlarm(ctx context.Context, tenantID, alarmID string, withKnowledge bool) (model.AIAnalysis, error) {
	if !e.AIWorkflowsReady() {
		return model.AIAnalysis{}, ErrAIWorkflowsUnavailable
	}
	if e.Authorizer != nil {
		var err error
		ctx, err = e.authorize(ctx, tenantID, WorkflowAlarmAnalysis)
		if err != nil {
			return model.AIAnalysis{}, err
		}
	}
	alarm, err := e.engine.Repo.GetAlarm(ctx, tenantID, alarmID)
	if err != nil {
		return model.AIAnalysis{}, err
	}
	analysisContext := e.buildAlarmContext(ctx, alarm)
	var knowledge []ports.KnowledgeHit
	scope := model.AIAnalysisScopeNone
	var documents []string
	if withKnowledge {
		identity, hasIdentity := ports.AIRunIdentityFrom(ctx)
		authorized := false
		for _, permission := range identity.Scopes {
			if permission == ports.MCPToolScope("query_knowledge_base") {
				authorized = true
				break
			}
		}
		if !hasIdentity || !authorized {
			return model.AIAnalysis{}, errors.New("当前运行身份无知识库访问权限")
		}
		scope = model.AlarmAnalysisWorkflowID
		knowledge, documents, err = e.alarmAnalysisKnowledge(ctx, alarm, analysisContext.symptoms)
	}
	var analysis model.AIAnalysis
	if err == nil {
		analysis, err = e.runAlarmAnalysisWorkflow(ctx, alarm, analysisContext, knowledge, withKnowledge)
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && e.engine.Metrics != nil {
			e.engine.Metrics.Inc("ai_analysis_timeout_total")
		}
		if e.engine.Metrics != nil {
			e.engine.Metrics.Inc("ai_analysis_failed_total")
		}
		analysis = model.AIAnalysis{
			AlarmID:       alarm.ID,
			Summary:       "AI 研判暂时失败，已保留告警供人工研判。",
			RiskLevel:     alarm.AlarmLevel,
			Model:         aiModelName(e.engine.AI),
			PromptVersion: aiprompt.AlarmFallbackVersion,
			CreatedAt:     e.engine.Clock.Now().UnixMilli(),
			Error:         err.Error(),
		}
	}
	if err == nil && e.engine.Metrics != nil {
		e.engine.Metrics.Inc("ai_analysis_success_total")
	}
	analysis.Status = "succeeded"
	if err != nil {
		analysis.Status = "failed"
	}
	analysis.TenantID = alarm.TenantID
	analysis.AlarmID = alarm.ID
	analysis.KnowledgeScope = scope
	analysis.KnowledgeDocuments = documents
	analysis.CapacityRunID = ports.CapacityRunID(ctx)
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	if saveErr := e.engine.Repo.SaveAIAnalysis(saveCtx, analysis); saveErr != nil {
		return analysis, saveErr
	}
	if scope != model.AIAnalysisScopeNone {
		// 引用知识库的结果只返回给发起人，不广播到告警实时主题。
		return analysis, nil
	}
	payload := mustJSON(analysis)
	_ = e.engine.Bus.Publish(ctx, model.TopicAlarmAIAnalysis, alarm.ID, payload)
	_ = e.engine.Realtime.Publish(ctx, alarm.MQTTTopic("ai-analysis"), payload, 1, false)
	return analysis, nil
}

func aiModelName(client ports.AIClient) string {
	if provider, ok := client.(ports.AIInspectable); ok {
		info := provider.ProviderInfo()
		if name := strings.TrimSpace(info.Model); name != "" {
			return name
		}
		if id := strings.TrimSpace(info.ID); id != "" {
			return id
		}
	}
	return "unavailable"
}
