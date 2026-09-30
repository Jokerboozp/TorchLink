package redisadapter

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) CapacityMessageIDs(ctx context.Context, t string, q model.CapacityCleanupBatch) ([]string, error) {
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return nil, errors.New("capacity cleanup is unsupported")
	}
	return c.CapacityMessageIDs(ctx, t, q)
}
func (r *Repository) CleanupCapacityData(ctx context.Context, t string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	c, ok := r.Repository.(ports.CapacityDataCleaner)
	if !ok {
		return model.CapacityCleanupCounts{}, errors.New("capacity cleanup is unsupported")
	}
	n, err := c.CleanupCapacityData(ctx, t, q)
	if err != nil {
		return n, err
	}
	for _, device := range q.Devices {
		if err = r.client.Del(ctx, stateKey(t, device), latestKey(t, device)).Err(); err != nil {
			return n, err
		}
		var cursor uint64
		for {
			keys, next, e := r.client.Scan(ctx, cursor, fmt.Sprintf("alarm:active:%s:%s:*", cacheSegment(t), cacheSegment(device)), 500).Result()
			if e != nil {
				return n, e
			}
			if len(keys) > 0 {
				if e = r.client.Del(ctx, keys...).Err(); e != nil {
					return n, e
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	for _, device := range q.RemoveDevices {
		if err = r.client.SRem(ctx, "device:online:"+cacheSegment(t), device).Err(); err != nil {
			return n, err
		}
	}
	return n, nil
}
