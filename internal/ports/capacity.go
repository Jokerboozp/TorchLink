package ports

import (
	"context"
	"iot-platform/internal/model"
)

// CapacityDataCleaner removes data of dedicated capacity test products. It is
// optional: cleanup fails explicitly when a storage adapter cannot provide it.
// Callers must require full tenant device access.
type CapacityDataCleaner interface {
	CleanupCapacityData(context.Context, string, model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error)
	ListCapacityFixtureProducts(context.Context, string) ([]model.CapacityFixtureProduct, error)
	ListCapacityFixtureDevices(ctx context.Context, tenant, product, after string, limit int) ([]string, error)
}

// CapacityRetainedCleaner clears retained device state of removed fixtures.
type CapacityRetainedCleaner interface {
	ClearCapacityRetained(ctx context.Context, tenant, product string, devices []string) (int64, error)
}

type capacityRunKey struct{}

func WithCapacityRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, capacityRunKey{}, id)
}

func CapacityRunID(ctx context.Context) string {
	id, _ := ctx.Value(capacityRunKey{}).(string)
	return id
}
