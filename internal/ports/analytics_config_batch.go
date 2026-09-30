package ports

import (
	"context"
	"iot-platform/internal/model"
)

// AnalysisConfigBatchStore atomically updates related immutable revisions and
// their latest pointers. All revisions belong to one tenant and have distinct
// IDs and (kind,resource,scope,personal-owner) identities. A stale pointer or an
// invalid member rolls back every member; no partial asset timeline is visible.
type AnalysisConfigBatchStore interface {
	PutAnalysisConfigs(context.Context, []model.AnalysisConfigRevision, []int64) ([]model.AnalysisConfigRevision, error)
}
