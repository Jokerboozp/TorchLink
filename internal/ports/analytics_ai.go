package ports

import (
	"context"
	"iot-platform/internal/model"
	"time"
)

// AI leases are independent of fact-task leases. Expired RUNNING jobs have an
// unknown external outcome and must be failed, never automatically invoked again.
type AnalysisAIStore interface {
	CreateAnalysisAIRevision(context.Context, model.AnalysisAIRevision, int64, int) (model.AnalysisAIRevision, error)
	ClaimAnalysisAIRevision(context.Context, string, time.Duration, time.Duration, []string) (model.AnalysisAIRevision, error)
	RenewAnalysisAILease(context.Context, string, string, int64, time.Duration) (model.AnalysisAIRevision, error)
	RecordAnalysisAIFacts(context.Context, string, string, int64, []string) (model.AnalysisAIRevision, error)
	FinishAnalysisAIRevision(context.Context, string, string, int64, model.AnalysisAIResult, string, string) (model.AnalysisAIRevision, error)
	StopAnalysisAIRevision(context.Context, string, string, int64) (model.AnalysisAIRevision, error)
}

// A bound reader rechecks the account, full snapshot scope, version and job
// lease on every tool call. It accepts no arbitrary run, device, tenant or SQL.
type AnalysisAIReader interface {
	ReadBoundAnalysisFacts(context.Context, AIRunIdentity, string, int, int) (model.AnalysisAIFacts, error)
}
