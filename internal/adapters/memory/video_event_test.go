package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"testing"
)

func TestGovernanceVideoEventCapturedBindingAndStableCursor(t *testing.T) {
	ctx := context.Background()
	r := NewRepository()
	if err := r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2"} {
		if _, err := r.SaveVideoEvent(ctx, model.VideoAlarmEvent{TenantID: "t", EventID: id, CameraID: "camera", EventTime: 100, Raw: map[string]any{"governanceDeviceId": "forged", "mediaTransferStatus": "PENDING"}}); err != nil {
			t.Fatal(err)
		}
	}
	v, err := r.GetGovernanceVideoEvent(ctx, "t", "1")
	if err != nil || v.DeviceID != "a" || v.CurrentDeviceID != "a" {
		t.Fatal(v, err)
	}
	if _, err = r.GetGovernanceVideoEvent(ctx, "other", "1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross tenant video read", err)
	}
	f := ports.VideoEventFilter{DeviceIDs: []string{"a"}, Start: 1, End: 101, Limit: 1}
	page, err := r.ListGovernanceVideoEvents(ctx, "t", f)
	if err != nil || len(page) != 1 || page[0].Event.EventID != "1" {
		t.Fatal(page, err)
	}
	f.Cursor = "100:1"
	page, err = r.ListGovernanceVideoEvents(ctx, "t", f)
	if err != nil || len(page) != 1 || page[0].Event.EventID != "2" {
		t.Fatal(page, err)
	}
	if err = r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "b"}); err != nil {
		t.Fatal(err)
	}
	updated := v.Event
	updated.Raw = map[string]any{"governanceDeviceId": "b", "mediaTransferStatus": "STORED"}
	updated.SnapshotURL = "local://video-alarm/private"
	if err = r.UpdateVideoEvent(ctx, updated); err != nil {
		t.Fatal(err)
	}
	v, err = r.GetGovernanceVideoEvent(ctx, "t", "1")
	if err != nil || v.DeviceID != "a" || v.CurrentDeviceID != "b" || v.Event.SnapshotURL == "" {
		t.Fatal("media update lost historical binding", v, err)
	}
	for _, ids := range [][]string{{"a"}, {"b"}} {
		page, err = r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{DeviceIDs: ids, Start: 1, End: 101, Limit: 10})
		if err != nil || len(page) != 0 {
			t.Fatal("rebind leaked partial membership", ids, page, err)
		}
	}
	page, err = r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{DeviceIDs: []string{"a", "b"}, Start: 1, End: 101, Limit: 10})
	if err != nil || len(page) != 2 {
		t.Fatal(page, err)
	}
	// Emulate historical rows written before the server-owned binding existed.
	r.mu.Lock()
	r.video[key("t", "legacy")] = model.VideoAlarmEvent{TenantID: "t", EventID: "legacy", CameraID: "camera", EventTime: 99}
	r.mu.Unlock()
	page, err = r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{DeviceIDs: []string{"a", "b"}, Start: 1, End: 101, Limit: 10})
	if err != nil || len(page) != 2 {
		t.Fatal("legacy current mapping invented provenance", page, err)
	}
	page, err = r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{AllDevices: true, Start: 1, End: 101, Limit: 10})
	if err != nil || len(page) != 3 || page[0].DeviceID != "" {
		t.Fatal("whole tenant history unavailable", page, err)
	}
}
