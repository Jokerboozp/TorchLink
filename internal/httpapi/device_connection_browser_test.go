package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestDeviceConnectionBrowser(t *testing.T) {
	if os.Getenv("IOT_TEST_BROWSER") == "" {
		t.Skip("set IOT_TEST_BROWSER after frontend build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "device-detail-isolated-test-key-32-characters"
	api := New(cfg, engine, metrics.New(), log)
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "long-product-1788991005167", Name: "本地联调产品 1788991005167", Status: "ENABLED", Transport: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "local-check-1788991005167", ProductID: "long-product-1788991005167", Name: "本地联调传感器", DeviceRole: "DIRECT", Status: "ENABLED", AccessKey: "fixture-key", SecretHash: "fixture-hash", Tags: map[string]string{"connector": "HTTP"}}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveStandardMessage(ctx, model.StandardMessage{MessageID: "msg_detail", RawMessageID: "raw_detail", TenantID: "tenant", ProductID: "long-product-1788991005167", DeviceID: "local-check-1788991005167", MessageType: model.PropertyReport, Timestamp: time.Now().UnixMilli(), Properties: map[string]any{"temperature": 42, "location": strings.Repeat("long-device-location/", 12)}}); err != nil {
		t.Fatal(err)
	}
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute)
	viewer, _ := api.auth.Issue("reader", "tenant", "viewer", nil, time.Minute)
	cmd := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "device-connection-check.mjs"))
	cmd.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_VIEWER_TOKEN="+viewer)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
