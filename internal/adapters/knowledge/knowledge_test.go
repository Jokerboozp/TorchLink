package knowledge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"iot-platform/internal/ports"
)

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

func TestWeaviateIndexUsesDeterministicUUIDAndEmbeddingConfig(t *testing.T) {
	var schema map[string]any
	var object map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledge":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/schema":
			if err := json.NewDecoder(r.Body).Decode(&schema); err != nil {
				t.Fatalf("decode schema: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/objects":
			if err := json.NewDecoder(r.Body).Decode(&object); err != nil {
				t.Fatalf("decode object: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	index := NewWeaviate(server.URL)
	err := index.IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{
		TenantID: "tenant-1", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("设备告警处置"),
	})
	if err != nil {
		t.Fatalf("index knowledge: %v", err)
	}

	moduleConfig, ok := schema["moduleConfig"].(map[string]any)
	if !ok {
		t.Fatalf("missing moduleConfig in %#v", schema)
	}
	ollama, ok := moduleConfig["text2vec-ollama"].(map[string]any)
	if !ok || ollama["apiEndpoint"] != "http://ollama:11434" || ollama["model"] != "nomic-embed-text" {
		t.Fatalf("unexpected Ollama module config %#v", moduleConfig)
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
	objectID, ok := object["id"].(string)
	if !ok {
		t.Fatalf("missing object id in %#v", object)
	}
	if _, err := uuid.Parse(objectID); err != nil {
		t.Fatalf("object id is not a UUID: %q", objectID)
	}
	if class, _ := object["class"].(string); class != "IotKnowledge" {
		t.Fatalf("unexpected object class %#v", object["class"])
	}
	objectProperties, ok := object["properties"].(map[string]any)
	if !ok || objectProperties["chunkId"] != "chunk-1" || objectProperties["chunkIndex"] != float64(0) || objectProperties["characterCount"] != float64(len([]rune("设备告警处置"))) {
		t.Fatalf("chunk metadata was not persisted in object %#v", objectProperties)
	}
}

func TestWeaviateIndexDoesNotTreat422AsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledge" {
			w.Header().Set("Content-Type", "application/json")
			body, _ := json.Marshal(map[string]any{"class": "IotKnowledge", "properties": knowledgeProperties()})
			_, _ = w.Write(body)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/objects" {
			http.Error(w, `{"error":[{"message":"embedding failed"}]}`, http.StatusUnprocessableEntity)
			return
		}
		http.Error(w, strings.TrimSpace("unexpected request"), http.StatusNotFound)
	}))
	defer server.Close()

	err := NewWeaviate(server.URL).IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{
		TenantID: "tenant-1", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("失败"),
	})
	if err == nil {
		t.Fatal("expected 422 indexing error")
	}
}

func TestWeaviateListsStoredChunksInOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledge":
			body, _ := json.Marshal(map[string]any{"class": "IotKnowledge", "properties": knowledgeProperties()})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/graphql":
			_, _ = io.WriteString(w, `{"data":{"Get":{"IotKnowledge":[{"documentId":"doc-1","chunkId":"doc-1-chunk-0002","chunkIndex":2,"startChar":4,"endChar":10,"characterCount":6,"overlapChars":2,"content":"戊己庚辛壬癸"},{"documentId":"doc-1","chunkId":"doc-1-chunk-0001","chunkIndex":1,"startChar":0,"endChar":6,"characterCount":6,"overlapChars":0,"content":"甲乙丙丁戊己"}]}}}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	chunks, err := NewWeaviate(server.URL).ListKnowledgeChunks(context.Background(), "tenant-a", "doc-1")
	if err != nil || len(chunks) != 2 {
		t.Fatalf("unexpected chunks=%#v err=%v", chunks, err)
	}
	if chunks[0].Index != 1 || chunks[1].Index != 2 || !chunks[0].Vectorized || chunks[1].OverlapChars != 2 || chunks[1].Content != "戊己庚辛壬癸" {
		t.Fatalf("unexpected ordered chunks %#v", chunks)
	}
}

func TestWeaviateLiveIndexAndScopeIsolation(t *testing.T) {
	endpoint := strings.TrimRight(os.Getenv("IOT_TEST_WEAVIATE_URL"), "/")
	if endpoint == "" {
		t.Skip("IOT_TEST_WEAVIATE_URL not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	id := "audit-" + uuid.NewString()
	in := ports.KnowledgeIndexInput{TenantID: id, WorkflowID: "audit-workflow", DocumentID: id, ChunkID: id, Content: []byte("联调专用资料：巡检柜第六路输入状态为一，模拟测试已通过。")}
	objectID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{in.TenantID, in.WorkflowID, in.ChunkID}, "\x00")))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(cleanup, http.MethodDelete, endpoint+"/v1/objects/IotKnowledge/"+objectID.String(), nil)
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
	w := NewWeaviate(endpoint)
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
