package redisadapter

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// Set IOT_TEST_REDIS_ADDR (host:port of a disposable Redis) to run.
func TestRedisRateLimiterSharesOneBudget(t *testing.T) {
	addr := os.Getenv("IOT_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("IOT_TEST_REDIS_ADDR is not configured")
	}
	ctx := context.Background()
	client := NewClient(Options{Addr: addr, Password: os.Getenv("IOT_TEST_REDIS_PASSWORD")})
	defer client.Close()
	a, b := NewRateLimiter(client), NewRateLimiter(client)
	key := fmt.Sprintf("test-%d", time.Now().UnixNano())
	allowed := 0
	for i := 0; i < 10; i++ {
		for _, l := range []*RateLimiter{a, b} {
			if ok, err := l.Allow(ctx, key, 6, time.Minute); err != nil {
				t.Fatal(err)
			} else if ok {
				allowed++
			}
		}
	}
	if allowed != 6 {
		t.Fatalf("two limiters admitted %d, want one shared budget of 6", allowed)
	}
	if n, reset, err := a.Hits(ctx, key, time.Minute); err != nil || n != 20 || reset <= 0 {
		t.Fatal(n, reset, err)
	}
	if err := b.Reset(ctx, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := a.Hits(ctx, key, time.Minute); n != 0 {
		t.Fatal("reset not shared")
	}
}

func TestCachedStateDecodesVersionedAndLegacyEntries(t *testing.T) {
	if s, ok := decodeCachedState([]byte(`{"v":3,"s":{"tenantId":"t","deviceId":"d","businessStatus":"ONLINE"}}`)); !ok || s.Version != 3 || s.DeviceID != "d" {
		t.Fatal("versioned entry", s, ok)
	}
	if s, ok := decodeCachedState([]byte(`{"tenantId":"t","deviceId":"d","businessStatus":"ALARM"}`)); !ok || s.BusinessStatus != "ALARM" {
		t.Fatal("legacy entry", s, ok)
	}
	if _, ok := decodeCachedState([]byte(`garbage`)); ok {
		t.Fatal("garbage decoded")
	}
}
