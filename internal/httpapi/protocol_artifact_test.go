package httpapi

import (
	"context"
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

func TestLegacyGoProtocolUploadIsUnavailable(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = t.TempDir()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ExternalParser{Root: cfg.DataDir}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages", token, map[string]any{
		"id": "protocol_go", "name": "Go Worker", "parserType": parser.GoProtocolParserName,
	}, http.StatusUnprocessableEntity)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages/protocol_go/artifact", token, map[string]any{}, http.StatusNotFound)
}
