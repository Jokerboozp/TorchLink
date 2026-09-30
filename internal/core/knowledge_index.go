package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	KnowledgeChunkSize    = 1200
	KnowledgeChunkOverlap = 200
	maxKnowledgeDocBytes  = 32 << 20
)

// KnowledgeContentError marks documents that cannot be indexed because of
// their content (unsupported format, no text) rather than an index failure.
type KnowledgeContentError struct{ Err error }

func (e KnowledgeContentError) Error() string { return e.Err.Error() }
func (e KnowledgeContentError) Unwrap() error { return e.Err }

// KnowledgeIndexResult describes what was stored for a document.
type KnowledgeIndexResult struct {
	Characters int
	Chunks     int
}

// IndexKnowledgeDocument extracts text, splits it with the platform chunking
// strategy and writes every chunk to the workflow-bound index. Upload and the
// index rebuild share it so both produce identical chunks.
func IndexKnowledgeDocument(ctx context.Context, kb ports.KnowledgeBase, doc model.KnowledgeDoc, data []byte) (KnowledgeIndexResult, error) {
	text, err := ExtractKnowledgeText(doc.Filename, data)
	if err != nil {
		return KnowledgeIndexResult{}, KnowledgeContentError{err}
	}
	chunks := ChunkKnowledgeTextDetailed(text, KnowledgeChunkSize, KnowledgeChunkOverlap)
	if len(chunks) == 0 {
		return KnowledgeIndexResult{}, KnowledgeContentError{errors.New("document contains no indexable text")}
	}
	inputs := make([]ports.KnowledgeIndexInput, len(chunks))
	for i, chunk := range chunks {
		inputs[i] = ports.KnowledgeIndexInput{TenantID: doc.TenantID, WorkflowID: doc.WorkflowID, ProductID: doc.ProductID, Category: doc.Category, Tags: doc.Tags, DocumentID: doc.ID, ChunkID: fmt.Sprintf("%s-chunk-%04d", doc.ID, i+1), ChunkIndex: chunk.Index, StartChar: chunk.StartChar, EndChar: chunk.EndChar, CharacterCount: chunk.CharacterCount, OverlapChars: chunk.OverlapChars, Content: []byte(chunk.Text)}
	}
	switch index := kb.(type) {
	case ports.BatchKnowledgeBase:
		err = index.IndexKnowledgeBatch(ctx, inputs)
	case ports.FilteredKnowledgeBase:
		for _, input := range inputs {
			if err = index.IndexKnowledge(ctx, input); err != nil {
				break
			}
		}
	default:
		err = errors.New("workflow-bound knowledge indexing is not supported by the configured index")
	}
	if err != nil {
		return KnowledgeIndexResult{}, err
	}
	return KnowledgeIndexResult{Characters: len([]rune(text)), Chunks: len(chunks)}, nil
}

// errRebuildElsewhere means another API replica holds the rebuild lock.
var errRebuildElsewhere = errors.New("knowledge index rebuild is running on another replica")

// KnowledgeIndexStatus is reported on the knowledge page while the index is
// rebuilt for a new embedding model.
type KnowledgeIndexStatus struct {
	State  string `json:"state"` // ready, rebuilding, failed
	Done   int    `json:"done"`
	Failed int    `json:"failed"`
	Total  int    `json:"total"`
	Error  string `json:"error,omitempty"`
}

// KnowledgeReindexer re-embeds every stored document from its archived
// original when the persistent index was built with another embedding model.
type KnowledgeReindexer struct {
	KB      ports.KnowledgeBase
	Store   ports.KnowledgeReindexStore
	Repo    ports.Repository
	Archive ports.Archive
	Log     interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}

	mu         sync.RWMutex
	status     KnowledgeIndexStatus
	StatusSink func(KnowledgeIndexStatus)
}

func (k *KnowledgeReindexer) Status() KnowledgeIndexStatus {
	if k == nil {
		return KnowledgeIndexStatus{State: "ready"}
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.status.State == "" {
		return KnowledgeIndexStatus{State: "ready"}
	}
	return k.status
}

func (k *KnowledgeReindexer) setStatus(update func(*KnowledgeIndexStatus)) {
	k.mu.Lock()
	update(&k.status)
	status := k.status
	k.mu.Unlock()
	if k.StatusSink != nil {
		k.StatusSink(status)
	}
}

// Run checks the index once and rebuilds it when required. It retries while
// the index or embedding service is still starting.
func (k *KnowledgeReindexer) Run(ctx context.Context) {
	index, ok := k.KB.(ports.RebuildableKnowledgeBase)
	if !ok || k.Store == nil {
		return
	}
	for attempt := 0; ; attempt++ {
		err := k.runOnce(ctx, index)
		if err == nil || ctx.Err() != nil {
			return
		}
		if errors.Is(err, errRebuildElsewhere) {
			k.setStatus(func(s *KnowledgeIndexStatus) { *s = KnowledgeIndexStatus{State: "rebuilding"} })
		} else {
			k.setStatus(func(s *KnowledgeIndexStatus) { s.State, s.Error = "failed", err.Error() })
			k.Log.Warn("knowledge index rebuild check failed", "attempt", attempt+1, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(min(time.Duration(attempt+1)*10*time.Second, 5*time.Minute)):
		}
	}
}

func (k *KnowledgeReindexer) runOnce(ctx context.Context, index ports.RebuildableKnowledgeBase) error {
	needed, err := index.NeedsRebuild(ctx)
	if err != nil || !needed {
		if err == nil {
			k.setStatus(func(s *KnowledgeIndexStatus) { *s = KnowledgeIndexStatus{State: "ready"} })
		}
		return err
	}
	release, locked, err := k.Store.TryKnowledgeReindexLock(ctx)
	if err != nil {
		return err
	}
	if !locked {
		// Another replica is rebuilding; check again later.
		return errRebuildElsewhere
	}
	defer release()
	// Re-check under the lock: another replica may have just finished.
	if needed, err = index.NeedsRebuild(ctx); err != nil || !needed {
		if err == nil {
			k.setStatus(func(s *KnowledgeIndexStatus) { *s = KnowledgeIndexStatus{State: "ready"} })
		}
		return err
	}
	docs, err := k.Store.ListAllKnowledgeDocs(ctx)
	if err != nil {
		return err
	}
	var eligible map[string]bool
	if scoped, ok := index.(interface {
		RebuildDocumentIDs(context.Context) (map[string]bool, error)
	}); ok {
		eligible, err = scoped.RebuildDocumentIDs(ctx)
		if err != nil {
			return err
		}
	}
	filtered := docs[:0]
	for _, doc := range docs {
		if doc.Status != "DELETING" && (eligible == nil || eligible[doc.ID]) {
			filtered = append(filtered, doc)
		}
	}
	docs = filtered
	k.Log.Info("rebuilding knowledge index", "embeddingModel", index.EmbeddingModel(), "documents", len(docs))
	k.setStatus(func(s *KnowledgeIndexStatus) { *s = KnowledgeIndexStatus{State: "rebuilding", Total: len(docs)} })
	if err = index.ResetIndex(ctx); err != nil {
		return err
	}
	failed := 0
	for _, doc := range docs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result, indexErr := k.reindexDocument(ctx, doc)
		if doc.Metadata == nil {
			doc.Metadata = map[string]any{}
		}
		doc.Metadata["embeddingModel"] = index.EmbeddingModel()
		if indexErr != nil {
			failed++
			// A legacy INDEXED document can have no PostgreSQL chunks yet. Keep
			// it required after a failed attempt so a later retry cannot silently
			// activate an index which omits previously available knowledge.
			doc.Metadata["rebuildRequired"] = true
			doc.Status = "INDEX_FAILED"
			doc.Metadata["indexError"] = indexErr.Error()
			k.Log.Error("knowledge document rebuild failed", "tenantId", doc.TenantID, "documentId", doc.ID, "error", indexErr)
		} else {
			doc.Status = "INDEXED"
			doc.Metadata["chunks"] = result.Chunks
			doc.Metadata["characters"] = result.Characters
			delete(doc.Metadata, "indexError")
			delete(doc.Metadata, "rebuildRequired")
		}
		if jobs, ok := k.Repo.(ports.KnowledgeDocumentJobs); ok {
			if _, err = jobs.UpdateKnowledgeDocument(ctx, doc); err != nil {
				return err
			}
		} else if err = k.Repo.SaveKnowledgeDoc(ctx, doc); err != nil {
			return err
		}
		k.setStatus(func(s *KnowledgeIndexStatus) {
			s.Done++
			s.Failed = failed
		})
	}
	if failed > 0 {
		k.setStatus(func(s *KnowledgeIndexStatus) {
			s.State = "failed"
			s.Error = fmt.Sprintf("%d documents failed", failed)
		})
		return fmt.Errorf("knowledge index rebuild failed for %d documents; the active index was preserved", failed)
	}
	if err = index.DropLegacyIndex(ctx); err != nil {
		return fmt.Errorf("activate knowledge index: %w", err)
	}
	k.setStatus(func(s *KnowledgeIndexStatus) { s.State = "ready" })
	k.Log.Info("knowledge index rebuilt", "documents", len(docs), "failed", failed)
	return nil
}

func (k *KnowledgeReindexer) reindexDocument(ctx context.Context, doc model.KnowledgeDoc) (KnowledgeIndexResult, error) {
	reader, err := k.Archive.GetObject(ctx, doc.ObjectBucket, doc.ObjectKey)
	if err != nil {
		return KnowledgeIndexResult{}, fmt.Errorf("read archived document: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxKnowledgeDocBytes+1))
	if err != nil {
		return KnowledgeIndexResult{}, fmt.Errorf("read archived document: %w", err)
	}
	if len(data) > maxKnowledgeDocBytes {
		return KnowledgeIndexResult{}, errors.New("archived document exceeds 32 MiB")
	}
	return IndexKnowledgeDocument(ctx, k.KB, doc, data)
}
