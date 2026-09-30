package postgres

import (
	"context"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.AnalysisRestoredObjectStore = (*Repository)(nil)

func (r *Repository) GetRestoredObjectLocation(ctx context.Context, tenant, bucket, key string) (model.AnalysisRestoredObjectLocation, error) {
	return r.analysisStore().GetRestoredObjectLocation(ctx, tenant, bucket, key)
}
