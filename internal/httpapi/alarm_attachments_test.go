package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestAlarmAttachments(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "attachment-root-password"
	cfg.AdminTenants = []string{"t"}
	cfg.JWTSecret = "attachment-test-secret-at-least-32-bytes"
	cfg.DevMode = true
	dir := t.TempDir()
	archive, err := local.NewArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(repo), Archive: archive, Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	for _, a := range []model.Alarm{{ID: "fire", DeviceID: "d1"}, {ID: "other", DeviceID: "d2"}} {
		a.TenantID, a.RuleID, a.Status, a.AlarmType, a.AlarmLevel, a.FirstTriggeredAt, a.LastTriggeredAt, a.TriggerCount = "t", a.ID, "ACTIVE", "DEVICE_FAULT", "LOW", 1, 1, 1
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: a.DeviceID, AccessKey: a.DeviceID, ProductID: "p"})
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)["accessToken"].(string)
	upload := func(alarm, token, name string, data []byte, status int) map[string]any {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", name)
		_, _ = part.Write(data)
		_ = form.Close()
		r, _ := http.NewRequest("POST", srv.URL+"/api/v1/alarms/"+alarm+"/attachments", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != status {
			t.Fatalf("upload %s: %d %v, want %d", name, resp.StatusCode, out, status)
		}
		return out
	}
	var photo bytes.Buffer
	_ = png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	upload("fire", root, "page.html", []byte("<html><script>alert(1)</script>"), 422)
	saved := upload("fire", root, "../现场照片.html", photo.Bytes(), 200)
	upload("fire", root, "处置记录.pdf", []byte("%PDF-1.4\n%%EOF\n"), 200)
	attachments := saved["attachments"].([]any)
	att := attachments[0].(map[string]any)
	if att["name"] != "现场照片.png" || att["contentType"] != "image/png" || att["uploadedBy"] != "root" {
		t.Fatalf("attachment %v", att)
	}
	path := "/api/v1/alarms/fire/attachments/" + att["id"].(string)

	get := func(path, token string) (*http.Response, []byte) {
		t.Helper()
		r, _ := http.NewRequest("GET", srv.URL+path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, body
	}
	resp, body := get(path+"?inline=1", root)
	if resp.StatusCode != 200 || !bytes.Equal(body, photo.Bytes()) || resp.Header.Get("Content-Type") != "image/png" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "inline") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download %d %v", resp.StatusCode, resp.Header)
	}

	// A user limited to another device cannot read or change these files.
	user := map[string]any{"username": "other", "password": "other-password-1", "enabled": true, "permissions": []string{"menu:devices", "menu:alarms", "POST " + alarmAttachmentPath, "DELETE " + alarmAttachmentPath + "/:attachmentId"}, "deviceScope": "selected", "deviceIds": []string{"d2"}}
	req("POST", "/api/v1/access/users", root, user, 200)
	other := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "other", "password": "other-password-1", "tenantId": "t"}, 200)["accessToken"].(string)
	if resp, _ := get(path, other); resp.StatusCode != 404 {
		t.Fatalf("out-of-scope download %d", resp.StatusCode)
	}
	upload("fire", other, "x.png", photo.Bytes(), 403)
	req("DELETE", path, other, nil, 403)
	upload("other", other, "x.png", photo.Bytes(), 200)

	// Deleting removes the record and the file.
	req("DELETE", path, root, nil, 200)
	if resp, _ := get(path, root); resp.StatusCode != 404 {
		t.Fatalf("deleted attachment still served: %d", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(dir, model.AlarmAttachmentBucket, filepath.FromSlash(model.AlarmAttachmentKey("t", "fire", att["id"].(string))))); !os.IsNotExist(err) {
		t.Fatalf("deleted attachment file kept: %v", err)
	}
	// The count is limited, and closed alarms are no longer changed.
	for range model.MaxAlarmAttachments - 1 {
		upload("fire", root, "p.png", photo.Bytes(), 200)
	}
	upload("fire", root, "p.png", photo.Bytes(), 422)
	req("POST", "/api/v1/alarms/fire/actions", root, map[string]any{"action": "closed"}, 200)
	upload("fire", root, "p.png", photo.Bytes(), 422)
}
