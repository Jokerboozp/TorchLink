package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestVideoWebhookEnforcesPlatformTenantBinding(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	cfg := config.Load()
	cfg.DevMode = false
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	cfg.VideoSecrets = map[string]string{"video-a": "secret-a"}
	cfg.VideoPlatformTenants = map[string]string{"video-a": "tenant-a"}
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	if err := repo.SaveVideoCameraMapping(context.Background(), model.VideoCameraMapping{TenantID: "tenant-a", CameraID: "camera-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	valid := []byte(`{"eventId":"video-binding-valid","tenantId":"tenant-a","cameraId":"camera-a","alarmType":"FIRE"}`)
	status, _ := postSignedVideo(t, server.URL, valid, "video-a", "secret-a")
	if status != http.StatusCreated {
		t.Fatalf("valid bound webhook status=%d", status)
	}
	unbound := []byte(`{"eventId":"video-binding-unbound","tenantId":"tenant-a","cameraId":"camera-b","alarmType":"FIRE"}`)
	status, _ = postSignedVideo(t, server.URL, unbound, "video-a", "secret-a")
	if status != http.StatusForbidden {
		t.Fatalf("unbound camera webhook status=%d, want 403", status)
	}

	spoofed := []byte(`{"eventId":"video-binding-spoofed","tenantId":"tenant-b","cameraId":"camera-b","alarmType":"FIRE"}`)
	status, _ = postSignedVideo(t, server.URL, spoofed, "video-a", "secret-a")
	if status != http.StatusForbidden {
		t.Fatalf("cross-tenant webhook status=%d, want 403", status)
	}
}

func postSignedVideo(t *testing.T, baseURL string, body []byte, platform, secret string) (int, []byte) {
	t.Helper()
	ts := time.Now().Unix()
	timestamp := fmtInt64(ts)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write(body)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/integrations/video/alarm", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Video-Platform-ID", platform)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func fmtInt64(v int64) string { return strconv.FormatInt(v, 10) }
