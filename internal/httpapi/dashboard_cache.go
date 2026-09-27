package httpapi

import (
	"context"
	"golang.org/x/sync/singleflight"
	"iot-platform/internal/model"
	"sync"
	"time"
)

type dashboardEntry struct {
	groups []model.DashboardCount
	at     time.Time
}
type dashboardCache struct {
	mu     sync.Mutex
	values map[string]dashboardEntry
	runs   singleflight.Group
}

func (c *dashboardCache) get(ctx context.Context, key string, load func(context.Context) ([]model.DashboardCount, error)) ([]model.DashboardCount, error) {
	read := func() ([]model.DashboardCount, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		v, ok := c.values[key]
		return v.groups, ok && time.Since(v.at) < 2*time.Second
	}
	if v, ok := read(); ok {
		return v, nil
	}
	result := c.runs.DoChan(key, func() (any, error) {
		if v, ok := read(); ok {
			return v, nil
		}
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		v, err := load(work)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.values == nil {
			c.values = map[string]dashboardEntry{}
		}
		if len(c.values) >= 128 {
			for k := range c.values {
				delete(c.values, k)
				break
			}
		}
		c.values[key] = dashboardEntry{v, time.Now()}
		return v, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-result:
		if v.Err != nil {
			return nil, v.Err
		}
		return v.Val.([]model.DashboardCount), nil
	}
}
