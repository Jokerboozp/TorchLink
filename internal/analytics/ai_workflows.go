package analytics

import "slices"

const WorkflowMonitoring = "monitoring-continuity-reviewer"
const MonitoringAIPromptVersion = "monitoring-fixed-facts-v1"
const WorkflowRulePolicy = "rule-policy-analyst"
const RulePolicyAIPromptVersion = "rule-lab-fixed-facts-v1"
const WorkflowResponse = "response-reviewer"
const ResponseAIPromptVersion = "response-fixed-facts-v1"
const WorkflowMaintenance = "maintenance-outcome-reviewer"
const MaintenanceAIPromptVersion = "maintenance-fixed-facts-v1"
const WorkflowInvestment = "maintenance-investment-advisor"
const InvestmentAIPromptVersion = "investment-fixed-facts-v1"

// Each registered application freezes its workflow and prompt contract. Adding
// an unrelated kind alone never enables model execution or snapshot tools.
func AnalysisWorkflow(kind string) (AIWorkflowSpec, bool) {
	switch kind {
	case KindDataQuality:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowDataQuality, PromptVersion: AnalysisAIPromptVersion}, true
	case KindMonitoring:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowMonitoring, PromptVersion: MonitoringAIPromptVersion}, true
	case KindRuleLab:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowRulePolicy, PromptVersion: RulePolicyAIPromptVersion}, true
	case KindResponse:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowResponse, PromptVersion: ResponseAIPromptVersion}, true
	case KindMaintenance:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowMaintenance, PromptVersion: MaintenanceAIPromptVersion}, true
	case KindInvestment:
		return AIWorkflowSpec{Kind: kind, WorkflowID: WorkflowInvestment, PromptVersion: InvestmentAIPromptVersion}, true
	}
	return AIWorkflowSpec{}, false
}

func IsAnalysisWorkflow(workflow string) bool {
	return slices.Contains([]string{WorkflowDataQuality, WorkflowMonitoring, WorkflowRulePolicy, WorkflowResponse, WorkflowMaintenance, WorkflowInvestment}, workflow)
}

func analysisCollections(workflow string) []string {
	switch workflow {
	case WorkflowDataQuality:
		return []string{"findings", "metrics", "evidence"}
	case WorkflowMonitoring:
		return []string{"findings", "metrics", "intervals", "dependency-groups", "evidence"}
	case WorkflowRulePolicy:
		return []string{"findings", "diffs", "metrics", "outcomes", "labels", "evidence"}
	case WorkflowResponse:
		return []string{"findings", "metrics", "evidence"}
	case WorkflowMaintenance:
		return []string{"findings", "observations", "change-metrics", "evidence"}
	case WorkflowInvestment:
		return []string{"findings", "investment-priorities", "budget-lines", "evidence"}
	}
	return nil
}
