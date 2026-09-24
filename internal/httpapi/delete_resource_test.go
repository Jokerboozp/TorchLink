package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestDeleteResourceRoutesReturnConflictAndNotFound(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant_a", ID: "product"})
	_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: "device", ProductID: "product"})
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret = "root", "root-password-test", "delete-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.DevMode = true
	server := httptest.NewServer(New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	request := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	token := request("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	request("DELETE", "/api/v1/products/product", token, nil, 409)
	request("DELETE", "/api/v1/device-registry/device", token, nil, 200)
	request("DELETE", "/api/v1/products/product", token, nil, 200)
	request("DELETE", "/api/v1/products/product", token, nil, 404)
}

func TestDeleteProtocolReleaseOnlyRemovesSelectedVersionAndArtifacts(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	_ = repo.SaveProtocolDefinition(ctx, model.ProtocolDefinition{TenantID: "tenant_a", ID: "protocol", Name: "测试协议"})
	for _, version := range []string{"1.0.0", "2.0.0"} {
		if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant_a", ProtocolID: "protocol", Version: version}); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant_a", ProductID: "product", ProtocolID: "protocol", Version: "2.0.0", PreviousVersion: "1.0.0"})
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret = "root", "root-password-test", "delete-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.DevMode = true
	cfg.DataDir = t.TempDir()
	for _, version := range []string{"1.0.0", "2.0.0"} {
		path := filepath.Join(cfg.DataDir, "protocol-releases", "tenant_a", "protocol", version)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "package.zip"), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	token := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	path := server.URL + "/api/v2/protocols/protocol/releases/1.0.0"
	requestJSON(t, server.Client(), "DELETE", path, token, nil, 409)
	_ = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant_a", ProductID: "product", ProtocolID: "protocol", Version: "2.0.0"})
	requestJSON(t, server.Client(), "DELETE", path, token, nil, 200)
	requestJSON(t, server.Client(), "DELETE", path, token, nil, 404)
	if _, err := repo.GetProtocolDefinition(ctx, "tenant_a", "protocol"); err != nil {
		t.Fatalf("protocol family was deleted: %v", err)
	}
	if _, err := repo.GetProtocolRelease(ctx, "tenant_a", "protocol", "2.0.0"); err != nil {
		t.Fatalf("other version was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "protocol-releases", "tenant_a", "protocol", "1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("deleted version artifacts remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "protocol-releases", "tenant_a", "protocol", "2.0.0", "package.zip")); err != nil {
		t.Fatalf("other version artifacts were removed: %v", err)
	}
}

func TestDeleteKnowledgeDocumentClearsIndexObjectAndRecord(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	index := knowledge.NewLocal()
	if _, err = archive.PutObject(ctx, "iot-knowledge-docs", "tenant/doc.txt", bytes.NewBufferString("sample"), 6, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err = index.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: "tenant_a", DocumentID: "doc", ChunkID: "chunk", Content: []byte("sample")}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveKnowledgeDoc(ctx, model.KnowledgeDoc{TenantID: "tenant_a", ID: "doc", ObjectBucket: "iot-knowledge-docs", ObjectKey: "tenant/doc.txt"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret = "root", "root-password-test", "delete-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.DevMode = true
	server := httptest.NewServer(New(cfg, &core.Engine{Repo: repo, Archive: archive, KB: index}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	request := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	token := request("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	request("DELETE", "/api/v1/knowledge/documents/doc", token, nil, 200)
	if docs, err := repo.ListKnowledgeDocs(ctx, "tenant_a"); err != nil || len(docs) != 0 {
		t.Fatalf("metadata remains: %v %v", docs, err)
	}
	if chunks, err := index.ListKnowledgeChunks(ctx, "tenant_a", "doc"); err != nil || len(chunks) != 0 {
		t.Fatalf("index remains: %v %v", chunks, err)
	}
	if object, err := archive.GetObject(ctx, "iot-knowledge-docs", "tenant/doc.txt"); err == nil {
		object.Close()
		t.Fatal("object remains")
	}
}
