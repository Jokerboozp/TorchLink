package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/adapters/embedding"
	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type knowledgeHTTPEmbeddingState struct {
	mu        sync.Mutex
	failModel map[string]bool
	observeID string
	batches   []int
	progress  []struct{ Done, Total int }
}

func (s *knowledgeHTTPEmbeddingState) setFailure(model string, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failModel[model] = failed
}

// This covers durable jobs and the actual OpenAI-compatible HTTP adapter,
// sharing a real PostgreSQL pool. No cloud model API is contacted.
func TestKnowledgeRuntimePostgresHTTPJobsRecoveryAndModelSwitch(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schemaName := fmt.Sprintf("knowledge_runtime_test_%d", time.Now().UnixNano())
	schemaIdentifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schemaIdentifier); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schemaIdentifier+" CASCADE") }()
	// RuntimeParams modifications are not included by Config.ConnString. Put
	// search_path in the actual DSN passed to the repository constructor.
	scopedDSN := dsn + " search_path=" + schemaName + ",public"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, parseErr := url.Parse(dsn)
		if parseErr != nil {
			t.Fatal("invalid test PostgreSQL URL")
		}
		query := u.Query()
		query.Set("search_path", schemaName+",public")
		u.RawQuery = query.Encode()
		scopedDSN = u.String()
	}
	repo, err := postgres.NewWithOptions(ctx, scopedDSN, postgres.PoolOptions{MaxConns: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	var selectedSchema string
	if err = repo.Pool().QueryRow(ctx, `SELECT current_schema()`).Scan(&selectedSchema); err != nil || selectedSchema != schemaName {
		t.Fatalf("PostgreSQL test pool did not select the isolated schema: %v", err)
	}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := &knowledgeHTTPEmbeddingState{failModel: map[string]bool{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unexpected embedding API request", http.StatusBadRequest)
			return
		}
		var request struct {
			Model      string   `json:"model"`
			Input      []string `json:"input"`
			Dimensions int      `json:"dimensions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Input) == 0 || request.Dimensions != 2 {
			http.Error(w, "invalid embedding request", http.StatusBadRequest)
			return
		}
		state.mu.Lock()
		failed, observedDoc := state.failModel[request.Model], state.observeID
		state.batches = append(state.batches, len(request.Input))
		state.mu.Unlock()
		if failed {
			// Keep the client's retries within the test deadline.
			w.Header().Set("Retry-After", "1")
			http.Error(w, "simulated upstream failure", http.StatusServiceUnavailable)
			return
		}
		if observedDoc != "" {
			var raw []byte
			if err := repo.Pool().QueryRow(r.Context(), `SELECT metadata->'indexProgress' FROM ai_knowledge_doc WHERE id=$1`, observedDoc).Scan(&raw); err == nil && len(raw) > 0 {
				var progress struct{ Done, Total int }
				if json.Unmarshal(raw, &progress) == nil {
					state.mu.Lock()
					state.progress = append(state.progress, progress)
					state.mu.Unlock()
				}
			}
		}
		data := make([]map[string]any, len(request.Input))
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": []float32{1, .5}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	config := ports.EmbeddingConfig{BaseURL: server.URL + "/v1", Model: "model-a", APIKey: "test-key", Dimensions: 2, BatchSize: 1}
	for _, active := range []bool{false, true} {
		if err = repo.SaveEmbeddingConfig(ctx, config, active); err != nil {
			t.Fatal(err)
		}
	}
	factory := func(cfg ports.EmbeddingConfig) (ports.KnowledgeBase, error) {
		client, createErr := embedding.NewOpenAI(embedding.Config{BaseURL: cfg.BaseURL, Model: cfg.Model, APIKey: cfg.APIKey, QueryInstruction: cfg.QueryInstruction, Dimensions: cfg.Dimensions, BatchSize: cfg.BatchSize})
		if createErr != nil {
			return nil, createErr
		}
		return knowledge.NewPostgres(repo.Pool(), client, knowledge.PostgresOptions{Provider: "mock-http", Dimensions: cfg.Dimensions, ExpectedConfig: &cfg}), nil
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	newRuntime := func(desired, active ports.EmbeddingConfig) *KnowledgeRuntime {
		t.Helper()
		runtime, createErr := NewKnowledgeRuntime(desired, active, factory, repo, repo, repo, repo, archive, log)
		if createErr != nil {
			t.Fatal(createErr)
		}
		return runtime
	}
	runtime := newRuntime(config, config)
	putDocument := func(id, status, content string, metadata map[string]any) model.KnowledgeDoc {
		t.Helper()
		doc := model.KnowledgeDoc{ID: id, TenantID: "tenant-a", WorkflowID: "ops", ProductID: id, ObjectBucket: "knowledge", ObjectKey: id + ".txt", Filename: id + ".txt", Status: status, Metadata: metadata}
		if _, putErr := archive.PutObject(ctx, doc.ObjectBucket, doc.ObjectKey, bytes.NewBufferString(content), int64(len(content)), "text/plain"); putErr != nil {
			t.Fatal(putErr)
		}
		if saveErr := repo.SaveKnowledgeDoc(ctx, doc); saveErr != nil {
			t.Fatal(saveErr)
		}
		return doc
	}
	readDocument := func(id string) model.KnowledgeDoc {
		t.Helper()
		docs, queryErr := repo.ListKnowledgeDocs(ctx, "tenant-a")
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		for _, doc := range docs {
			if doc.ID == id {
				return doc
			}
		}
		t.Fatalf("document %s is missing", id)
		return model.KnowledgeDoc{}
	}
	assertStatus := func(id, status string) model.KnowledgeDoc {
		t.Helper()
		doc := readDocument(id)
		if doc.Status != status {
			t.Fatalf("document %s status=%s, want %s", id, doc.Status, status)
		}
		return doc
	}
	assertQueryable := func(runtime *KnowledgeRuntime, id string, present bool) {
		t.Helper()
		hits, queryErr := runtime.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: "tenant-a", WorkflowID: "ops", ProductIDs: []string{id}, Question: "烟雾告警复位", Limit: 5})
		if queryErr != nil || (len(hits) > 0) != present {
			t.Fatalf("query %s present=%v hits=%#v err=%v", id, present, hits, queryErr)
		}
		for _, hit := range hits {
			if hit.DocumentID != id || hit.EndChar-hit.StartChar != hit.CharacterCount {
				t.Fatalf("query source or positions lost: %#v", hit)
			}
		}
	}
	longText := strings.Repeat("烟雾告警复位现场核查规范。", 170)
	mainDoc := putDocument("main", "UPLOADED", longText, map[string]any{})
	assertStatus(mainDoc.ID, "UPLOADED")
	if err = runtime.rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	if doc := readDocument(mainDoc.ID); doc.Status != "UPLOADED" {
		t.Fatalf("ordinary upload bypassed the durable worker: %#v", doc)
	}
	state.mu.Lock()
	state.observeID = mainDoc.ID
	state.mu.Unlock()
	if err = runtime.processDocument(ctx); err != nil {
		t.Fatal(err)
	}
	indexed := assertStatus(mainDoc.ID, "INDEXED")
	var finalProgress struct{ Done, Total int }
	encodedProgress, _ := json.Marshal(indexed.Metadata["indexProgress"])
	if err = json.Unmarshal(encodedProgress, &finalProgress); err != nil || finalProgress.Done < 2 || finalProgress.Done != finalProgress.Total {
		t.Fatalf("completed batch progress was not persisted: %#v %v", indexed.Metadata, err)
	}
	state.mu.Lock()
	intermediateProgress := false
	for _, progress := range state.progress {
		if progress.Done > 0 && progress.Done < progress.Total {
			intermediateProgress = true
		}
	}
	state.observeID = ""
	state.mu.Unlock()
	if !intermediateProgress {
		t.Fatal("HTTP batch progress never became visible in PostgreSQL")
	}
	assertQueryable(runtime, mainDoc.ID, true)

	failedDoc := putDocument("retry", "UPLOADED", "烟雾告警复位失败重试", map[string]any{})
	state.setFailure("model-a", true)
	if err = runtime.processDocument(ctx); err != nil {
		t.Fatal(err)
	}
	// An unreachable vector service queues the document for a later retry.
	waiting := assertStatus(failedDoc.ID, "UPLOADED")
	if waiting.Metadata["indexStage"] != "retry_wait" || waiting.Metadata["indexError"] == nil || metadataInt(waiting.Metadata["indexAttempts"]) != 1 || waiting.Metadata["indexRetryAt"] == nil {
		t.Fatalf("transient embedding failure was not queued for retry: %#v", waiting.Metadata)
	}
	if err = runtime.processDocument(ctx); err != nil {
		t.Fatal(err)
	}
	waiting = assertStatus(failedDoc.ID, "UPLOADED")
	if metadataInt(waiting.Metadata["indexAttempts"]) != 1 {
		t.Fatal("document was retried before its retry time")
	}
	// The retry runs once it is due; using up the retries is unit tested.
	state.setFailure("model-a", false)
	waiting.Metadata["indexRetryAt"] = time.Now().Add(-time.Second).UnixMilli()
	if err = repo.SaveKnowledgeDoc(ctx, waiting); err != nil {
		t.Fatal(err)
	}
	if err = runtime.processDocument(ctx); err != nil {
		t.Fatal(err)
	}
	failed := assertStatus(failedDoc.ID, "INDEXED")
	if failed.Metadata["indexError"] != nil || failed.Metadata["indexAttempts"] != nil || failed.Metadata["indexRetryAt"] != nil {
		t.Fatalf("successful retry kept the failure state: %#v", failed.Metadata)
	}
	assertQueryable(runtime, failed.ID, true)

	queued := putDocument("restart-queued", "UPLOADED", "烟雾告警重启队列恢复", map[string]any{})
	expired := putDocument("restart-expired", "INDEXING", "烟雾告警租约过期恢复", map[string]any{"indexLeaseUntil": time.Now().Add(-time.Minute).UnixMilli()})
	runtime = newRuntime(config, config)
	for i := 0; i < 2; i++ {
		if err = runtime.processDocument(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertStatus(queued.ID, "INDEXED")
	assertStatus(expired.ID, "INDEXED")
	assertQueryable(runtime, queued.ID, true)
	assertQueryable(runtime, expired.ID, true)

	deleting := readDocument(failed.ID)
	deleting.Status = "DELETING"
	if err = repo.SaveKnowledgeDoc(ctx, deleting); err != nil {
		t.Fatal(err)
	}
	assertQueryable(runtime, failed.ID, false)
	if err = runtime.processDocument(ctx); err != nil {
		t.Fatal(err)
	}
	if reader, getErr := archive.GetObject(ctx, deleting.ObjectBucket, deleting.ObjectKey); getErr == nil {
		reader.Close()
		t.Fatal("deleted original was retained")
	} else if !errors.Is(getErr, os.ErrNotExist) {
		t.Fatal(getErr)
	}
	var retained int
	if err = repo.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM ai_knowledge_doc WHERE id=$1)+(SELECT count(*) FROM ai_knowledge_chunk WHERE document_id=$1)`, deleting.ID).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("deleted metadata/chunks retained: %d %v", retained, err)
	}

	newConfig := config
	newConfig.Model = "model-b"
	failedFirstUpload := putDocument("never-indexed", "INDEX_FAILED", "首次上传失败", map[string]any{"indexError": "unsupported document content"})
	lateUpload := putDocument("late-upload", "UPLOADED", "烟雾告警新上传文档", map[string]any{})
	state.setFailure("model-b", true)
	if err = runtime.Configure(ctx, newConfig); err != nil {
		t.Fatal(err)
	}
	if err = runtime.rebuild(ctx); err == nil {
		t.Fatal("failed replacement was accepted")
	}
	if runtime.EmbeddingModel() != "model-a" {
		t.Fatal("failed replacement changed the runtime model")
	}
	assertQueryable(runtime, mainDoc.ID, true)
	var activeModel string
	if err = repo.Pool().QueryRow(ctx, `SELECT model FROM ai_knowledge_index_version WHERE status='active'`).Scan(&activeModel); err != nil || activeModel != "model-a" {
		t.Fatalf("failed replacement changed active PostgreSQL index: %s %v", activeModel, err)
	}
	desired, found, err := repo.LoadEmbeddingConfig(ctx, false)
	if err != nil || !found || desired.Model != "model-b" {
		t.Fatalf("desired embedding configuration lost: %#v %v %v", desired, found, err)
	}
	active, found, err := repo.LoadEmbeddingConfig(ctx, true)
	if err != nil || !found || active.Model != "model-a" {
		t.Fatalf("working embedding configuration lost: %#v %v %v", active, found, err)
	}
	runtime = newRuntime(desired, active)
	assertQueryable(runtime, mainDoc.ID, true)
	state.setFailure("model-b", false)
	if err = runtime.rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	assertStatus(failedFirstUpload.ID, "INDEX_FAILED")
	assertStatus(lateUpload.ID, "UPLOADED")
	if runtime.EmbeddingModel() != "model-b" {
		t.Fatal("completed replacement did not become active")
	}
	var orphaned int
	if err = repo.Pool().QueryRow(ctx, `SELECT count(*) FROM ai_knowledge_chunk c JOIN ai_knowledge_index_version v ON v.id=c.version_id WHERE v.status IN ('building','failed')`).Scan(&orphaned); err != nil || orphaned != 0 {
		t.Fatalf("rebuild retry retained failed partial chunks: %d %v", orphaned, err)
	}
	for _, id := range []string{mainDoc.ID, queued.ID, expired.ID} {
		assertStatus(id, "INDEXED")
		assertQueryable(runtime, id, true)
	}
	active, found, err = repo.LoadEmbeddingConfig(ctx, true)
	if err != nil || !found || active.Model != "model-b" {
		t.Fatalf("completed active configuration was not persisted: %#v %v %v", active, found, err)
	}
	// A replica that still holds the previous model must refresh the committed
	// active configuration under the worker lock before claiming a new upload.
	staleReplica := newRuntime(desired, config)
	if err = staleReplica.processDocument(ctx); err != nil {
		t.Fatal(err)
	}
	if staleReplica.EmbeddingModel() != "model-b" {
		t.Fatal("stale replica did not adopt the completed active configuration")
	}
	assertStatus(lateUpload.ID, "INDEXED")
	assertQueryable(staleReplica, lateUpload.ID, true)
	runtime = newRuntime(desired, active)
	assertQueryable(runtime, mainDoc.ID, true)
}
