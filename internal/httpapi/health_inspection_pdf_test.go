package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/parser"
)

func TestHealthInspectionPDFDownload(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	report := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection", login["accessToken"].(string), map[string]any{}, http.StatusOK)
	generatedAt, ok := report["generatedAt"].(float64)
	if !ok || generatedAt <= 0 {
		t.Fatalf("health inspection generatedAt = %#v", report["generatedAt"])
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/health-inspection/pdf", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+login["accessToken"].(string))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/pdf" || !bytes.Contains([]byte(response.Header.Get("Content-Disposition")), []byte(fmt.Sprintf("health-inspection-%d.pdf", int64(generatedAt)))) || !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("PDF response status=%d type=%q disposition=%q prefix=%q", response.StatusCode, response.Header.Get("Content-Type"), response.Header.Get("Content-Disposition"), data[:min(len(data), 8)])
	}
}
