package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type rebuildTestIndex struct {
	mu            sync.Mutex
	stale         bool
	resets        int
	legacyDropped bool
	indexed       []ports.KnowledgeIndexInput
	failDocument  string
}

func (f *rebuildTestIndex) Index(context.Context, string, string, string, []byte) error { return nil }
func (f *rebuildTestIndex) Search(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
func (f *rebuildTestIndex) Health(context.Context) error { return nil }
func (f *rebuildTestIndex) NeedsRebuild(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stale, nil
}
func (f *rebuildTestIndex) ResetIndex(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets++
	f.indexed = nil
	return nil
}
func (f *rebuildTestIndex) DropLegacyIndex(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.legacyDropped, f.stale = true, false
	return nil
}
func (f *rebuildTestIndex) EmbeddingModel() string { return "Qwen/Qwen3-Embedding-0.6B" }
func (f *rebuildTestIndex) IndexKnowledgeBatch(_ context.Context, inputs []ports.KnowledgeIndexInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(inputs) > 0 && inputs[0].DocumentID == f.failDocument {
		return errors.New("embedding service unavailable")
	}
	f.indexed = append(f.indexed, inputs...)
	return nil
}

func TestKnowledgeReindexerRebuildsFromArchivedOriginals(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	docs := []model.KnowledgeDoc{
		{ID: "doc-a", TenantID: "tenant-a", WorkflowID: "wf-a", ProductID: "p1", Category: "manual", Tags: []string{"复位"}, Filename: "a.txt", Status: "INDEXED", CreatedAt: 1},
		{ID: "doc-b", TenantID: "tenant-b", WorkflowID: "wf-b", Filename: "b.txt", Status: "INDEXED", CreatedAt: 2},
		{ID: "doc-missing", TenantID: "tenant-b", WorkflowID: "wf-b", Filename: "missing.txt", Status: "INDEXED", CreatedAt: 3},
	}
	for i, doc := range docs {
		doc.ObjectBucket, doc.ObjectKey = "iot-knowledge-docs", doc.TenantID+"/"+doc.Filename
		docs[i] = doc
		if doc.ID != "doc-missing" {
			body := strings.Repeat("火灾报警控制器复位步骤。", 150)
			if _, err := archive.PutObject(ctx, doc.ObjectBucket, doc.ObjectKey, bytes.NewReader([]byte(body)), int64(len(body)), "text/plain"); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.SaveKnowledgeDoc(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	index := &rebuildTestIndex{stale: true, failDocument: "doc-b"}
	reindexer := &KnowledgeReindexer{KB: index, Store: repo, Repo: repo, Archive: archive, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := reindexer.runOnce(ctx, index); err == nil {
		t.Fatal("partial rebuild must preserve the active index and report failure")
	}
	status := reindexer.Status()
	if status.State != "failed" || status.Total != 3 || status.Done != 3 || status.Failed != 2 {
		t.Fatalf("unexpected rebuild status %#v", status)
	}
	if index.resets != 1 || index.legacyDropped {
		t.Fatalf("failed rebuild must not activate the partial version: resets=%d legacy=%v", index.resets, index.legacyDropped)
	}
	if len(index.indexed) < 2 {
		t.Fatalf("expected doc-a chunks, got %d", len(index.indexed))
	}
	for _, chunk := range index.indexed {
		if chunk.DocumentID != "doc-a" || chunk.TenantID != "tenant-a" || chunk.WorkflowID != "wf-a" || chunk.ProductID != "p1" || chunk.Category != "manual" || len(chunk.Tags) != 1 {
			t.Fatalf("rebuilt chunk lost its scope metadata: %#v", chunk)
		}
	}
	byID := map[string]model.KnowledgeDoc{}
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		items, _ := repo.ListKnowledgeDocs(ctx, tenant)
		for _, item := range items {
			byID[item.ID] = item
		}
	}
	if byID["doc-a"].Status != "INDEXED" || byID["doc-a"].Metadata["embeddingModel"] != "Qwen/Qwen3-Embedding-0.6B" {
		t.Fatalf("rebuilt document not marked: %#v", byID["doc-a"])
	}
	for _, id := range []string{"doc-b", "doc-missing"} {
		if byID[id].Status != "INDEX_FAILED" || byID[id].Metadata["indexError"] == "" || byID[id].Metadata["rebuildRequired"] != true {
			t.Fatalf("failed document %s must record the failure without blocking others: %#v", id, byID[id])
		}
	}

	// A current index is left untouched.
	index.stale = false
	if err := reindexer.runOnce(ctx, index); err != nil || index.resets != 1 {
		t.Fatalf("current index must not be rebuilt again: resets=%d err=%v", index.resets, err)
	}
}

type lockedReindexStore struct{ ports.KnowledgeReindexStore }

func (lockedReindexStore) TryKnowledgeReindexLock(context.Context) (func(), bool, error) {
	return nil, false, nil
}

func TestKnowledgeReindexerWaitsForAnotherReplica(t *testing.T) {
	index := &rebuildTestIndex{stale: true}
	repo := memory.NewRepository()
	reindexer := &KnowledgeReindexer{KB: index, Store: lockedReindexStore{repo}, Repo: repo, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { reindexer.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for reindexer.Status().State != "rebuilding" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if status := reindexer.Status(); status.State != "rebuilding" || status.Error != "" {
		t.Fatalf("a rebuild on another replica must be reported as rebuilding, got %#v", status)
	}
	cancel()
	<-done
	if index.resets != 0 {
		t.Fatal("index must not be reset without holding the rebuild lock")
	}
}

func TestKnowledgeWhitespaceOverlapKeepsValidOffsets(t *testing.T) {
	text := "甲乙丙丁戊己庚辛壬癸" + strings.Repeat(" ", 25) + "结束"
	chunks := ChunkKnowledgeTextDetailed(text, 12, 6)
	for _, chunk := range chunks {
		if chunk.OverlapChars < 0 || chunk.OverlapChars > chunk.CharacterCount {
			t.Fatalf("invalid overlap: %#v", chunk)
		}
		if got := string([]rune(text)[chunk.StartChar:chunk.EndChar]); got != chunk.Text {
			t.Fatalf("offsets lost text: %q != %q", got, chunk.Text)
		}
	}
}
