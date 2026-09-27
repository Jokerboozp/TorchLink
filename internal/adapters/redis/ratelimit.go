package redisadapter

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter keeps fixed-window budgets in Redis so every process of a
// cluster consumes the same budget. Windows are aligned to Unix time; node
// clock skew shifts a window boundary by at most the skew.
type RateLimiter struct {
	client redis.UniversalClient
	now    func() time.Time
}

func NewRateLimiter(client redis.UniversalClient) *RateLimiter {
	return &RateLimiter{client: client, now: time.Now}
}

func windowKey(key string, d time.Duration, start time.Time) string {
	return "rl:" + cacheSegment(key) + ":" + strconv.FormatInt(d.Milliseconds(), 10) + ":" + strconv.FormatInt(start.UnixMilli(), 10)
}

func (r *RateLimiter) Allow(ctx context.Context, key string, limit int, d time.Duration) (bool, error) {
	start := r.now().Truncate(d)
	k := windowKey(key, d, start)
	pipe := r.client.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.PExpire(ctx, k, 2*d)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return incr.Val() <= int64(limit), nil
}

func (r *RateLimiter) Hits(ctx context.Context, key string, d time.Duration) (int, time.Duration, error) {
	now := r.now()
	start := now.Truncate(d)
	n, err := r.client.Get(ctx, windowKey(key, d, start)).Int()
	if errors.Is(err, redis.Nil) {
		n, err = 0, nil
	}
	return n, start.Add(d).Sub(now), err
}

func (r *RateLimiter) Reset(ctx context.Context, key string, d time.Duration) error {
	return r.client.Del(ctx, windowKey(key, d, r.now().Truncate(d))).Err()
}
