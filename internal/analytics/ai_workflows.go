package analytics

import "slices"

const WorkflowMonitoring = "monitoring-continuity-reviewer"
const MonitoringAIPromptVersion = "monitoring-fixed-facts-v1"

// Each registered application freezes its workflow and prompt contract. Adding
// an unrelated kind alone never enables model execution or snapshot tools.
func AnalysisWorkflow(kind string) (AIWorkflowSpec, bool) {
	switch kind {
	case KindDataQuality:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowDataQuality, PromptVersion: AnalysisAIPromptVersion}, true
	case KindMonitoring:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowMonitoring, PromptVersion: MonitoringAIPromptVersion}, true
	}
	return AIWorkflowSpec{}, false
}

func IsAnalysisWorkflow(workflow string) bool {
	return slices.Contains([]string{WorkflowDataQuality, WorkflowMonitoring}, workflow)
}

func analysisCollections(workflow string) []string {
	switch workflow {
	case WorkflowDataQuality:
		return []string{"findings", "metrics", "evidence"}
	case WorkflowMonitoring:
		return []string{"findings", "metrics", "intervals", "dependency-groups", "evidence"}
	}
	return nil
}
