package repositorytest

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"sync/atomic"
	"testing"
)

func ProtocolMarket(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	entry := model.ProtocolMarketEntry{TenantID: "market-test", ProtocolID: "p", Version: "1", SubmittedBy: "publisher", Status: "SUBMITTED", Generation: 1}
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := repo.SubmitProtocolMarket(ctx, entry)
			if err == nil {
				created.Add(1)
			} else if !errors.Is(err, model.ErrMarketConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("duplicate submission", created.Load())
	}
	if _, err := repo.ReviewProtocolMarket(ctx, entry.TenantID, "p", "1", "publisher", "APPROVED", "self review", 1); !errors.Is(err, model.ErrMarketReview) {
		t.Fatal("self review accepted", err)
	}
	var approved atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ReviewProtocolMarket(ctx, entry.TenantID, "p", "1", "reviewer", "APPROVED", "verified", 1)
			if err == nil {
				approved.Add(1)
			} else if !errors.Is(err, model.ErrMarketConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if approved.Load() != 1 {
		t.Fatal("duplicate approval", approved.Load())
	}
	if _, err := repo.ReviewProtocolMarket(ctx, "other", "p", "1", "reviewer", "WITHDRAWN", "foreign", 2); err == nil {
		t.Fatal("cross tenant review accepted")
	}
	if list, err := repo.ListProtocolMarket(ctx, "other"); err != nil || len(list) != 0 {
		t.Fatal("cross tenant list", err)
	}
	result, err := repo.ReviewProtocolMarket(ctx, entry.TenantID, "p", "1", "reviewer", "WITHDRAWN", "retired", 2)
	if err != nil || result.Status != "WITHDRAWN" || len(result.Reviews) != 2 || result.Generation != 3 {
		t.Fatal("withdrawal history", result, err)
	}
	if _, err := repo.ReviewProtocolMarket(ctx, entry.TenantID, "p", "1", "reviewer", "APPROVED", "restore", 3); !errors.Is(err, model.ErrMarketReview) {
		t.Fatal("withdrawn version reused", err)
	}
}
