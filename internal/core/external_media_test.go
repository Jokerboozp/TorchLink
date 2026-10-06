package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/netguard"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/externaldata"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func externalMediaEngine(t *testing.T, repo ports.Repository) *Engine {
	t.Helper()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Media fixtures serve from httptest on the loopback address.
	e.MediaOutbound = netguard.Loopback
	return e
}

func externalMediaSource(t *testing.T, repo ports.Repository, rawURL string) externaldata.Entry {
	t.Helper()
	u, _ := url.Parse(rawURL)
	source := externaldata.Source{ID: "external-source", Name: "视频平台", Username: "exec", Enabled: true, AllowedHosts: []string{u.Host}}
	body, _ := json.Marshal(source)
	entry, err := repo.ExternalDataStore().Put(context.Background(), externaldata.Entry{TenantID: "media-tenant", Kind: "source", ID: source.ID, Body: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func externalMediaEvent(rawURL string) model.VideoAlarmEvent {
	return model.VideoAlarmEvent{EventID: "event-one", TenantID: "media-tenant", Source: "external-source", CameraID: "camera-one", AlarmType: "FIRE", EventTime: time.Now().UnixMilli(), SnapshotURL: rawURL, Raw: map[string]any{"externalSourceId": "external-source", "deviceId": "platform-device", "mediaTransferStatus": "PENDING"}}
}

func TestExternalMediaSourceAllowlistAndTenant(t *testing.T) {
	var unwanted atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { unwanted.Store(true); io.WriteString(w, "other") }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, other.URL, http.StatusFound)
			return
		}
		io.WriteString(w, "image")
	}))
	defer server.Close()
	repo := memory.NewRepository()
	e := externalMediaEngine(t, repo)
	sourceEntry := externalMediaSource(t, repo, server.URL)
	v := externalMediaEvent(server.URL + "/snapshot.jpg")
	if err := e.validateExternalVideoMediaURLs(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	stored, err := e.transferVideoURL(context.Background(), v, v.SnapshotURL, "snapshot")
	if err != nil || !strings.HasPrefix(stored, "local://video-alarm/") {
		t.Fatalf("source allowlist did not enable media: %s %v", stored, err)
	}
	v.Raw["allowedHosts"] = []string{strings.TrimPrefix(other.URL, "http://")}
	if _, err = e.transferVideoURL(context.Background(), v, other.URL, "snapshot"); err == nil || unwanted.Load() {
		t.Fatal("other port or raw host override accepted")
	}
	if _, err = e.transferVideoURL(context.Background(), v, server.URL+"/redirect", "snapshot"); err == nil || unwanted.Load() {
		t.Fatal("redirect escaped source host:port allowlist")
	}
	v.TenantID = "different-tenant"
	if err = e.validateExternalVideoMediaURLs(context.Background(), v); err == nil {
		t.Fatal("source inherited across tenants")
	}
	v.TenantID = "media-tenant"
	var source externaldata.Source
	json.Unmarshal(sourceEntry.Body, &source)
	source.Enabled = false
	sourceEntry.Body, _ = json.Marshal(source)
	if _, err = repo.ExternalDataStore().Put(context.Background(), sourceEntry, sourceEntry.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = e.transferVideoURL(context.Background(), v, v.SnapshotURL, "snapshot"); err == nil {
		t.Fatal("disabled source still downloaded media")
	}
}

func TestExternalMediaPartialFailureRetainsURLsAndRetries(t *testing.T) {
	var recoverSnapshot atomic.Bool
	var snapshotRequests, clipRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/snapshot.jpg" {
			snapshotRequests.Add(1)
			if !recoverSnapshot.Load() {
				http.Error(w, "temporary", 500)
				return
			}
		} else {
			clipRequests.Add(1)
		}
		io.WriteString(w, "media")
	}))
	defer server.Close()
	ctx := context.Background()
	repo := memory.NewRepository()
	e := externalMediaEngine(t, repo)
	externalMediaSource(t, repo, server.URL)
	v := externalMediaEvent(server.URL + "/snapshot.jpg")
	v.VideoClipURL = server.URL + "/clip.mp4"
	if _, err := repo.SaveVideoEvent(ctx, v); err != nil {
		t.Fatal(err)
	}
	linked := model.Alarm{ID: "recovered-alarm", TenantID: v.TenantID, DeviceID: "platform-device", Source: "video", RuleID: "external-rule-1", Status: "RECOVERED", Details: map[string]any{"videoEvent": v}}
	if _, _, err := repo.UpsertAlarm(ctx, linked); err != nil {
		t.Fatal(err)
	}
	other := v
	other.EventID = "different-event"
	other.SnapshotURL = "https://different.invalid/keep.jpg"
	if _, _, err := repo.UpsertAlarm(ctx, model.Alarm{ID: "other-alarm", TenantID: v.TenantID, DeviceID: linked.DeviceID, Source: "video", RuleID: "external-rule-2", Status: "ACTIVE", Details: map[string]any{"videoEvent": other}}); err != nil {
		t.Fatal(err)
	}
	e.processVideoMedia(ctx, v)
	failed, err := repo.GetVideoEvent(ctx, v.TenantID, v.EventID)
	if err != nil {
		t.Fatal(err)
	}
	pending, _ := repo.ListPendingVideoEvents(ctx, 10)
	if len(pending) != 0 {
		t.Fatal("failed media was not backed off")
	}
	if failed.SnapshotURL != v.SnapshotURL || !strings.HasPrefix(failed.VideoClipURL, "local://") || failed.Raw["snapshotTransferStatus"] != "FAILED" || failed.Raw["clipTransferStatus"] != "STORED" {
		t.Fatalf("partial transfer lost retry target: %+v", failed)
	}
	if v.Raw["mediaTransferStatus"] != "PENDING" {
		t.Fatal("input event mutated")
	}
	a, _ := repo.GetAlarm(ctx, v.TenantID, linked.ID)
	attached := a.Details["videoEvent"].(model.VideoAlarmEvent)
	if a.Status != "RECOVERED" || attached.Raw["mediaTransferStatus"] != "FAILED" {
		t.Fatalf("failure not reflected in recovered alarm: %+v", a)
	}
	recoverSnapshot.Store(true)
	if _, err = e.QueueVideoMediaRetry(ctx, v.TenantID, v.EventID); err != nil {
		t.Fatal(err)
	}
	if err = e.retryPendingVideoMediaOnce(ctx); err != nil {
		t.Fatal(err)
	}
	pending, _ = repo.ListPendingVideoEvents(ctx, 10)
	if len(pending) != 0 || snapshotRequests.Load() != 2 || clipRequests.Load() != 1 {
		t.Fatalf("retry repeated stored attachment or left pending: %d %d %d", len(pending), snapshotRequests.Load(), clipRequests.Load())
	}
	a, _ = repo.GetAlarm(ctx, v.TenantID, linked.ID)
	attached = a.Details["videoEvent"].(model.VideoAlarmEvent)
	if a.Status != "RECOVERED" || !strings.HasPrefix(attached.SnapshotURL, "local://") || attached.Raw["mediaTransferStatus"] != "STORED" {
		t.Fatalf("successful media not linked: %+v", a)
	}
	a, _ = repo.GetAlarm(ctx, v.TenantID, "other-alarm")
	if a.Details["videoEvent"].(model.VideoAlarmEvent).SnapshotURL != other.SnapshotURL {
		t.Fatal("media overwrote another event on same camera/device")
	}
}

func TestExternalMediaInvalidURLRemainsFailed(t *testing.T) {
	repo := memory.NewRepository()
	e := externalMediaEngine(t, repo)
	externalMediaSource(t, repo, "https://media.example:443")
	v := externalMediaEvent("javascript:invalid")
	repo.SaveVideoEvent(context.Background(), v)
	e.processVideoMedia(context.Background(), v)
	saved, err := repo.GetVideoEvent(context.Background(), v.TenantID, v.EventID)
	if err != nil || saved.Raw["mediaTransferStatus"] != "FAILED" || saved.SnapshotURL != v.SnapshotURL {
		t.Fatalf("invalid media incorrectly marked stored: %+v %v", saved, err)
	}
}

func TestExternalMediaMatchesExactEventsAcrossAlarmPages(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	e := externalMediaEngine(t, repo)
	event := externalMediaEvent("local://video-alarm/stored.jpg")
	for i := 0; i < 260; i++ {
		a := model.Alarm{ID: fmt.Sprintf("unrelated-%d", i), TenantID: event.TenantID, DeviceID: "same-device", RuleID: fmt.Sprintf("rule-%d", i), Status: "ACTIVE", LastTriggeredAt: int64(1000 + i)}
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	original := event
	original.SnapshotURL = "https://before.invalid/image.jpg"
	raw, _ := json.Marshal(original)
	var generic map[string]any
	json.Unmarshal(raw, &generic)
	for _, slot := range []string{"videoEvent", "latestVideoEvent", "videoConfirmation"} {
		a := model.Alarm{ID: slot, TenantID: event.TenantID, DeviceID: "real-device", RuleID: slot, Source: "device", Status: "CLOSED", LastTriggeredAt: 1, Details: map[string]any{slot: generic}}
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.updateVideoAlarmMedia(ctx, event); err != nil {
		t.Fatal(err)
	}
	for _, slot := range []string{"videoEvent", "latestVideoEvent", "videoConfirmation"} {
		a, err := repo.GetAlarm(ctx, event.TenantID, slot)
		if err != nil || a.Details[slot].(model.VideoAlarmEvent).SnapshotURL != event.SnapshotURL || a.Status != "CLOSED" {
			t.Fatalf("slot %s not updated: %+v %v", slot, a, err)
		}
	}
}

type failMediaAlarmUpdate struct {
	*memory.Repository
	fail bool
}

func (r *failMediaAlarmUpdate) UpdateAlarmIf(ctx context.Context, a model.Alarm) (bool, error) {
	if r.fail {
		return false, errors.New("temporary storage failure")
	}
	return r.Repository.UpdateAlarmIf(ctx, a)
}

func TestExternalMediaRetriesAlarmLinkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "image") }))
	defer server.Close()
	ctx := context.Background()
	repo := &failMediaAlarmUpdate{Repository: memory.NewRepository(), fail: true}
	e := externalMediaEngine(t, repo)
	externalMediaSource(t, repo, server.URL)
	v := externalMediaEvent(server.URL + "/snapshot.jpg")
	repo.SaveVideoEvent(ctx, v)
	repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm", TenantID: v.TenantID, DeviceID: "device", RuleID: "rule", Status: "ACTIVE", Details: map[string]any{"videoEvent": v}})
	e.processVideoMedia(ctx, v)
	saved, err := repo.GetVideoEvent(ctx, v.TenantID, v.EventID)
	if err != nil || !strings.HasPrefix(saved.SnapshotURL, "local://") || saved.Raw["mediaTransferStatus"] != "FAILED" {
		t.Fatalf("failed link lost durable retry: %+v %v", saved, err)
	}
	repo.fail = false
	if _, err = e.QueueVideoMediaRetry(ctx, v.TenantID, v.EventID); err != nil {
		t.Fatal(err)
	}
	if err := e.retryPendingVideoMediaOnce(ctx); err != nil {
		t.Fatal(err)
	}
	pending, _ := repo.ListPendingVideoEvents(ctx, 10)
	a, _ := repo.GetAlarm(ctx, v.TenantID, "alarm")
	if len(pending) != 0 || !strings.HasPrefix(a.Details["videoEvent"].(model.VideoAlarmEvent).SnapshotURL, "local://") {
		t.Fatal("link retry did not finish")
	}
}
