package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
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

func TestOnboardingBrowser(t *testing.T) {
	if os.Getenv("IOT_TEST_BROWSER") == "" {
		t.Skip("set IOT_TEST_BROWSER to Chromium executable after frontend build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "browser-isolated-test-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	api := New(cfg, engine, metrics.New(), log)
	api.SetProtocolListeners(browserConnectionSnapshot{})
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "legacy-product", Name: "历史产品", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "legacy", AccessKey: "legacy-key", ProductID: "legacy-product", Name: "历史设备", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"listener-a", "listener-b"} {
		if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: id, ProductID: "legacy-product", Mode: "listener", Network: "tcp", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Hour)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "onboarding-check.mjs"))
	command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output)
	}
	t.Log(string(output))
	devices, err := repo.ListManagedDevices(ctx, "tenant")
	if err != nil || len(devices) != 2 || devices[0].Status != "ENABLED" {
		t.Fatalf("browser did not persist enabled device: %+v %v", devices, err)
	}
	nodes, err := repo.ListEdgeNodes(ctx, "tenant")
	if err != nil || len(nodes) != 1 || nodes[0].ID != "browser-edge" {
		t.Fatalf("browser did not persist edge registration: %+v %v", nodes, err)
	}
}

// The browser uses a runtime snapshot; no physical listener or device is required.
type browserConnectionSnapshot struct{ connectionSnapshot }

func (browserConnectionSnapshot) Sessions(tenant, id string) []map[string]any {
	if tenant == "tenant" {
		return (connectionSnapshot{}).Sessions("t", id)
	}
	return nil
}
