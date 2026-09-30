package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"iot-platform/internal/adapters/embedding"
	"iot-platform/internal/config"
	"iot-platform/internal/ports"
)

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func TestLocalDeleteKnowledgeDocumentKeepsOtherDocumentsAndTenants(t *testing.T) {
	index := NewLocal()
	ctx := context.Background()
	for _, item := range []ports.KnowledgeIndexInput{
		{TenantID: "one", DocumentID: "remove", ChunkID: "a", Content: []byte("one")},
		{TenantID: "one", DocumentID: "keep", ChunkID: "b", Content: []byte("two")},
		{TenantID: "two", DocumentID: "remove", ChunkID: "c", Content: []byte("three")},
	} {
		if err := index.IndexKnowledge(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := index.DeleteKnowledgeDocument(ctx, "one", "remove", ""); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		tenant, document string
		want             int
	}{{"one", "remove", 0}, {"one", "keep", 1}, {"two", "remove", 1}} {
		chunks, err := index.ListKnowledgeChunks(ctx, check.tenant, check.document)
		if err != nil || len(chunks) != check.want {
			t.Fatalf("%s/%s: %d chunks, %v", check.tenant, check.document, len(chunks), err)
		}
	}
}

func TestLocalKnowledgeAppliesWorkflowMetadataFilters(t *testing.T) {
	index := NewLocal()
	for _, input := range []ports.KnowledgeIndexInput{
		{TenantID: "tenant-a", WorkflowID: "ops-assistant", ProductID: "smoke", Category: "alarm-sop", Tags: []string{"certified", "fire"}, DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("烟雾 告警 现场 复核 处置")},
		{TenantID: "tenant-a", WorkflowID: "camera-assistant", ProductID: "camera", Category: "manual", Tags: []string{"video"}, DocumentID: "doc-2", ChunkID: "chunk-2", Content: []byte("烟雾 告警 摄像头 联动")},
		{TenantID: "tenant-b", WorkflowID: "ops-assistant", ProductID: "smoke", Category: "alarm-sop", Tags: []string{"certified", "fire"}, DocumentID: "doc-3", ChunkID: "chunk-3", Content: []byte("烟雾 告警 其他租户")},
	} {
		if err := index.IndexKnowledge(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := index.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "tenant-a", WorkflowID: "ops-assistant", Question: "烟雾 告警", ProductIDs: []string{"smoke"}, Categories: []string{"alarm-sop"}, Tags: []string{"certified"}, Limit: 5, MinScore: .5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "doc-1" || hits[0].ProductID != "smoke" || hits[0].Score < .5 {
		t.Fatalf("unexpected filtered hits: %#v", hits)
	}
	otherAgent, err := index.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "tenant-a", WorkflowID: "camera-assistant", Question: "烟雾 告警", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherAgent) != 1 || otherAgent[0].DocumentID != "doc-2" {
		t.Fatalf("workflow association leaked across Agents: %#v", otherAgent)
	}
}

func TestLocalKnowledgeSupportsSingleCharacterChineseQueries(t *testing.T) {
	index := NewLocal()
	if err := index.IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{TenantID: "tenant-a", DocumentID: "doc-water", ChunkID: "chunk-water", Content: []byte("水压异常处置")}); err != nil {
		t.Fatal(err)
	}
	hits, err := index.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "tenant-a", Question: "水", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "doc-water" {
		t.Fatalf("single-character query returned %#v", hits)
	}
}

func TestLocalKnowledgeListsStoredChunkDetails(t *testing.T) {
	index := NewLocal()
	for _, input := range []ports.KnowledgeIndexInput{
		{TenantID: "tenant-a", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "doc-1-chunk-0001", ChunkIndex: 1, StartChar: 0, EndChar: 6, CharacterCount: 6, Content: []byte("甲乙丙丁戊己")},
		{TenantID: "tenant-a", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "doc-1-chunk-0002", ChunkIndex: 2, StartChar: 4, EndChar: 10, CharacterCount: 6, OverlapChars: 2, Content: []byte("戊己庚辛壬癸")},
	} {
		if err := index.IndexKnowledge(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	chunks, err := index.ListKnowledgeChunks(context.Background(), "tenant-a", "doc-1")
	if err != nil || len(chunks) != 2 {
		t.Fatalf("unexpected chunks=%#v err=%v", chunks, err)
	}
	if chunks[1].Index != 2 || chunks[1].StartChar != 4 || chunks[1].OverlapChars != 2 || chunks[1].Vectorized {
		t.Fatalf("unexpected chunk details %#v", chunks)
	}
}

type fakeEmbedder struct {
	model   string
	queries []string
}

func (f *fakeEmbedder) Model() string                { return f.model }
func (f *fakeEmbedder) Health(context.Context) error { return nil }
func (f *fakeEmbedder) Embed(_ context.Context, inputs []string, purpose ports.EmbedPurpose) ([][]float32, error) {
	out := make([][]float32, len(inputs))
	for i, input := range inputs {
		if purpose == ports.EmbedQuery {
			f.queries = append(f.queries, input)
		}
		out[i] = []float32{float32(len([]rune(input))), 0.5}
	}
	return out, nil
}

func v2Schema(model string) []byte {
	body, _ := json.Marshal(map[string]any{"class": "IotKnowledgeV2", "description": "embedding=" + model, "properties": knowledgeProperties()})
	return body
}

func TestWeaviateIndexSendsVectorsWithDeterministicUUID(t *testing.T) {
	var schema map[string]any
	var batch struct {
		Objects []map[string]any `json:"objects"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledgeV2":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/schema":
			if err := json.NewDecoder(r.Body).Decode(&schema); err != nil {
				t.Fatalf("decode schema: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/batch/objects":
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Fatalf("decode batch: %v", err)
			}
			_, _ = io.WriteString(w, `[{"result":{}},{"result":{}}]`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	index := NewWeaviate(server.URL, &fakeEmbedder{model: "Qwen/Qwen3-Embedding-0.6B"})
	err := index.IndexKnowledgeBatch(context.Background(), []ports.KnowledgeIndexInput{
		{TenantID: "tenant-1", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("设备告警处置")},
		{TenantID: "tenant-1", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "chunk-2", ChunkIndex: 1, Content: []byte("复位")},
	})
	if err != nil {
		t.Fatalf("index knowledge: %v", err)
	}
	if schema["vectorizer"] != "none" || schema["description"] != "embedding=Qwen/Qwen3-Embedding-0.6B" || schema["moduleConfig"] != nil {
		t.Fatalf("class must store external vectors for the configured model: %#v", schema)
	}
	properties, ok := schema["properties"].([]any)
	if !ok {
		t.Fatalf("missing schema properties in %#v", schema)
	}
	for _, raw := range properties {
		property, _ := raw.(map[string]any)
		if property["name"] != "tags" {
			continue
		}
		dataType, _ := property["dataType"].([]any)
		if len(dataType) != 1 || dataType[0] != "text[]" {
			t.Fatalf("tags property is not a text array: %#v", property)
		}
	}
	if len(batch.Objects) != 2 {
		t.Fatalf("expected one batch with both chunks, got %#v", batch.Objects)
	}
	object := batch.Objects[0]
	objectID, _ := object["id"].(string)
	if _, err := uuid.Parse(objectID); err != nil || objectID != knowledgeObjectID("tenant-1", "ops-assistant", "chunk-1") {
		t.Fatalf("object id is not the deterministic UUID: %q", objectID)
	}
	if object["class"] != "IotKnowledgeV2" {
		t.Fatalf("unexpected object class %#v", object["class"])
	}
	vector, _ := object["vector"].([]any)
	if len(vector) != 2 || vector[0] != float64(len([]rune("设备告警处置"))) {
		t.Fatalf("chunk vector missing: %#v", object["vector"])
	}
	objectProperties, ok := object["properties"].(map[string]any)
	if !ok || objectProperties["chunkId"] != "chunk-1" || objectProperties["chunkIndex"] != float64(0) || objectProperties["characterCount"] != float64(len([]rune("设备告警处置"))) {
		t.Fatalf("chunk metadata was not persisted in object %#v", objectProperties)
	}
}

func TestWeaviateIndexReportsBatchObjectErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledgeV2":
			_, _ = w.Write(v2Schema("m"))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/batch/objects":
			_, _ = io.WriteString(w, `[{"result":{"errors":{"error":[{"message":"vector lengths don't match"}]}}}]`)
		default:
			http.Error(w, strings.TrimSpace("unexpected request"), http.StatusNotFound)
		}
	}))
	defer server.Close()

	err := NewWeaviate(server.URL, &fakeEmbedder{model: "m"}).IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{
		TenantID: "tenant-1", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("失败"),
	})
	if err == nil || !strings.Contains(err.Error(), "vector lengths") {
		t.Fatalf("expected per-object batch error, got %v", err)
	}
}

func TestWeaviateSearchUsesQueryVectorAndScopeFilters(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledgeV2":
			_, _ = w.Write(v2Schema("m"))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/graphql":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			query = body["query"]
			_, _ = io.WriteString(w, `{"data":{"Get":{"IotKnowledgeV2":[{"documentId":"doc-1","chunkId":"c1","content":"复位步骤","tags":["a"],"_additional":{"certainty":0.8}},{"documentId":"doc-2","chunkId":"c2","content":"低分","_additional":{"certainty":0.1}}]}}}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	embedder := &fakeEmbedder{model: "m"}
	hits, err := NewWeaviate(server.URL, embedder).SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "tenant-a", WorkflowID: "wf", Question: "如何复位", Limit: 5, MinScore: 0.25})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DocumentID != "doc-1" || hits[0].Score != 0.8 {
		t.Fatalf("unexpected hits %#v", hits)
	}
	if len(embedder.queries) != 1 || embedder.queries[0] != "如何复位" {
		t.Fatalf("question must be embedded as a query: %#v", embedder.queries)
	}
	for _, fragment := range []string{"IotKnowledgeV2(", "nearVector:{vector:[4,0.5]}", `valueText:"tenant-a"`, `valueText:"wf"`} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query %q missing %q", query, fragment)
		}
	}
	if strings.Contains(query, "nearText") {
		t.Fatal("search must not depend on a Weaviate vectorizer module")
	}
}

func TestWeaviateDetectsIndexBuiltWithAnotherModel(t *testing.T) {
	legacy := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledgeV2":
			_, _ = w.Write(v2Schema("old-model"))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledge":
			if !legacy {
				http.NotFound(w, r)
				return
			}
			_, _ = io.WriteString(w, `{"class":"IotKnowledge"}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	index := NewWeaviate(server.URL, &fakeEmbedder{model: "new-model"})
	if _, err := index.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: "t", Question: "q"}); !errors.Is(err, ErrKnowledgeIndexStale) {
		t.Fatalf("search against another model's vectors must fail clearly, got %v", err)
	}
	if stale, err := index.NeedsRebuild(context.Background()); err != nil || !stale {
		t.Fatalf("model change must require a rebuild: %v %v", stale, err)
	}
	current := NewWeaviate(server.URL, &fakeEmbedder{model: "old-model"})
	if stale, err := current.NeedsRebuild(context.Background()); err != nil || stale {
		t.Fatalf("matching index must not be rebuilt: %v %v", stale, err)
	}
	legacy = true
	if stale, err := current.NeedsRebuild(context.Background()); err != nil || !stale {
		t.Fatalf("legacy Ollama-vectorized class must trigger a rebuild: %v %v", stale, err)
	}
}

func TestWeaviateListsStoredChunksInOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledgeV2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(v2Schema("m"))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/graphql":
			_, _ = io.WriteString(w, `{"data":{"Get":{"IotKnowledgeV2":[{"documentId":"doc-1","chunkId":"doc-1-chunk-0002","chunkIndex":2,"startChar":4,"endChar":10,"characterCount":6,"overlapChars":2,"content":"戊己庚辛壬癸"},{"documentId":"doc-1","chunkId":"doc-1-chunk-0001","chunkIndex":1,"startChar":0,"endChar":6,"characterCount":6,"overlapChars":0,"content":"甲乙丙丁戊己"}]}}}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	chunks, err := NewWeaviate(server.URL, &fakeEmbedder{model: "m"}).ListKnowledgeChunks(context.Background(), "tenant-a", "doc-1")
	if err != nil || len(chunks) != 2 {
		t.Fatalf("unexpected chunks=%#v err=%v", chunks, err)
	}
	if chunks[0].Index != 1 || chunks[1].Index != 2 || !chunks[0].Vectorized || chunks[1].OverlapChars != 2 || chunks[1].Content != "戊己庚辛壬癸" {
		t.Fatalf("unexpected ordered chunks %#v", chunks)
	}
}

func TestWeaviateLiveIndexAndScopeIsolation(t *testing.T) {
	endpoint := strings.TrimRight(os.Getenv("IOT_TEST_WEAVIATE_URL"), "/")
	embeddingURL := strings.TrimRight(os.Getenv("IOT_TEST_EMBEDDING_URL"), "/")
	if endpoint == "" || embeddingURL == "" {
		t.Skip("IOT_TEST_WEAVIATE_URL and IOT_TEST_EMBEDDING_URL not configured")
	}
	embedder, err := embedding.NewOpenAI(embedding.Config{BaseURL: embeddingURL, Model: envOr("IOT_TEST_EMBEDDING_MODEL", "Qwen/Qwen3-Embedding-0.6B"), APIKey: os.Getenv("IOT_TEST_EMBEDDING_API_KEY"), QueryInstruction: config.DefaultEmbeddingQueryInstruction})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	id := "audit-" + uuid.NewString()
	in := ports.KnowledgeIndexInput{TenantID: id, WorkflowID: "audit-workflow", DocumentID: id, ChunkID: id, Content: []byte("联调专用资料：巡检柜第六路输入状态为一，模拟测试已通过。")}
	objectID := knowledgeObjectID(in.TenantID, in.WorkflowID, in.ChunkID)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(cleanup, http.MethodDelete, endpoint+"/v1/objects/IotKnowledgeV2/"+objectID, nil)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error("temporary knowledge object cleanup failed")
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 204 && response.StatusCode != 404 {
			t.Errorf("temporary knowledge cleanup HTTP %d", response.StatusCode)
		}
	})
	w := NewWeaviate(endpoint, embedder)
	if err := w.IndexKnowledge(ctx, in); err != nil {
		t.Fatal("live knowledge indexing failed")
	}
	chunks, err := w.ListKnowledgeChunks(ctx, id, id)
	if err != nil || len(chunks) != 1 || chunks[0].Content != string(in.Content) {
		t.Fatal("indexed chunk readback mismatch")
	}
	query := ports.KnowledgeSearchRequest{TenantID: id, WorkflowID: in.WorkflowID, Question: "第六路输入状态", Limit: 3}
	hits, err := w.SearchKnowledge(ctx, query)
	if err != nil || len(hits) != 1 || hits[0].DocumentID != id {
		t.Fatal("live scoped vector retrieval failed")
	}
	query.WorkflowID = "unrelated-workflow"
	hits, err = w.SearchKnowledge(ctx, query)
	if err != nil || len(hits) != 0 {
		t.Fatal("knowledge leaked across workflow scope")
	}
	query.WorkflowID = in.WorkflowID
	query.TenantID = id + "-other"
	hits, err = w.SearchKnowledge(ctx, query)
	if err != nil || len(hits) != 0 {
		t.Fatal("knowledge leaked across tenant scope")
	}
}
