package httpapi

import (
	"bytes"
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
	"iot-platform/internal/parser"
)

func TestDeviceOnboardingBrowser(t *testing.T) {
	if os.Getenv("IOT_TEST_BROWSER") == "" {
		t.Skip("set IOT_TEST_BROWSER to Chrome or Edge after frontend build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	// Parsing runs so the wizard can confirm a real device report.
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "browser-device-onboarding-test-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"
	cfg.MQTTPublicURL = "mqtts://devices.example.test:8883"
	api := New(cfg, engine, metrics.New(), log)
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
	command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "device-onboarding-check.mjs"))
	command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		t.Fatalf("browser: %v\n%s", err, output.String())
	}
	devices, err := repo.ListManagedDevices(ctx, "tenant")
	if err != nil || len(devices) != 2 {
		t.Fatalf("browser device count: %d %v", len(devices), err)
	}
	for _, device := range devices {
		if device.SecretHash == "" {
			t.Fatal("standard device has no credential")
		}
	}
	products, err := repo.ListProducts(ctx, "tenant")
	if err != nil || len(products) != 1 {
		t.Fatalf("browser template count: %d %v", len(products), err)
	}
}
