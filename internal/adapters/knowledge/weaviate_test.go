package knowledge /* 声明 knowledge 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"github.com/google/uuid" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestWeaviateIndexUsesDeterministicUUIDAndEmbeddingConfig(t *testing.T) { /* 定义 TestWeaviateIndexUsesDeterministicUUIDAndEmbeddingConfig 函数。 */
	var schema map[string]any                                                                    /* 声明 schema。 */
	var object map[string]any                                                                    /* 声明 object。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		switch { /* 根据条件选择处理路径。 */
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledge": /* 处理当前分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
		case r.Method == http.MethodPost && r.URL.Path == "/v1/schema": /* 处理当前分支。 */
			if err := json.NewDecoder(r.Body).Decode(&schema); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatalf("decode schema: %v", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			w.WriteHeader(http.StatusOK) /* 执行当前语句并推进处理流程。 */
		case r.Method == http.MethodPost && r.URL.Path == "/v1/objects": /* 处理当前分支。 */
			if err := json.NewDecoder(r.Body).Decode(&object); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatalf("decode object: %v", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			w.WriteHeader(http.StatusCreated) /* 执行当前语句并推进处理流程。 */
		default: /* 处理当前分支。 */
			http.Error(w, "unexpected request", http.StatusNotFound) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	index := NewWeaviate(server.URL)                                             /* 更新 index 的值。 */
	err := index.IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{ /* 更新 err 的值。 */
		TenantID: "tenant-1", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("设备告警处置"), /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("index knowledge: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	moduleConfig, ok := schema["moduleConfig"].(map[string]any) /* 更新 ok 的值。 */
	if !ok {                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("missing moduleConfig in %#v", schema) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ollama, ok := moduleConfig["text2vec-ollama"].(map[string]any)                                      /* 更新 ok 的值。 */
	if !ok || ollama["apiEndpoint"] != "http://ollama:11434" || ollama["model"] != "nomic-embed-text" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected Ollama module config %#v", moduleConfig) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	properties, ok := schema["properties"].([]any) /* 更新 ok 的值。 */
	if !ok {                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("missing schema properties in %#v", schema) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, raw := range properties { /* 循环处理当前数据。 */
		property, _ := raw.(map[string]any) /* 更新 _ 的值。 */
		if property["name"] != "tags" {     /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		dataType, _ := property["dataType"].([]any)        /* 更新 _ 的值。 */
		if len(dataType) != 1 || dataType[0] != "text[]" { /* 判断条件并选择处理分支。 */
			t.Fatalf("tags property is not a text array: %#v", property) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	objectID, ok := object["id"].(string) /* 更新 ok 的值。 */
	if !ok {                              /* 判断条件并选择处理分支。 */
		t.Fatalf("missing object id in %#v", object) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := uuid.Parse(objectID); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("object id is not a UUID: %q", objectID) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if class, _ := object["class"].(string); class != "IotKnowledge" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected object class %#v", object["class"]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	objectProperties, ok := object["properties"].(map[string]any)                                                                                                                /* 更新 ok 的值。 */
	if !ok || objectProperties["chunkId"] != "chunk-1" || objectProperties["chunkIndex"] != float64(0) || objectProperties["characterCount"] != float64(len([]rune("设备告警处置"))) { /* 判断条件并选择处理分支。 */
		t.Fatalf("chunk metadata was not persisted in object %#v", objectProperties) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestWeaviateIndexDoesNotTreat422AsSuccess(t *testing.T) { /* 定义 TestWeaviateIndexDoesNotTreat422AsSuccess 函数。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledge" { /* 判断条件并选择处理分支。 */
			w.Header().Set("Content-Type", "application/json")                                                    /* 执行当前语句并推进处理流程。 */
			body, _ := json.Marshal(map[string]any{"class": "IotKnowledge", "properties": knowledgeProperties()}) /* 更新 _ 的值。 */
			_, _ = w.Write(body)                                                                                  /* 更新 _ 的值。 */
			return                                                                                                /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if r.Method == http.MethodPost && r.URL.Path == "/v1/objects" { /* 判断条件并选择处理分支。 */
			http.Error(w, `{"error":[{"message":"embedding failed"}]}`, http.StatusUnprocessableEntity) /* 执行当前语句并推进处理流程。 */
			return                                                                                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		http.Error(w, strings.TrimSpace("unexpected request"), http.StatusNotFound) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	err := NewWeaviate(server.URL).IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{ /* 更新 err 的值。 */
		TenantID: "tenant-1", WorkflowID: "ops-assistant", DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("失败"), /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("expected 422 indexing error") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestWeaviateListsStoredChunksInOrder(t *testing.T) { /* 定义 TestWeaviateListsStoredChunksInOrder 函数。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		switch { /* 根据条件选择处理路径。 */
		case r.Method == http.MethodGet && r.URL.Path == "/v1/schema/IotKnowledge": /* 处理当前分支。 */
			body, _ := json.Marshal(map[string]any{"class": "IotKnowledge", "properties": knowledgeProperties()}) /* 更新 _ 的值。 */
			w.Header().Set("Content-Type", "application/json")                                                    /* 执行当前语句并推进处理流程。 */
			_, _ = w.Write(body)                                                                                  /* 更新 _ 的值。 */
		case r.Method == http.MethodPost && r.URL.Path == "/v1/graphql": /* 处理当前分支。 */
			_, _ = io.WriteString(w, `{"data":{"Get":{"IotKnowledge":[{"documentId":"doc-1","chunkId":"doc-1-chunk-0002","chunkIndex":2,"startChar":4,"endChar":10,"characterCount":6,"overlapChars":2,"content":"戊己庚辛壬癸"},{"documentId":"doc-1","chunkId":"doc-1-chunk-0001","chunkIndex":1,"startChar":0,"endChar":6,"characterCount":6,"overlapChars":0,"content":"甲乙丙丁戊己"}]}}}`) /* 更新 _ 的值。 */
		default: /* 处理当前分支。 */
			http.Error(w, "unexpected request", http.StatusNotFound) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	chunks, err := NewWeaviate(server.URL).ListKnowledgeChunks(context.Background(), "tenant-a", "doc-1") /* 更新 err 的值。 */
	if err != nil || len(chunks) != 2 {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected chunks=%#v err=%v", chunks, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if chunks[0].Index != 1 || chunks[1].Index != 2 || !chunks[0].Vectorized || chunks[1].OverlapChars != 2 || chunks[1].Content != "戊己庚辛壬癸" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected ordered chunks %#v", chunks) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
