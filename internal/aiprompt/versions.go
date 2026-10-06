// Package aiprompt holds the prompts of the platform's business AI workflows
// and their versions. A version changes whenever its prompt or the context
// sent with it changes, so run records and analysis statistics can compare
// versions.
package aiprompt

const (
	AlarmAnalysisVersion    = "alarm-analysis-v3"
	AlarmFallbackVersion    = "fallback-v2"
	ChatVersion             = "chat-v1"
	HealthInspectionVersion = "health-inspection-v1"
	OpsReportVersion        = "ops-report-v1"
	ProtocolAssistVersion   = "protocol-assistant-v1"
	RuleDraftVersion        = "rule-draft-v1"
)
