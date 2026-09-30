package redisadapter

import (
	"context"
	"errors"
	"path"
	"testing"

	"github.com/redis/go-redis/v9"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

type cleanupRedis struct {
	redis.UniversalClient
	keys map[string]bool
	sets map[string]map[string]bool
	fail bool
}

func (c *cleanupRedis) Del(_ context.Context, keys ...string) *redis.IntCmd {
	if c.fail {
		return redis.NewIntResult(0, errors.New("cache unavailable"))
	}
	var n int64
	for _, k := range keys {
		if c.keys[k] {
			n++
			delete(c.keys, k)
		}
	}
	return redis.NewIntResult(n, nil)
}
func (c *cleanupRedis) Scan(ctx context.Context, _ uint64, pattern string, _ int64) *redis.ScanCmd {
	var keys []string
	for k := range c.keys {
		if matches, _ := path.Match(pattern, k); matches {
			keys = append(keys, k)
		}
	}
	cmd := redis.NewScanCmd(ctx, nil)
	cmd.SetVal(keys, 0)
	return cmd
}
func (c *cleanupRedis) SRem(_ context.Context, key string, members ...interface{}) *redis.IntCmd {
	var n int64
	for _, v := range members {
		if s, ok := v.(string); ok && c.sets[key][s] {
			delete(c.sets[key], s)
			n++
		}
	}
	return redis.NewIntResult(n, nil)
}

func TestCapacityCleanupInvalidatesOnlyScopedCacheAndRetries(t *testing.T) {
	ctx := context.Background()
	base := memory.NewRepository()
	_, _ = base.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", ProductID: "p", DeviceID: "cap", MessageID: "raw", ParseAttemptedAt: 1})
	c := &cleanupRedis{keys: map[string]bool{}, sets: map[string]map[string]bool{"device:online:" + cacheSegment("t"): {"cap": true, "business": true}}, fail: true}
	removed := []string{stateKey("t", "cap"), latestKey("t", "cap"), "alarm:active:" + cacheSegment("t") + ":" + cacheSegment("cap") + ":alarm"}
	kept := []string{stateKey("other", "cap"), latestKey("t", "business"), "alarm:active:" + cacheSegment("other") + ":" + cacheSegment("cap") + ":alarm"}
	for _, k := range append(removed, kept...) {
		c.keys[k] = true
	}
	r := New(base, c)
	q := model.CapacityCleanupBatch{RunID: "cap-20260930-120000-abcdef", Product: "p", Devices: []string{"cap"}, RemoveDevices: []string{"cap"}}
	if _, err := r.CleanupCapacityData(ctx, "t", q); err == nil {
		t.Fatal("cache failure reported success")
	}
	c.fail = false
	if _, err := r.CleanupCapacityData(ctx, "t", q); err != nil {
		t.Fatal("retry failed", err)
	}
	if _, err := base.GetRawIndex(ctx, "t", "raw"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("durable raw retained", err)
	}
	for _, k := range removed {
		if c.keys[k] {
			t.Fatal("cache remains", k)
		}
	}
	for _, k := range kept {
		if !c.keys[k] {
			t.Fatal("unrelated cache deleted", k)
		}
	}
	if c.sets["device:online:"+cacheSegment("t")]["cap"] || !c.sets["device:online:"+cacheSegment("t")]["business"] {
		t.Fatal("wrong online membership removed")
	}
}

func TestCacheKeysDoNotCollideWhenIDsContainSeparators(t *testing.T) {
	if stateKey("tenant:a", "device") == stateKey("tenant", "a:device") {
		t.Fatal("state cache keys collide")
	}
	if latestKey("tenant:a", "device") == latestKey("tenant", "a:device") {
		t.Fatal("latest-message cache keys collide")
	}
}
