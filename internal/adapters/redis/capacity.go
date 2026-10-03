package redisadapter

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"strings"
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
	// Removed devices lose their cached state, latest message and active alarms.
	remove := map[string]bool{}
	keys := []string{}
	members := []any{}
	for _, device := range q.Devices {
		segment := cacheSegment(device)
		if remove[segment] {
			continue
		}
		remove[segment] = true
		keys = append(keys, stateKey(t, device), latestKey(t, device))
		members = append(members, device)
	}
	for len(keys) > 0 {
		batch := min(1000, len(keys))
		if err = r.client.Del(ctx, keys[:batch]...).Err(); err != nil {
			return n, err
		}
		keys = keys[batch:]
	}
	// One tenant scan per batch replaces a full key-space scan for every test
	// device. Encoded segments cannot contain ':', so the exact device boundary
	// preserves shared fixtures and IDs which merely share a text prefix.
	if len(remove) > 0 {
		prefix := "alarm:active:" + cacheSegment(t) + ":"
		var cursor uint64
		for {
			page, next, err := r.client.Scan(ctx, cursor, prefix+"*", 500).Result()
			if err != nil {
				return n, err
			}
			selected := []string{}
			for _, key := range page {
				if !strings.HasPrefix(key, prefix) {
					continue
				}
				device, _, found := strings.Cut(strings.TrimPrefix(key, prefix), ":")
				if found && remove[device] {
					selected = append(selected, key)
				}
			}
			if len(selected) > 0 {
				if err := r.client.Del(ctx, selected...).Err(); err != nil {
					return n, err
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	for len(members) > 0 {
		batch := min(1000, len(members))
		if err = r.client.SRem(ctx, "device:online:"+cacheSegment(t), members[:batch]...).Err(); err != nil {
			return n, err
		}
		members = members[batch:]
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
