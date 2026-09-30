package postgres

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"testing"
)

func TestGovernanceVideoEventHistoricalReader(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	if err := r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2"} {
		if _, err := r.SaveVideoEvent(ctx, model.VideoAlarmEvent{TenantID: "t", EventID: id, CameraID: "camera", EventTime: 100, Raw: map[string]any{"governanceDeviceId": "forged"}}); err != nil {
			t.Fatal(err)
		}
	}
	v, err := r.GetGovernanceVideoEvent(ctx, "t", "1")
	if err != nil || v.DeviceID != "a" || v.CurrentDeviceID != "a" {
		t.Fatal(v, err)
	}
	if _, err = r.GetGovernanceVideoEvent(ctx, "other", "1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross tenant read", err)
	}
	page, err := r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{DeviceIDs: []string{"a"}, Start: 1, End: 101, Limit: 1, Cursor: "100:1"})
	if err != nil || len(page) != 1 || page[0].Event.EventID != "2" {
		t.Fatal(page, err)
	}
	if err = r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "b"}); err != nil {
		t.Fatal(err)
	}
	v.Event.Raw = map[string]any{"mediaTransferStatus": "STORED", "governanceDeviceId": "b"}
	if err = r.UpdateVideoEvent(ctx, v.Event); err != nil {
		t.Fatal(err)
	}
	v, err = r.GetGovernanceVideoEvent(ctx, "t", "1")
	if err != nil || v.DeviceID != "a" || v.CurrentDeviceID != "b" {
		t.Fatal("async update reset binding", v, err)
	}
	for _, ids := range [][]string{{"a"}, {"b"}} {
		page, err = r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{DeviceIDs: ids, Start: 1, End: 101, Limit: 10})
		if err != nil || len(page) != 0 {
			t.Fatal("rebind leaked scoped history", page, err)
		}
	}
	if _, err = r.pool.Exec(ctx, `UPDATE video_alarm_event SET body=jsonb_set(body,'{raw}','{}') WHERE tenant_id='t' AND event_id='2'`); err != nil {
		t.Fatal(err)
	}
	page, err = r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{DeviceIDs: []string{"a", "b"}, Start: 1, End: 101, Limit: 10})
	if err != nil || len(page) != 1 {
		t.Fatal("legacy binding guessed", page, err)
	}
	page, err = r.ListGovernanceVideoEvents(ctx, "t", ports.VideoEventFilter{AllDevices: true, Start: 1, End: 101, Limit: 10})
	if err != nil || len(page) != 2 {
		t.Fatal("all device nil scope query", page, err)
	}
}
