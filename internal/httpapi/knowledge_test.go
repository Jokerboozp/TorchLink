package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/parser"
)

func TestKnowledgeUploadAndTenantScopedList(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.KB = knowledge.NewLocal()
	cfg := config.Load()
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	operatorToken, err := api.auth.Issue("operator", "tenant-a", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	otherTenantToken, err := api.auth.Issue("viewer", "tenant-b", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err = form.WriteField("workflowId", "ops-assistant"); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("productId", "fire-smoke"); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("category", "alarm-sop"); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("tags", "smoke,certified"); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("file", "fire-sop.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(file, strings.NewReader("高温烟雾告警处置：先核对设备状态，再通知现场人员复核。")); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/knowledge/documents", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var uploaded map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated || uploaded["status"] != "INDEXED" || uploaded["filename"] != "fire-sop.txt" {
		t.Fatalf("unexpected upload response status=%d body=%#v", resp.StatusCode, uploaded)
	}

	listed := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents", viewerToken, nil, http.StatusOK)
	if listed["count"] != float64(1) || listed["persistentIndex"] != false || listed["indexMode"] != "local-memory" {
		t.Fatalf("unexpected knowledge list %#v", listed)
	}
	items := listed["items"].([]any)
	item := items[0].(map[string]any)
	if item["tenantId"] != "tenant-a" || item["workflowId"] != "ops-assistant" || item["productId"] != "fire-smoke" || item["category"] != "alarm-sop" || item["filename"] != "fire-sop.txt" {
		t.Fatalf("unexpected knowledge item %#v", item)
	}
	tags := item["tags"].([]any)
	if len(tags) != 2 || tags[0] != "smoke" || tags[1] != "certified" {
		t.Fatalf("unexpected knowledge tags %#v", tags)
	}
	detail := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents/"+item["id"].(string), viewerToken, nil, http.StatusOK)
	indexDetails := detail["index"].(map[string]any)
	if indexDetails["mode"] != "local-memory" || indexDetails["chunkCount"] != float64(1) {
		t.Fatalf("unexpected knowledge index details %#v", detail)
	}
	chunking := indexDetails["chunking"].(map[string]any)
	if chunking["size"] != float64(1200) || chunking["overlap"] != float64(200) {
		t.Fatalf("unexpected chunking policy %#v", chunking)
	}
	detailChunks := detail["chunks"].([]any)
	if len(detailChunks) != 1 || detailChunks[0].(map[string]any)["content"] != "高温烟雾告警处置：先核对设备状态，再通知现场人员复核。" || detailChunks[0].(map[string]any)["vectorized"] != false {
		t.Fatalf("unexpected knowledge chunks %#v", detailChunks)
	}

	isolated := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents", otherTenantToken, nil, http.StatusOK)
	if isolated["count"] != float64(0) {
		t.Fatalf("knowledge documents leaked across tenants: %#v", isolated)
	}
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents/"+item["id"].(string), otherTenantToken, nil, http.StatusNotFound)
}
