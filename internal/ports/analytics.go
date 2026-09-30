package ports

import (
	"context"
	"time"

	"iot-platform/internal/model"
)

// AnalysisStore writes only analysis objects; it has no production alarm,
// device-control, bus or realtime capabilities. Claim is a trusted worker API.
// Current user permissions must be rechecked before reads, writes and AI calls.
type AnalysisStore interface {
	CreateAnalysisRun(context.Context, model.AnalysisRun, int) (model.AnalysisRun, error)
	GetAnalysisRun(context.Context, string, string) (model.AnalysisRun, error)
	ListAnalysisRuns(context.Context, string, model.AnalysisFilter) ([]model.AnalysisRun, int, error)
	ClaimAnalysisRun(context.Context, string, time.Duration, []string) (model.AnalysisRun, error)
	RenewAnalysisLease(context.Context, string, string, int64, time.Duration) (model.AnalysisRun, error)
	CommitAnalysisBatch(context.Context, string, string, int64, model.AnalysisBatch) (model.AnalysisRun, error)
	StopAnalysisRun(context.Context, string, string, int64) (model.AnalysisRun, error)
	GetAnalysisSnapshot(context.Context, string, string) (model.AnalysisSnapshot, error)
	ListAnalysisEvidence(context.Context, string, model.AnalysisFilter) ([]model.AnalysisEvidence, int, error)
	ListAnalysisOutputs(context.Context, string, model.AnalysisFilter) ([]model.AnalysisOutput, int, error)
	PutAnalysisConfig(context.Context, model.AnalysisConfigRevision, int64) (model.AnalysisConfigRevision, error)
	GetAnalysisConfig(context.Context, string, string) (model.AnalysisConfigRevision, error)
	ListAnalysisConfigs(context.Context, string, model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error)
	AppendAnalysisReview(context.Context, model.AnalysisReview, int64) (model.AnalysisReview, error)
	ListAnalysisReviews(context.Context, string, model.AnalysisFilter) ([]model.AnalysisReview, int, error)
	PutAnalysisAIRevision(context.Context, model.AnalysisAIRevision, int64) (model.AnalysisAIRevision, error)
	GetAnalysisAIRevision(context.Context, string, string) (model.AnalysisAIRevision, error)
	ListAnalysisAIRevisions(context.Context, string, model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error)
}
