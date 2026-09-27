// Package ratelimit provides fixed-window budgets shared by every process of
// a cluster (through a shared store such as Redis) with a conservative local
// fallback.
package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Limiter is a fixed-window counter keyed by budget name.
type Limiter interface {
	// Allow consumes one unit of key's budget of limit per window.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	// Hits reports the units used in key's current window and when it resets.
	Hits(ctx context.Context, key string, window time.Duration) (int, time.Duration, error)
	// Reset clears key's current window.
	Reset(ctx context.Context, key string, window time.Duration) error
}

// tableLimit bounds tracked keys; expired windows are swept first.
const tableLimit = 100000

type window struct {
	start, end time.Time
	count      int
}

// Local is an in-process fixed-window limiter.
type Local struct {
	mu        sync.Mutex
	keys      map[string]window
	lastSweep time.Time
	Now       func() time.Time
}

func NewLocal() *Local { return &Local{keys: map[string]window{}, Now: time.Now} }

func (l *Local) current(key string, d time.Duration, now time.Time) window {
	w := l.keys[key]
	start := now.Truncate(d)
	if !w.start.Equal(start) {
		w = window{start: start, end: start.Add(d)}
	}
	return w
}

func (l *Local) Allow(_ context.Context, key string, limit int, d time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	w := l.current(key, d, now)
	if w.count >= limit {
		return false, nil
	}
	if _, tracked := l.keys[key]; !tracked && len(l.keys) >= tableLimit {
		if now.Sub(l.lastSweep) >= time.Second {
			// A key whose window has ended equals an absent key.
			for k, v := range l.keys {
				if !now.Before(v.end) {
					delete(l.keys, k)
				}
			}
			l.lastSweep = now
		}
		if len(l.keys) >= tableLimit {
			return false, nil
		}
	}
	w.count++
	l.keys[key] = w
	return true, nil
}

func (l *Local) Hits(_ context.Context, key string, d time.Duration) (int, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	w := l.current(key, d, now)
	return w.count, w.start.Add(d).Sub(now), nil
}

func (l *Local) Reset(_ context.Context, key string, _ time.Duration) error {
	l.mu.Lock()
	delete(l.keys, key)
	l.mu.Unlock()
	return nil
}

// Cluster uses the shared limiter and, when it fails, a local limiter with
// every budget divided by the instance count, so an outage of the shared
// store never widens the cluster-wide budget.
type Cluster struct {
	Shared    Limiter // nil: local only (single process)
	Local     *Local
	Instances int
	errors    atomic.Int64
}

func NewCluster(shared Limiter, instances int) *Cluster {
	if instances < 1 {
		instances = 1
	}
	return &Cluster{Shared: shared, Local: NewLocal(), Instances: instances}
}

// SharedErrors counts shared-store failures that fell back to local budgets.
func (c *Cluster) SharedErrors() int64 { return c.errors.Load() }

func (c *Cluster) share(limit int) int {
	if c.Shared == nil {
		return limit
	}
	return max(1, limit/c.Instances)
}

func (c *Cluster) Allow(ctx context.Context, key string, limit int, d time.Duration) (bool, error) {
	if c.Shared != nil {
		ok, err := c.Shared.Allow(ctx, key, limit, d)
		if err == nil {
			return ok, nil
		}
		c.errors.Add(1)
	}
	return c.Local.Allow(ctx, key, c.share(limit), d)
}

func (c *Cluster) Hits(ctx context.Context, key string, d time.Duration) (int, time.Duration, error) {
	if c.Shared != nil {
		n, reset, err := c.Shared.Hits(ctx, key, d)
		if err == nil {
			return n, reset, nil
		}
		c.errors.Add(1)
	}
	n, reset, err := c.Local.Hits(ctx, key, d)
	// Scale the local count so thresholds keep their cluster-wide meaning.
	return n * max(1, c.Instances*boolInt(c.Shared != nil)), reset, err
}

func (c *Cluster) Reset(ctx context.Context, key string, d time.Duration) error {
	_ = c.Local.Reset(ctx, key, d)
	if c.Shared != nil {
		if err := c.Shared.Reset(ctx, key, d); err != nil {
			c.errors.Add(1)
			return err
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
