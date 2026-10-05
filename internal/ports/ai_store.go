package ports

import (
	"context"

	"iot-platform/internal/model"
)

// AIStore keeps AI analyses, inspection and analysis jobs, and tool call logs.
type AIStore interface {
	SaveAIAnalysis(context.Context, model.AIAnalysis) error
	GetAIAnalysis(ctx context.Context, tenantID, alarmID, knowledgeScope string) (model.AIAnalysis, error)
	// CreateHealthInspectionJob returns false when the tenant already has a
	// running inspection; at most one runs per tenant across all replicas.
	CreateHealthInspectionJob(context.Context, model.HealthInspectionJob) (bool, error)
	// UpdateRunningHealthInspectionJob changes a job only while the stored copy
	// is still running, so a job already marked interrupted is not revived.
	UpdateRunningHealthInspectionJob(context.Context, model.HealthInspectionJob) (bool, error)
	// LatestHealthInspectionJob returns the newest job, optionally with status.
	LatestHealthInspectionJob(ctx context.Context, tenantID, status string) (model.HealthInspectionJob, error)
	// Summary never loads device detail rows. Pages address an immutable report ID.
	LatestHealthInspectionSummary(context.Context, string, string) (model.HealthInspectionJob, error)
	HealthInspectionPage(context.Context, string, string, int, int) (model.HealthInspectionJob, error)
	// CreateAlarmAnalysisJob returns false while a job for the same alarm and
	// knowledge scope is running; finished jobs of that alarm and scope are
	// replaced, so only the newest result is kept.
	CreateAlarmAnalysisJob(context.Context, model.AlarmAnalysisJob) (bool, error)
	// UpdateRunningAlarmAnalysisJob changes a job only while it is still running.
	UpdateRunningAlarmAnalysisJob(context.Context, model.AlarmAnalysisJob) (bool, error)
	LatestAlarmAnalysisJob(ctx context.Context, tenantID, alarmID, knowledgeScope string) (model.AlarmAnalysisJob, error)
	SaveAIToolCall(context.Context, model.AIToolCallLog) error
}
