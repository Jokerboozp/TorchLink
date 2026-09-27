package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLocalFixedWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLocal()
	l.Now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(ctx, "k", 3, time.Second); !ok {
			t.Fatal("rejected within budget")
		}
	}
	if ok, _ := l.Allow(ctx, "k", 3, time.Second); ok {
		t.Fatal("budget exceeded")
	}
	if n, reset, _ := l.Hits(ctx, "k", time.Second); n != 3 || reset != time.Second {
		t.Fatal(n, reset)
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow(ctx, "k", 3, time.Second); !ok {
		t.Fatal("next window rejected")
	}
	_ = l.Reset(ctx, "k", time.Second)
	if n, _, _ := l.Hits(ctx, "k", time.Second); n != 0 {
		t.Fatal("reset kept count")
	}
}

type fakeShared struct {
	err   error
	calls int
}

func (f *fakeShared) Allow(context.Context, string, int, time.Duration) (bool, error) {
	f.calls++
	return f.err == nil, f.err
}
func (f *fakeShared) Hits(context.Context, string, time.Duration) (int, time.Duration, error) {
	return 7, time.Second, f.err
}
func (f *fakeShared) Reset(context.Context, string, time.Duration) error { return f.err }

func TestClusterFallsBackWithoutWideningTheBudget(t *testing.T) {
	ctx := context.Background()
	shared := &fakeShared{}
	c := NewCluster(shared, 4)
	if ok, _ := c.Allow(ctx, "k", 20, time.Minute); !ok || shared.calls != 1 {
		t.Fatal("shared limiter not used")
	}
	if n, _, _ := c.Hits(ctx, "k", time.Minute); n != 7 {
		t.Fatal("shared count ignored", n)
	}
	shared.err = errors.New("redis down")
	allowed := 0
	for i := 0; i < 20; i++ {
		if ok, _ := c.Allow(ctx, "k", 20, time.Minute); ok {
			allowed++
		}
	}
	// 4 replicas each fall back to 20/4 = 5, never 20 each.
	if allowed != 5 || c.SharedErrors() == 0 {
		t.Fatalf("fallback allowed %d, want 5", allowed)
	}
	if n, _, _ := c.Hits(ctx, "k", time.Minute); n != 20 {
		t.Fatalf("fallback count must be scaled to the cluster: %d", n)
	}
	single := NewCluster(nil, 4)
	allowed = 0
	for i := 0; i < 30; i++ {
		if ok, _ := single.Allow(ctx, "k", 20, time.Minute); ok {
			allowed++
		}
	}
	if allowed != 20 {
		t.Fatal("single process must keep the full budget", allowed)
	}
}
