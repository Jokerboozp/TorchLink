package analytics

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
const WorkflowRecurring = "recurring-alarm-analyst"
const RecurringAIPromptVersion = "recurring-fixed-facts-v2"

// WorkflowDefinition is the fixed registry used by HTTP, AI and MCP. Neither
// manifests nor tool names can be supplied by a browser or by uploaded data.
type WorkflowDefinition struct {
	AIWorkflowSpec
	Menu, Prefix, Manifest, Schema string
	Collections                    []string
}

var workflowDefinitions = []WorkflowDefinition{
	{AIWorkflowSpec{KindDataQuality, WorkflowDataQuality, AnalysisAIPromptVersion}, "dataQuality", "/api/v1/data-quality", "data-quality-analyst.json", "quality-v1", []string{"findings", "metrics", "evidence"}},
	{AIWorkflowSpec{KindMonitoring, WorkflowMonitoring, MonitoringAIPromptVersion}, "monitoringGaps", "/api/v1/monitoring-gaps", "monitoring-continuity-reviewer.json", "monitoring-v1", []string{"findings", "metrics", "intervals", "dependency-groups", "evidence"}},
	{AIWorkflowSpec{KindRuleLab, WorkflowRulePolicy, RulePolicyAIPromptVersion}, "ruleLab", "/api/v1/rule-lab", "rule-policy-analyst.json", "rule-policy-v1", []string{"findings", "diffs", "metrics", "outcomes", "labels", "evidence"}},
	{AIWorkflowSpec{KindResponse, WorkflowResponse, ResponseAIPromptVersion}, "response", "/api/v1/response-runs", "response-reviewer.json", "response-v1", []string{"findings", "metrics", "evidence"}},
	{AIWorkflowSpec{KindMaintenance, WorkflowMaintenance, MaintenanceAIPromptVersion}, "maintenance", "/api/v1/maintenance-observations", "maintenance-outcome-reviewer.json", "maintenance-v1", []string{"findings", "observations", "change-metrics", "evidence"}},
	{AIWorkflowSpec{KindInvestment, WorkflowInvestment, InvestmentAIPromptVersion}, "maintenance", "/api/v1/investment-scenarios", "maintenance-investment-advisor.json", "investment-v1", []string{"findings", "investment-priorities", "budget-lines", "evidence"}},
	{AIWorkflowSpec{KindRecurring, WorkflowRecurring, RecurringAIPromptVersion}, "alarmGovernance", "/api/v1/alarm-governance", "recurring-alarm-analyst.json", "recurring-v1", []string{"findings", "metrics", "observations", "cycles", "verifications", "activities", "coverages", "measures", "observation-results", "activity-candidates", "evidence"}},
}

func WorkflowDefinitionFor(kind string) (WorkflowDefinition, bool) {
	for _, d := range workflowDefinitions {
		if d.Kind == kind {
			d.Collections = append([]string(nil), d.Collections...)
			return d, true
		}
	}
	return WorkflowDefinition{}, false
}
func WorkflowDefinitionByID(id string) (WorkflowDefinition, bool) {
	for _, d := range workflowDefinitions {
		if d.WorkflowID == id {
			return WorkflowDefinitionFor(d.Kind)
		}
	}
	return WorkflowDefinition{}, false
}
func AnalysisWorkflow(kind string) (AIWorkflowSpec, bool) {
	d, ok := WorkflowDefinitionFor(kind)
	return d.AIWorkflowSpec, ok
}
func IsAnalysisWorkflow(workflow string) bool { _, ok := WorkflowDefinitionByID(workflow); return ok }
func analysisCollections(workflow string) []string {
	d, _ := WorkflowDefinitionByID(workflow)
	return d.Collections
}
