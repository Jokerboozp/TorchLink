package redisadapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type cleanupRedis struct {
	redis.UniversalClient
	keys map[string]bool
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
func TestCapacityCleanupInvalidatesOnlyScopedCacheAndRetries(t *testing.T) {
	ctx := context.Background()
	base := memory.NewRepository()
	_ = base.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Name: model.CapacityFixtureProductName("p"), Description: model.CapacityFixtureDescription, ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED"})
	_ = base.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ProductID: "p", ID: "cap", Name: "容量测试 cap", RegistrationSource: "ONBOARDING", AccessKey: "cap"})
	_, _ = base.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", ProductID: "p", DeviceID: "cap", MessageID: "raw", ParseAttemptedAt: 1})
	c := &cleanupRedis{keys: map[string]bool{}, fail: true}
	removed := []string{stateKey("t", "cap"), latestKey("t", "cap")}
	kept := []string{stateKey("other", "cap"), stateKey("t", "business"), latestKey("t", "business")}
	for _, k := range append(removed, kept...) {
		c.keys[k] = true
	}
	r := New(base, c)
	q := model.CapacityCleanupBatch{RunID: "cap-20260930-120000-abcdef", Product: "p", Devices: []string{"cap"}}
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
	lister, ok := repository.(ports.CapacityDataCleaner)
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
	products, err := lister.ListCapacityFixtureProducts(ctx, "t")
	if err != nil || len(products) != 2 || products[0].ProductID != "a" || products[0].DeviceCount != 2 {
		t.Fatal("product listing", products, err)
	}
	ids, err := lister.ListCapacityFixtureDevices(ctx, "t", "a", "d1", 1)
	if err != nil || len(ids) != 1 || ids[0] != "d2" {
		t.Fatal("device page", ids, err)
	}
	if products, err = lister.ListCapacityFixtureProducts(ctx, "other"); err != nil || len(products) != 0 {
		t.Fatal("decorators expanded tenant scope", products, err)
	}
}

func TestDeviceStateCacheExpiresAndFollowsDeletion(t *testing.T) {
	addr := os.Getenv("IOT_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("IOT_TEST_REDIS_ADDR is not configured")
	}
	ctx := context.Background()
	client := NewClient(Options{Addr: addr, Password: os.Getenv("IOT_TEST_REDIS_PASSWORD")})
	defer client.Close()
	base := memory.NewRepository()
	tenant := "cache-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := base.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: "d", ProductID: "p", Name: "d"}); err != nil {
		t.Fatal(err)
	}
	r := New(base, client)
	if ok, err := r.UpsertDeviceStateIf(ctx, model.DeviceState{TenantID: tenant, DeviceID: "d", BusinessStatus: "ONLINE"}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	ttl, err := client.PTTL(ctx, stateKey(tenant, "d")).Result()
	if err != nil || ttl <= 0 || ttl > stateTTL {
		t.Fatalf("cached state ttl %v %v", ttl, err)
	}
	if err = r.DeleteResource(ctx, tenant, "device", "d"); err != nil {
		t.Fatal(err)
	}
	if n, _ := client.Exists(ctx, stateKey(tenant, "d")).Result(); n != 0 {
		t.Fatal("deleted device kept its cached state")
	}
}
