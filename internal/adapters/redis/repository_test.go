package redisadapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/redis/go-redis/v9"
	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
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
	kept := []string{stateKey("other", "cap"), stateKey("t", "business"), latestKey("t", "business"), "alarm:active:" + cacheSegment("other") + ":" + cacheSegment("cap") + ":alarm", "alarm:active:" + cacheSegment("t") + ":" + cacheSegment("business") + ":alarm"}
	for _, k := range append(removed, kept...) {
		c.keys[k] = true
	}
	r := New(base, c)
	q := model.CapacityCleanupBatch{RunID: "cap-20260930-120000-abcdef", Product: "p", Devices: []string{"cap", "business"}, RemoveDevices: []string{"cap"}}
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

func TestCapacityFixtureDiscoveryThroughStorageComposition(t *testing.T) {
	ctx := context.Background()
	base := memory.NewRepository()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	telemetry, err := clickhouse.New(ctx, server.URL, base)
	if err != nil {
		t.Fatal(err)
	}
	var repository ports.Repository = New(telemetry, &cleanupRedis{})
	lister, ok := repository.(ports.CapacityFixtureLister)
	if !ok {
		t.Fatal("fixture capability hidden by Redis/ClickHouse decorators")
	}
	for _, id := range []string{"a", "b"} {
		p := model.Product{TenantID: "t", ID: id, Name: "容量测试标准设备 " + id, Description: "capacity-test 自动创建", ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED"}
		if err = repository.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"d1", "d2"} {
		if err = repository.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ProductID: "a", ID: id, Name: "容量测试 " + id, RegistrationSource: "ONBOARDING", AccessKey: id}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := lister.ListCapacityFixtureProducts(ctx, "t", "", 1)
	if err != nil || len(page) != 1 || page[0].ProductID != "a" || page[0].DeviceCount != 2 {
		t.Fatal("first product page", page, err)
	}
	if err = lister.PrepareCapacityFixture(ctx, "t", "a", page[0].Fingerprint); err != nil {
		t.Fatal("fixture preparation hidden by decorators", err)
	}
	p, err := repository.GetProduct(ctx, "t", "a")
	if err != nil || p.Status != "DISABLED" {
		t.Fatal("preparation did not reach base storage", p, err)
	}
	page, err = lister.ListCapacityFixtureProducts(ctx, "t", "a", 1)
	if err != nil || len(page) != 1 || page[0].ProductID != "b" {
		t.Fatal("second product page", page, err)
	}
	ids, err := lister.ListCapacityFixtureDevices(ctx, "t", "a", "", 1)
	if err != nil || len(ids) != 1 || ids[0] != "d1" {
		t.Fatal("first device page", ids, err)
	}
	ids, err = lister.ListCapacityFixtureDevices(ctx, "t", "a", "d1", 1)
	if err != nil || len(ids) != 1 || ids[0] != "d2" {
		t.Fatal("second device page", ids, err)
	}
	page, err = lister.ListCapacityFixtureProducts(ctx, "other", "", 100)
	if err != nil || len(page) != 0 {
		t.Fatal("decorators expanded tenant scope", page, err)
	}
}
