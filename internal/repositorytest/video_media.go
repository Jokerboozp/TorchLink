package repositorytest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"iot-platform/internal/model"
)

type videoMediaRepository interface {
	SaveVideoEvent(context.Context, model.VideoAlarmEvent) (bool, error)
	GetVideoEvent(context.Context, string, string) (model.VideoAlarmEvent, error)
	ListPendingVideoEvents(context.Context, int) ([]model.VideoAlarmEvent, error)
}

func VideoMediaRetries(t *testing.T, repo videoMediaRepository) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	for i := 0; i < 110; i++ {
		v := model.VideoAlarmEvent{TenantID: "media-retry", EventID: fmt.Sprintf("delayed-%d", i), EventTime: int64(i), Raw: map[string]any{"mediaTransferStatus": "FAILED", "mediaRetryAt": now + 60000, "mediaLastAttemptAt": now}}
		if _, err := repo.SaveVideoEvent(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []model.VideoAlarmEvent{
		{TenantID: "media-retry", EventID: "ready-new", EventTime: 1000, Raw: map[string]any{"mediaTransferStatus": "PENDING"}},
		{TenantID: "media-retry", EventID: "ready-old", EventTime: 1, Raw: map[string]any{"mediaTransferStatus": "FAILED", "mediaRetryAt": now - 1, "mediaLastAttemptAt": now - 60000}},
	} {
		if _, err := repo.SaveVideoEvent(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	items, err := repo.ListPendingVideoEvents(ctx, 100)
	if err != nil || len(items) != 2 || items[0].EventID != "ready-new" || items[1].EventID != "ready-old" {
		t.Fatalf("delayed failures starved ready media: %+v %v", items, err)
	}
	got, err := repo.GetVideoEvent(ctx, "media-retry", "delayed-1")
	if err != nil || got.EventID != "delayed-1" {
		t.Fatalf("durable event: %+v %v", got, err)
	}
	if _, err = repo.GetVideoEvent(ctx, "other-tenant", "delayed-1"); err == nil {
		t.Fatal("cross-tenant video event returned")
	}
}
