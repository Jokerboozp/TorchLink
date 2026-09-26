package memory

import (
	"context"
	"testing"

	"iot-platform/internal/model"
)

func TestUpdateVideoEventReplacesPendingRecord(t *testing.T) {
	repo := NewRepository()
	event := model.VideoAlarmEvent{
		TenantID: "tenant_001",
		EventID:  "event_001",
		Raw:      map[string]any{"mediaTransferStatus": "PENDING"},
	}
	created, err := repo.SaveVideoEvent(context.Background(), event)
	if err != nil || !created {
		t.Fatalf("SaveVideoEvent() created=%v err=%v", created, err)
	}

	event.Raw["mediaTransferStatus"] = "COMPLETED"
	if err := repo.UpdateVideoEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListPendingVideoEvents(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("updated event remained pending: %#v", pending)
	}
}
