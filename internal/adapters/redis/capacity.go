package redisadapter

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) CleanupCapacityData(ctx context.Context, t string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return model.CapacityCleanupCounts{}, errors.New("capacity cleanup is unsupported")
	}
	n, err := c.CleanupCapacityData(ctx, t, q)
	if err != nil {
		return n, err
	}
	// Removed devices lose their cached state and latest message.
	keys := []string{}
	seen := map[string]bool{}
	for _, device := range q.Devices {
		if seen[device] {
			continue
		}
		seen[device] = true
		keys = append(keys, stateKey(t, device), latestKey(t, device))
	}
	for len(keys) > 0 {
		batch := min(1000, len(keys))
		if err = r.client.Del(ctx, keys[:batch]...).Err(); err != nil {
			return n, err
		}
		keys = keys[batch:]
	}
	return n, nil
}

func (r *Repository) ListCapacityFixtureProducts(ctx context.Context, tenant string) ([]model.CapacityFixtureProduct, error) {
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return nil, errors.New("capacity cleanup is unsupported")
	}
	return c.ListCapacityFixtureProducts(ctx, tenant)
}

func (r *Repository) ListCapacityFixtureDevices(ctx context.Context, tenant, product, after string, limit int) ([]string, error) {
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return nil, errors.New("capacity cleanup is unsupported")
	}
	return c.ListCapacityFixtureDevices(ctx, tenant, product, after, limit)
}

var _ ports.CapacityDataCleaner = (*Repository)(nil)
