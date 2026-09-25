package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// DefaultWorkflowKnowledgeBinding is the retrieval policy used before an
// administrator saves one for the Agent.
func DefaultWorkflowKnowledgeBinding(tenantID, workflowID string) model.WorkflowKnowledgeBinding {
	if workflowID == "system-observer" {
		return model.WorkflowKnowledgeBinding{TenantID: tenantID, WorkflowID: workflowID, RetrievalMode: "disabled", TopK: 5, MinScore: 0.25, NoMatchPolicy: "allow-model"}
	}
	return model.WorkflowKnowledgeBinding{TenantID: tenantID, WorkflowID: workflowID, RetrievalMode: "auto", TopK: 5, MinScore: 0.25, NoMatchPolicy: "allow-model"}
}

// SearchWorkflowKnowledge retrieves only documents owned by the bound Agent.
// An index without workflow filtering is rejected instead of falling back to a
// tenant-wide search.
func SearchWorkflowKnowledge(ctx context.Context, kb ports.KnowledgeBase, tenantID, question string, binding model.WorkflowKnowledgeBinding) ([]ports.KnowledgeHit, error) {
	if filtered, ok := kb.(ports.FilteredKnowledgeBase); ok {
		return filtered.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: tenantID, WorkflowID: binding.WorkflowID, Question: question, Limit: binding.TopK, MinScore: binding.MinScore})
	}
	return nil, errors.New("workflow-bound knowledge search is not supported by the configured index")
}

// alarmAnalysisKnowledge follows the alarm-handler Agent's knowledge binding.
// It returns the evidence text and the distinct source document IDs.
func (e *Engine) alarmAnalysisKnowledge(ctx context.Context, alarm model.Alarm) ([]string, []string, error) {
	binding, err := e.Repo.GetWorkflowKnowledgeBinding(ctx, alarm.TenantID, model.AlarmAnalysisWorkflowID)
	if err != nil {
		return nil, nil, fmt.Errorf("读取告警研判知识策略失败：%w", err)
	}
	if binding.WorkflowID == "" {
		binding = DefaultWorkflowKnowledgeBinding(alarm.TenantID, model.AlarmAnalysisWorkflowID)
	}
	if binding.RetrievalMode == "disabled" {
		return nil, nil, nil
	}
	if e.KB == nil {
		return nil, nil, errors.New("知识库未配置，无法按告警研判智能体检索")
	}
	question := strings.Join([]string{alarm.AlarmType, alarm.DeviceType, "处置 SOP 维修"}, " ")
	hits, err := SearchWorkflowKnowledge(ctx, e.KB, alarm.TenantID, question, binding)
	if err != nil {
		return nil, nil, fmt.Errorf("检索告警研判知识失败：%w", err)
	}
	if len(hits) == 0 && binding.NoMatchPolicy == "require-evidence" {
		return nil, nil, errors.New("告警研判智能体要求知识证据，但未检索到匹配内容")
	}
	knowledge := make([]string, 0, len(hits))
	documents := []string{}
	seen := map[string]bool{}
	for _, hit := range hits {
		knowledge = append(knowledge, hit.Content)
		if hit.DocumentID != "" && !seen[hit.DocumentID] {
			seen[hit.DocumentID] = true
			documents = append(documents, hit.DocumentID)
		}
	}
	return knowledge, documents, nil
}
