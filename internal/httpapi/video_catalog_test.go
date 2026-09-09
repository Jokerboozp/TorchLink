package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGB28181AuthenticatedCatalogImport(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go compiler unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	if err := repo.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "t", ID: "video-node", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	secret := "catalog-integration-node-secret"
	if err := repo.SetEdgeCredential(ctx, "t", "video-node", onboarding.Hash(secret)); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "go", "test", ".", "-run", "^TestSIPRegistrationCatalogAndKeepalive$/udp$", "-count=1", "-v")
	command.Dir = filepath.Join("..", "..", "protocol-packages", "gb28181-metadata")
	command.Env = append(os.Environ(), "IOT_TEST_GB_PLATFORM_URL="+server.URL, "IOT_TEST_GB_NODE_SECRET="+secret)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual SIP module: %v %s", err, output)
	}
	h, err := repo.GetEdgeHeartbeat(ctx, "t", "video-node")
	if err != nil || len(h.VideoCatalog) != 1 || len(h.VideoCatalog[0].Channels) != 1 || !h.VideoCatalog[0].Registered {
		t.Fatal("actual authenticated catalog missing", h, err)
	}
	call := func(tenant, role, path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, bytes.NewReader(b))
		token, err := api.auth.Issue("tester", tenant, role, nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/edge-nodes/video-node/video-catalog/import"
	body := map[string]any{"deviceId": "34020000001320000001", "cameraId": "34020000001310000001"}
	if w := call("t", "viewer", path, body); w.Code != 403 {
		t.Fatal("viewer import", w.Code)
	}
	if w := call("other", "admin", path, body); w.Code != 404 {
		t.Fatal("foreign import", w.Code)
	}
	if w := call("t", "operator", path, body); w.Code != 201 {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	camera, err := repo.GetVideoCameraMapping(ctx, "t", "34020000001310000001")
	if err != nil || camera.CameraName != "Test camera" || camera.DeviceID != "" || camera.StreamURL != "" {
		t.Fatal("camera metadata", camera, err)
	}
	camera.CameraName = "Operator name"
	camera.VideoPlatformID = "client-forged-source"
	if w := call("t", "operator", "/api/v1/integrations/video/cameras", camera); w.Code != 201 {
		t.Fatalf("edit imported camera: %d %s", w.Code, w.Body.String())
	}
	if w := call("t", "operator", path, body); w.Code != 200 {
		t.Fatal("duplicate import", w.Code)
	}
	camera, _ = repo.GetVideoCameraMapping(ctx, "t", camera.CameraID)
	if camera.CameraName != "Operator name" {
		t.Fatal("import overwrote edit")
	}
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		token, err := api.auth.Issue("browser-test", "t", "admin", nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "gb28181-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("browser: %v %s", err, output)
		}
		t.Log(string(output))
	})

	h.LastSeenAt = time.Now().Add(-time.Minute).UnixMilli()
	if err := repo.SaveEdgeHeartbeat(ctx, "t", "video-node", h); err != nil {
		t.Fatal(err)
	}
	if w := call("t", "operator", path, body); w.Code != 409 {
		t.Fatal("stale heartbeat import", w.Code)
	}
}
