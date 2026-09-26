package knowledge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"iot-platform/internal/ports"
)

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
