package ports

import (
	"context"
	"iot-platform/internal/model"
)

// CapacityDataCleaner is optional: cleanup must fail explicitly if a storage
// adapter cannot remove test data. Reads/checks precede external mutations.
type CapacityDataCleaner interface {
	CapacityMessageIDs(context.Context, string, model.CapacityCleanupBatch) ([]string, error)
	CleanupCapacityData(context.Context, string, model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error)
}

type capacityRunKey struct{}

func WithCapacityRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, capacityRunKey{}, id)
}

func CapacityRunID(ctx context.Context) string {
	id, _ := ctx.Value(capacityRunKey{}).(string)
	return id
}
