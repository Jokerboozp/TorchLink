package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func TestExternalAlarmMediaDownloadRangeAndPermissions(t *testing.T) {
	api, repo, ctx := externalAPIFixture(t)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, payload any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, payload, status)
	}
	admin := req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": "root", "password": api.cfg.AdminPassword}, 200)["accessToken"].(string)
	users := map[string]string{}
	for _, name := range []string{"media-viewer", "media-retry"} {
		perms := []string{"menu:devices", "menu:alarms"}
		if name == "media-retry" {
			perms = append(perms, "POST /api/v1/alarms/:id/media/retry")
		}
		req("POST", "/api/v1/access/roles", admin, map[string]any{"id": name, "name": name, "permissions": perms, "deviceScope": "selected", "deviceIds": []string{"ext-device"}}, 200)
		req("POST", "/api/v1/access/users", admin, map[string]any{"username": name, "password": "test-media-password", "enabled": true, "roleIds": []string{name}, "deviceScope": "inherit"}, 200)
		users[name] = req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": name, "password": "test-media-password"}, 200)["accessToken"].(string)
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	videoBytes := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}, []byte("video-test-payload")...)
	v := model.VideoAlarmEvent{EventID: "media-event", TenantID: externalTestTenant, CameraID: "ext-camera", EventTime: 1700000000000, Raw: map[string]any{"mediaTransferStatus": "STORED", "snapshotTransferStatus": "STORED", "clipTransferStatus": "STORED"}}
	keyBase := fmt.Sprintf("%s/%s/%s/", v.TenantID, time.UnixMilli(v.EventTime).UTC().Format("2006/01/02"), v.EventID)
	snapshotKey := keyBase + "snapshot-" + v.CameraID + ".png"
	var err error
	v.SnapshotURL, err = api.engine.Archive.PutObject(ctx, "video-alarm", snapshotKey, bytes.NewReader(pngData.Bytes()), int64(pngData.Len()), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	v.VideoClipURL, err = api.engine.Archive.PutObject(ctx, "video-alarm", keyBase+"clip-"+v.CameraID+".mp4", bytes.NewReader(videoBytes), int64(len(videoBytes)), "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveVideoEvent(ctx, v); err != nil {
		t.Fatal(err)
	}
	a := model.Alarm{ID: "media-alarm", TenantID: externalTestTenant, DeviceID: "ext-device", Status: "RECOVERED", Source: "video", RuleID: "media", Details: map[string]any{"videoEvent": v}}
	if _, _, err = repo.UpsertAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	other := a
	other.ID = "other-alarm"
	other.DeviceID = "other-device"
	other.RuleID = "other-media"
	if _, _, err = repo.UpsertAlarm(ctx, other); err != nil {
		t.Fatal(err)
	}
	download := func(path, token, rangeHeader string, status int) ([]byte, http.Header) {
		t.Helper()
		r, _ := http.NewRequest("GET", server.URL+path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if rangeHeader != "" {
			r.Header.Set("Range", rangeHeader)
		}
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != status {
			t.Fatalf("download status %d want %d: %s (%v)", response.StatusCode, status, data, err)
		}
		return data, response.Header
	}
	path := "/api/v1/alarms/media-alarm/media/"
	body, headers := download(path+"snapshot", users["media-viewer"], "", 200)
	if !bytes.Equal(body, pngData.Bytes()) || headers.Get("Content-Type") != "image/png" || !strings.Contains(headers.Get("Content-Disposition"), "snapshot.png") || headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("invalid download response %v", headers)
	}
	body, headers = download(path+"clip", users["media-viewer"], "bytes=0-7", 206)
	if !bytes.Equal(body, videoBytes[:8]) || headers.Get("Content-Type") != "video/mp4" || headers.Get("Content-Range") == "" {
		t.Fatalf("invalid range response %s %v", body, headers)
	}
	download(path+"snapshot", "", "", 401)
	download("/api/v1/alarms/other-alarm/media/snapshot", users["media-viewer"], "", 403)
	req("POST", path+"retry", users["media-viewer"], nil, 403)
	// Persisted media is newer than a stale alarm attachment; retry must preserve
	// the archived object rather than copying the stale external URL back.
	a, _ = repo.GetAlarm(ctx, externalTestTenant, a.ID)
	stale := v
	stale.SnapshotURL = "http://unreachable.invalid/stale.jpg"
	a.Details["videoEvent"] = stale
	repo.UpdateAlarmIf(ctx, a)
	response := req("POST", path+"retry", users["media-retry"], nil, 202)
	if response["queued"] != true {
		t.Fatal(response)
	}
	stored, err := repo.GetVideoEvent(ctx, externalTestTenant, v.EventID)
	if err != nil || stored.SnapshotURL != v.SnapshotURL || stored.Raw["mediaTransferStatus"] != "PENDING" {
		t.Fatalf("retry overwrote durable media: %+v %v", stored, err)
	}
	a, _ = repo.GetAlarm(ctx, externalTestTenant, a.ID)
	if a.Status != "RECOVERED" {
		t.Fatal("media retry altered alarm lifecycle")
	}
	// A video-event link must never become an arbitrary archive-object proxy.
	for _, forged := range []string{"local://private/secret", "local://video-alarm/other-tenant/image.png", "local://video-alarm/" + keyBase + "other-event/snapshot-ext-camera.png", "https://example.com/image.png"} {
		a, _ = repo.GetAlarm(ctx, externalTestTenant, a.ID)
		bad := v
		bad.SnapshotURL = forged
		a.Details["videoEvent"] = bad
		repo.UpdateAlarmIf(ctx, a)
		download(path+"snapshot", users["media-viewer"], "", 409)
	}
	a, _ = repo.GetAlarm(ctx, externalTestTenant, a.ID)
	a.Details["videoEvent"] = v
	repo.UpdateAlarmIf(ctx, a)
	api.engine.Archive.PutObject(ctx, "video-alarm", snapshotKey, strings.NewReader("<html>not a bitmap</html>"), 25, "image/png")
	download(path+"snapshot", users["media-viewer"], "", 415)
	data, _ := json.Marshal(response)
	if bytes.Contains(data, []byte("local://")) {
		t.Fatal("retry response exposed archive references")
	}
}
