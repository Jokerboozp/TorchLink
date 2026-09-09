package repositorytest

import (
	"context"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"testing"
	"time"
)

func ExecutionLease(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan model.ExecutionLease, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, ok, err := repo.AcquireExecutionLease(ctx, "lease-test", "profile/one", fmt.Sprintf("owner-%d", i), "http://gateway", time.Second)
			if err != nil {
				t.Error(err)
			}
			if ok {
				results <- v
			}
		}(i)
	}
	wg.Wait()
	close(results)
	var first model.ExecutionLease
	count := 0
	for v := range results {
		count++
		first = v
	}
	if count != 1 {
		t.Fatalf("%d owners acquired one resource", count)
	}
	renewed, ok, err := repo.AcquireExecutionLease(ctx, first.TenantID, first.Resource, first.Owner, first.Endpoint, time.Second)
	if err != nil || !ok || renewed.Token != first.Token {
		t.Fatal("renewal changed token", err)
	}
	if _, ok, err := repo.AcquireExecutionLease(ctx, "another-tenant", first.Resource, "different", first.Endpoint, time.Second); err != nil || !ok {
		t.Fatal("tenant namespace collided", err)
	}
	if err := repo.ReleaseExecutionLease(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, ok, err := repo.AcquireExecutionLease(ctx, first.TenantID, first.Resource, "next", first.Endpoint, time.Second)
	if err != nil || !ok || second.Token <= first.Token {
		t.Fatal("takeover lacks fencing", err)
	}
	if err := repo.ReleaseExecutionLease(ctx, first); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetExecutionLease(ctx, first.TenantID, first.Resource)
	if err != nil || current.Owner != second.Owner {
		t.Fatal("stale owner released successor", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := repo.GetExecutionLease(ctx, first.TenantID, first.Resource); err == nil {
		t.Fatal("expired ownership remained valid")
	}
	third, ok, err := repo.AcquireExecutionLease(ctx, first.TenantID, first.Resource, "third", first.Endpoint, time.Second)
	if err != nil || !ok || third.Token <= second.Token {
		t.Fatal("expired lease not fenced", err)
	}
}
