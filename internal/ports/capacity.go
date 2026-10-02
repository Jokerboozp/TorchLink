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

// These keyset pages are tenant scoped; callers must require full tenant device
// access. Preparation revalidates a preview and disables a tool-owned product.
type CapacityFixtureLister interface {
	ListCapacityFixtureProducts(context.Context, string, string, int) ([]model.CapacityFixtureProduct, error)
	ListCapacityFixtureDevices(context.Context, string, string, string, int) ([]string, error)
	PrepareCapacityFixture(context.Context, string, string, string) error
}

type capacityRunKey struct{}

func WithCapacityRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, capacityRunKey{}, id)
}

func CapacityRunID(ctx context.Context) string {
	id, _ := ctx.Value(capacityRunKey{}).(string)
	return id
}
