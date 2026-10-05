package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type embeddingConfigTestRuntime struct {
	config  ports.EmbeddingConfig
	updates int
}

func (e *embeddingConfigTestRuntime) CurrentConfig() ports.EmbeddingConfig { return e.config }
func (e *embeddingConfigTestRuntime) Configure(_ context.Context, cfg ports.EmbeddingConfig) error {
	e.config = cfg
	e.updates++
	return nil
}

func TestEmbeddingConfigurationPermissionsSecretsAndIndependentTest(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), log)
	engine.KB = knowledge.NewLocal()
	api := New(config.Config{DevMode: true, RerankURL: "http://reranker:8080", LocalAIHosts: ports.DefaultLocalAIHosts}, engine, metrics.New(), log)
	runtime := &embeddingConfigTestRuntime{config: ports.EmbeddingConfig{BaseURL: "https://embedding.example/v1", Model: "text-embedding-v4", APIKey: "private-embedding-credential", Dimensions: 1024, BatchSize: 10, TimeoutSeconds: 60}}
	api.SetEmbeddingRuntime(runtime)
	server := newTestHTTPServer(api)
	defer server.Close()
	admin, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/embedding-config", viewer, nil, 403)
	view := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/embedding-config", admin, nil, 200)
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), runtime.config.APIKey) || view["apiKeyConfigured"] != true {
		t.Fatalf("credential view invalid: %s", raw)
	}
	if rerank, _ := view["rerank"].(map[string]any); rerank["enabled"] != true || rerank["local"] != true || rerank["model"] != bundledRerankModel {
		t.Fatalf("bundled reranker not shown: %s", raw)
	}
	candidate := map[string]any{"baseUrl": "https://other-embedding.example/v1", "model": "other-model", "dimensions": 768, "batchSize": 8, "timeoutSeconds": 30, "apiKey": ""}
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/embedding-config", viewer, candidate, 403)
	if runtime.updates != 0 {
		t.Fatal("unauthorized configuration changed runtime")
	}
	saved := requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/embedding-config", admin, candidate, 200)
	if runtime.config.APIKey != "private-embedding-credential" || saved["dimensions"] != float64(768) {
		t.Fatal("blank key did not preserve server credential")
	}
	candidate["clearAPIKey"] = true
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/embedding-config", admin, candidate, 200)
	if runtime.config.APIKey != "" {
		t.Fatal("explicit credential clear ignored")
	}
	before := runtime.updates
	tested := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/embedding-test", admin, candidate, 200)
	if tested["success"] != false || runtime.updates != before {
		t.Fatal("connection diagnostic changed configuration")
	}
	candidate["dimensions"] = 4096
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/embedding-config", admin, candidate, 422)
	candidate["dimensions"] = 768
	// Plain HTTP is only for the bundled service (IOT_LOCAL_AI_HOSTS).
	candidate["baseUrl"] = "http://intranet-vectors:80/v1"
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/embedding-config", admin, candidate, 422)
	if runtime.updates != before {
		t.Fatal("invalid vector configuration became active")
	}
	candidate["baseUrl"] = "http://embedding:8080/v1"
	if saved := requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/embedding-config", admin, candidate, 200); saved["local"] != true {
		t.Fatalf("bundled vector service without a key rejected: %v", saved)
	}
	if routeMenu("/api/v1/ai/embedding-config") != "aiProviders" || routeMenu("/api/v1/ai/embedding-test") != "aiProviders" {
		t.Fatal("embedding settings escaped model-management permission scope")
	}
}
