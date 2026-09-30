package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	minio "github.com/minio/minio-go/v7"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type governanceVideoArchive struct {
	ports.Archive
	get func(context.Context, string, string) (io.ReadCloser, error)
}

func (a *governanceVideoArchive) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if a.get != nil {
		return a.get(ctx, bucket, key)
	}
	return a.Archive.GetObject(ctx, bucket, key)
}

type videoErrorReader struct{ err error }

func (v videoErrorReader) Read([]byte) (int, error) { return 0, v.err }

func TestGovernanceVideoSourceAndPrivateMediaPermissions(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, id := range []string{"a", "b"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id}); err != nil {
			t.Fatal(err)
		}
	}
	permissions := []string{"menu:devices", "menu:alarms", "menu:alarmGovernance", "menu:cameras", "action:alarmGovernance:record", "action:cameras:history", "action:cameras:download"}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "reader", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"a"}, Permissions: permissions}}}
	if _, err := repo.SaveAccessState(ctx, "t", state); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "a", CameraName: "原摄像头", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	guard := &governanceVideoArchive{Archive: archive}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t", "other"}, JWTSecret: "video-history-fixture-more-than-32-characters", DevMode: true}, &core.Engine{Repo: repo, Archive: guard}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetGovernanceVideoReader(repo)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.IssueUser("reader", "t", 1, time.Hour)
	admin, _ := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	foreign, _ := api.auth.Issue("admin", "other", "admin", nil, time.Hour)
	now := time.Now().UnixMilli()
	event := model.VideoAlarmEvent{TenantID: "t", EventID: "video1", CameraID: "camera", CameraName: "原摄像头", AlarmType: "SMOKE", AlarmName: "烟雾", EventTime: now - 1000, ReceivedAt: now, Raw: map[string]any{"governanceDeviceId": "forged", "secret": "private-video-payload"}}
	key := fmt.Sprintf("t/%s/video1/snapshot-camera.jpg", time.UnixMilli(event.EventTime).UTC().Format("2006/01/02"))
	event.SnapshotURL, err = archive.PutObject(ctx, "video-alarm", key, bytes.NewReader([]byte("private image bytes")), 19, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveVideoEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/alarm-governance/video-events?deviceIds=a&start=" + strconv.FormatInt(now-2000, 10) + "&end=" + strconv.FormatInt(now+1, 10) + "&limit=1"
	request := func(identity, method, p string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+p, identity, body, status)
	}
	page := request(token, "GET", path, nil, 200)
	items := page["items"].([]any)
	if len(items) != 1 {
		t.Fatal(page)
	}
	item := items[0].(map[string]any)
	version := item["targetVersion"].(float64)
	if version <= 0 || version > (1<<53)-1 || item["bindingQuality"] != "CAPTURED_BINDING" || strings.Contains(fmt.Sprint(page), "local://") || strings.Contains(fmt.Sprint(page), "private-video-payload") {
		t.Fatal("uncontrolled video source metadata", page)
	}
	request(foreign, "GET", path, nil, 200)
	obs, _, err := repo.SaveAlarmObservation(ctx, model.AlarmObservation{TenantID: "t", DeviceID: "a", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE", SourceSystem: "protocol", SourceEventID: "video-case", FactKind: "REPORT", EventAt: now - 1000})
	if err != nil {
		t.Fatal(err)
	}
	caseDoc := request(admin, "POST", "/api/v1/alarm-governance/cases", map[string]any{"title": "视频辅助核实", "deviceId": "a", "alarmType": "FIRE", "originKind": "DEVICE_DIRECT", "signalKey": "device:FIRE", "ownerUserId": "reader", "observationIds": []string{obs.ID}, "idempotencyKey": "case"}, 201)
	casePath := "/api/v1/alarm-governance/cases/" + caseDoc["id"].(string)
	link := request(token, "POST", casePath+"/business-links", map[string]any{"targetKind": "VIDEO_EVENT", "targetId": event.EventID, "targetVersion": version, "relation": "历史视频辅助资料", "idempotencyKey": "video"}, 201)
	mediaPath := "/api/v1/alarm-governance/business-links/" + link["id"].(string) + "/media?kind=snapshot"
	download := func(identity string, status int) []byte {
		req, _ := http.NewRequest("GET", server.URL+mediaPath, nil)
		req.Header.Set("Authorization", "Bearer "+identity)
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, e := io.ReadAll(resp.Body)
		if e != nil || resp.StatusCode != status {
			t.Fatalf("media status %d want %d: %s %v", resp.StatusCode, status, data, e)
		}
		return data
	}
	if string(download(token, 200)) != "private image bytes" {
		t.Fatal("private archive object missing")
	}
	download(foreign, 404)
	change := func() {
		latest, e := repo.LoadAccessState(ctx, "t")
		if e != nil {
			t.Fatal(e)
		}
		state.Revision = latest.Revision
		if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
			t.Fatal(e)
		}
	}
	state.Users[0].Permissions = slicesWithout(permissions, "action:cameras:download")
	change()
	request(token, "GET", path, nil, 200)
	download(token, 403)
	state.Users[0].Permissions = slicesWithout(permissions, "action:cameras:history")
	change()
	request(token, "GET", path, nil, 403)
	request(token, "GET", casePath, nil, 403)
	state.Users[0].Permissions = permissions
	change()
	// Missing objects and temporary storage failures have different semantics,
	// including errors produced by MinIO only on the first reader operation.
	guard.get = func(context.Context, string, string) (io.ReadCloser, error) {
		return nil, errors.New("temporary network failure")
	}
	download(token, 503)
	guard.get = func(context.Context, string, string) (io.ReadCloser, error) {
		return io.NopCloser(videoErrorReader{err: minio.ErrorResponse{Code: "NoSuchKey"}}), nil
	}
	download(token, 410)
	guard.get = func(context.Context, string, string) (io.ReadCloser, error) {
		return io.NopCloser(videoErrorReader{err: context.DeadlineExceeded}), nil
	}
	download(token, 503)
	request(token, "GET", path, nil, 200)
	guard.get = nil
	for _, reference := range []string{"http://127.0.0.1/private", "local://video-alarm/t/../../secret", "local://video-alarm/other/private.jpg", "minio://other-bucket/" + key} {
		event.SnapshotURL = reference
		if err = repo.UpdateVideoEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
		guard.get = func(context.Context, string, string) (io.ReadCloser, error) {
			t.Error("invalid reference reached archive")
			return nil, errors.New("rejected")
		}
		download(token, 410)
	}
	event.SnapshotURL = "local://video-alarm/" + key
	if err = repo.UpdateVideoEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetGovernanceVideoEvent(ctx, "t", event.EventID)
	if err != nil || float64(videoSourceVersion(current)) != version {
		t.Fatal("async media changed event version", current, err)
	}
	guard.get = func(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
		state.Users[0].Permissions = slicesWithout(permissions, "action:cameras:download")
		change()
		return archive.GetObject(ctx, bucket, key)
	}
	download(token, 403)
	state.Users[0].Permissions = permissions
	change()
	guard.get = nil
	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "b"}); err != nil {
		t.Fatal(err)
	}
	download(token, 403)
	request(token, "GET", casePath, nil, 403)
	state.Users[0].DeviceIDs = []string{"b"}
	change()
	request(token, "GET", casePath, nil, 403)
	state.Users[0].DeviceIDs = []string{"a", "b"}
	change()
	if string(download(token, 200)) != "private image bytes" {
		t.Fatal("complete current and historical membership denied")
	}
	if err = archive.DeleteObject(ctx, "video-alarm", key); err != nil {
		t.Fatal(err)
	}
	download(token, 410)
	request(token, "GET", casePath, nil, 200)
}

func slicesWithout(values []string, remove string) []string {
	out := []string{}
	for _, value := range values {
		if value != remove {
			out = append(out, value)
		}
	}
	return out
}

func TestVideoMediaObjectRejectsEncodedTraversalAndConfusedIdentity(t *testing.T) {
	v := model.VideoAlarmEvent{TenantID: "t", EventID: "event", CameraID: "camera", EventTime: 1000}
	for _, raw := range []string{"local://video-alarm/t/1970/01/01/event/snapshot-camera.jpg", "minio://video-alarm/t/1970/01/01/event/snapshot-camera.mp4"} {
		v.SnapshotURL = raw
		if _, _, err := videoMediaObject(v, "snapshot"); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"local://video-alarm/t/1970/01/01/event/%2e%2e/snapshot-camera.jpg", "local://video-alarm/t/1970/01/01/event/snapshot-camera.jpg?x=1", "local://video-alarm/t/1970/01/01/event/snapshot-camera-extra.jpg", "local://video-alarm/t/1970/01/01/event/clip-camera.jpg", "local://video-alarm/t/1970/01/01/event/snapshot-camera.%2Fsecret", "local://user@video-alarm/t/1970/01/01/event/snapshot-camera.jpg"} {
		v.SnapshotURL = raw
		if _, _, err := videoMediaObject(v, "snapshot"); err == nil {
			t.Fatal("unsafe reference accepted", raw)
		}
	}
}
