package repositorytest

import (
	"context"
	"testing"
	"time"

	"iot-platform/internal/model"
)

type rawPublishRepository interface {
	SaveRawIndex(context.Context, model.RawArchiveIndex) (bool, error)
	ListPendingRawIndexes(context.Context, int) ([]model.RawArchiveIndex, error)
	CountStalledRawIndexes(context.Context) (int, error)
}

// RawPublishRetriesStopAtTheLimit checks that pending queue retries prefer
// messages with fewer failures, so a few permanently failing messages cannot
// starve newer ones, and that messages at the attempt limit leave the retry
// list and are counted as stalled instead.
func RawPublishRetriesStopAtTheLimit(t *testing.T, repo rawPublishRepository) {
	t.Helper()
	ctx := context.Background()
	// Old enough that every backoff below has elapsed.
	archived := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	for _, v := range []struct {
		id       string
		attempts int
		at       int64
	}{{"raw-retry-failing", 5, archived}, {"raw-retry-fresh", 0, archived + 1000}, {"raw-retry-dead", model.MaxRawPublishAttempts, archived - 1000}} {
		if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{MessageID: v.id, TenantID: "raw-retry", ProductID: "p", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ObjectKey: v.id, PayloadHash: "h", PayloadSize: 1, ReceivedAt: v.at, ArchivedAt: v.at, PublishAttempts: v.attempts, LastPublishError: "broker unavailable"}); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := repo.ListPendingRawIndexes(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, v := range pending {
		if v.TenantID == "raw-retry" {
			ids = append(ids, v.MessageID)
		}
	}
	if len(ids) != 2 || ids[0] != "raw-retry-fresh" || ids[1] != "raw-retry-failing" {
		t.Fatalf("pending retries = %v, want fewer failures first and no message at the limit", ids)
	}
	stalled, err := repo.CountStalledRawIndexes(ctx)
	if err != nil || stalled != 1 {
		t.Fatalf("stalled = %d err=%v, want 1", stalled, err)
	}
}
