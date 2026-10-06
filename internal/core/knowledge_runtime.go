package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/aiprompt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

type KnowledgeFactory func(ports.EmbeddingConfig) (ports.KnowledgeBase, error)

// KnowledgeRuntime keeps queries on the last completed vector space while a
// candidate is rebuilt. The document rows are also the durable indexing queue.
type KnowledgeRuntime struct {
	mu           sync.RWMutex
	config       ports.EmbeddingConfig
	activeConfig ports.EmbeddingConfig
	active       ports.KnowledgeBase
	factory      KnowledgeFactory
	store        ports.EmbeddingConfigStore
	repo         ports.Repository
	locks        ports.KnowledgeReindexStore
	jobs         ports.KnowledgeDocumentJobs
	archive      ports.Archive
	StatusView   *KnowledgeReindexer
	// wake shortens the idle backoff after an upload or a configuration change.
	wake chan struct{}
	log  interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

func NewKnowledgeRuntime(cfg, activeCfg ports.EmbeddingConfig, factory KnowledgeFactory, store ports.EmbeddingConfigStore, repo ports.Repository, locks ports.KnowledgeReindexStore, jobs ports.KnowledgeDocumentJobs, archive ports.Archive, log interface {
	Info(string, ...any)
	Warn(string, ...any)
	Error(string, ...any)
}) (*KnowledgeRuntime, error) {
	active, err := factory(activeCfg)
	if err != nil {
		return nil, err
	}
	return &KnowledgeRuntime{config: cfg, activeConfig: activeCfg, active: active, factory: factory, store: store, repo: repo, locks: locks, jobs: jobs, archive: archive, log: log, StatusView: &KnowledgeReindexer{}, wake: make(chan struct{}, 1)}, nil
}

func (k *KnowledgeRuntime) CurrentConfig() ports.EmbeddingConfig {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.config
}
func (k *KnowledgeRuntime) current() ports.KnowledgeBase {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.active
}
func (k *KnowledgeRuntime) Configure(ctx context.Context, cfg ports.EmbeddingConfig) error {
	if _, err := k.factory(cfg); err != nil {
		return err
	}
	if k.store != nil {
		if err := k.store.SaveEmbeddingConfig(ctx, cfg, false); err != nil {
			return err
		}
	}
	k.mu.Lock()
	k.config = cfg
	k.mu.Unlock()
	k.Wake()
	return nil
}

// Wake makes the runtime check its queue now instead of after the idle
// backoff; upload, retry and delete handlers call it.
func (k *KnowledgeRuntime) Wake() {
	if k.wake == nil {
		return
	}
	select {
	case k.wake <- struct{}{}:
	default:
	}
}

// Knowledge work is checked every 2 seconds while there is work and backs off
// to 30 seconds when idle, so an idle platform does not query the database
// several times every 2 seconds on every replica.
const (
	knowledgeTickBusy = 2 * time.Second
	knowledgeTickIdle = 30 * time.Second
)

func (k *KnowledgeRuntime) Run(ctx context.Context) {
	var nextBuild time.Time
	idle := 0
	rebuildElsewhere := false
	for {
		if k.store != nil {
			if cfg, found, err := k.store.LoadEmbeddingConfig(ctx, false); err == nil && found {
				k.mu.Lock()
				k.config = cfg
				k.mu.Unlock()
			}
		}
		if err := k.syncActiveConfig(ctx); err != nil && ctx.Err() == nil {
			k.log.Warn("refresh active knowledge configuration", "error", err)
		}
		if time.Now().After(nextBuild) {
			err := k.rebuild(ctx)
			switch {
			case errors.Is(err, errRebuildElsewhere):
				// Another replica is rebuilding; say so once, not every retry.
				if !rebuildElsewhere {
					k.log.Info("knowledge index rebuild is running on another replica")
				}
				rebuildElsewhere = true
				nextBuild = time.Now().Add(15 * time.Second)
			case err != nil:
				rebuildElsewhere = false
				k.log.Warn("knowledge index rebuild deferred", "error", err)
				nextBuild = time.Now().Add(15 * time.Second)
			default:
				rebuildElsewhere = false
			}
		}
		// Work through the queue each tick instead of one document per tick.
		worked := false
		for range knowledgeDocumentsPerTick {
			processed, err := k.processNextDocument(ctx)
			if err != nil && ctx.Err() == nil {
				k.log.Warn("knowledge document job failed", "error", err)
			}
			worked = worked || processed
			if !processed || err != nil {
				break
			}
		}
		if worked || rebuildElsewhere {
			idle = 0
		} else {
			idle++
		}
		delay := min(knowledgeTickBusy<<min(idle, 4), knowledgeTickIdle)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-k.wake: // nil (never ready) for a runtime built without NewKnowledgeRuntime
			timer.Stop()
			idle = 0
		case <-timer.C:
		}
	}
}

func (k *KnowledgeRuntime) rebuild(ctx context.Context) error {
	cfg := k.CurrentConfig()
	k.mu.RLock()
	same := cfg == k.activeConfig
	current := k.active
	k.mu.RUnlock()
	if same {
		if index, ok := current.(ports.RebuildableKnowledgeBase); ok {
			needed, err := index.NeedsRebuild(ctx)
			if err != nil {
				return err
			}
			if !needed {
				// Persist the initial environment configuration even for an empty
				// index, so later switches can recover the last working space.
				if atomic, ok := current.(interface {
					ActiveEmbeddingConfigPersisted() bool
					PersistActiveEmbeddingConfig(context.Context) error
				}); ok && !atomic.ActiveEmbeddingConfigPersisted() {
					if err := atomic.PersistActiveEmbeddingConfig(ctx); err != nil {
						return err
					}
				}
				k.StatusView.setStatus(func(s *KnowledgeIndexStatus) { *s = KnowledgeIndexStatus{State: "ready"} })
				return nil
			}
		}
	}
	candidate, err := k.factory(cfg)
	if err != nil {
		return err
	}
	index, ok := candidate.(ports.RebuildableKnowledgeBase)
	if !ok {
		return errors.New("knowledge index does not support versioned rebuild")
	}
	reindex := &KnowledgeReindexer{KB: candidate, Repo: k.repo, Store: k.locks, Archive: k.archive, Log: k.log, StatusSink: func(status KnowledgeIndexStatus) {
		k.StatusView.setStatus(func(s *KnowledgeIndexStatus) { *s = status })
	}}
	err = reindex.runOnce(ctx, index)
	if err != nil {
		k.StatusView.setStatus(func(s *KnowledgeIndexStatus) { s.State = "failed"; s.Error = err.Error() })
		return err
	}
	if atomic, ok := candidate.(interface {
		ActiveEmbeddingConfigPersisted() bool
		PersistActiveEmbeddingConfig(context.Context) error
	}); ok {
		if !atomic.ActiveEmbeddingConfigPersisted() {
			if err = atomic.PersistActiveEmbeddingConfig(ctx); err != nil {
				return err
			}
		}
	} else if k.store != nil {
		if err = k.store.SaveEmbeddingConfig(ctx, cfg, true); err != nil {
			return err
		}
	}
	// Only the completed index changes here; a newer desired configuration is
	// retained for the next rebuild. PostgreSQL already committed index+config.
	k.mu.Lock()
	k.active = candidate
	k.activeConfig = cfg
	k.mu.Unlock()
	return nil
}

func (k *KnowledgeRuntime) syncActiveConfig(ctx context.Context) error {
	if k.store == nil {
		return nil
	}
	cfg, found, err := k.store.LoadEmbeddingConfig(ctx, true)
	if err != nil || !found {
		return err
	}
	k.mu.RLock()
	same := cfg == k.activeConfig
	k.mu.RUnlock()
	if same {
		return nil
	}
	candidate, err := k.factory(cfg)
	if err != nil {
		return err
	}
	index, ok := candidate.(ports.RebuildableKnowledgeBase)
	if !ok {
		return errors.New("knowledge index does not support configuration refresh")
	}
	needed, err := index.NeedsRebuild(ctx)
	if err != nil {
		return err
	}
	if needed {
		return errors.New("active knowledge configuration changed; waiting for its completed index")
	}
	k.mu.Lock()
	k.active, k.activeConfig = candidate, cfg
	k.mu.Unlock()
	return nil
}

// knowledgeDocumentsPerTick bounds one tick so configuration changes and
// rebuilds are still noticed while a long queue drains.
const knowledgeDocumentsPerTick = 20

func (k *KnowledgeRuntime) processDocument(ctx context.Context) error {
	_, err := k.processNextDocument(ctx)
	return err
}

// processNextDocument runs one queued document job and reports whether one was
// claimed. Document jobs on different replicas run in parallel; the shared
// lock only keeps them out of a running index rebuild.
func (k *KnowledgeRuntime) processNextDocument(ctx context.Context) (bool, error) {
	if k.jobs == nil || k.locks == nil {
		return false, nil
	}
	release, locked, err := k.locks.TryKnowledgeDocumentLock(ctx)
	if err != nil || !locked {
		return false, err
	}
	defer release()
	// Another replica may have activated a model while this worker waited.
	if err := k.syncActiveConfig(ctx); err != nil {
		return false, err
	}
	doc, found, err := k.jobs.ClaimKnowledgeDocument(ctx)
	if err != nil || !found {
		return false, err
	}
	return true, k.indexClaimedDocument(ctx, doc)
}

func (k *KnowledgeRuntime) indexClaimedDocument(ctx context.Context, doc model.KnowledgeDoc) error {
	if doc.Status == "DELETING" {
		objects, ok := k.archive.(ports.ObjectDeleter)
		if !ok {
			return errors.New("knowledge archive does not support deletion")
		}
		if err := objects.DeleteObject(ctx, doc.ObjectBucket, doc.ObjectKey); err != nil {
			return errors.New("knowledge original cleanup will retry")
		}
		return k.repo.DeleteResource(ctx, doc.TenantID, "knowledge", doc.ID)
	}
	// A large document embedded on the bundled CPU service can take far longer
	// than with a cloud API; the lease is renewed as each batch completes, so
	// the deadline only stops a job that stopped making progress.
	jobCtx, cancel := context.WithTimeout(ctx, knowledgeJobTimeout)
	defer cancel()
	if doc.Metadata == nil {
		doc.Metadata = map[string]any{}
	}
	doc.Metadata["indexStage"] = "embedding"
	progressCtx := ports.WithKnowledgeIndexProgress(jobCtx, func(done, total int) {
		doc.Metadata["indexProgress"] = map[string]int{"done": done, "total": total}
		doc.Metadata["indexLeaseUntil"] = time.Now().Add(5 * time.Minute).UnixMilli()
		if updated, err := k.jobs.UpdateKnowledgeDocument(jobCtx, doc); err != nil || !updated {
			cancel()
		}
	})
	reader, indexErr := k.archive.GetObject(jobCtx, doc.ObjectBucket, doc.ObjectKey)
	var result KnowledgeIndexResult
	if indexErr == nil {
		var data []byte
		data, indexErr = io.ReadAll(io.LimitReader(reader, maxKnowledgeDocBytes+1))
		reader.Close()
		if indexErr == nil && len(data) > maxKnowledgeDocBytes {
			indexErr = errors.New("document exceeds 32 MiB")
		}
		if indexErr == nil {
			result, indexErr = IndexKnowledgeDocument(progressCtx, k.current(), doc, data)
		}
	}
	delete(doc.Metadata, "indexLeaseUntil")
	attempts := metadataInt(doc.Metadata["indexAttempts"])
	switch {
	case indexErr != nil && transientIndexError(indexErr) && attempts < len(knowledgeRetryDelays):
		// The vector service was unreachable or rate limited: queue the
		// document again later instead of failing it for a passing outage.
		doc.Status = "UPLOADED"
		doc.Metadata["indexStage"] = "retry_wait"
		doc.Metadata["indexError"] = indexErr.Error()
		doc.Metadata["indexAttempts"] = attempts + 1
		doc.Metadata["indexRetryAt"] = time.Now().Add(knowledgeRetryDelays[attempts]).UnixMilli()
	case indexErr != nil:
		doc.Status = "INDEX_FAILED"
		doc.Metadata["indexStage"] = "failed"
		doc.Metadata["indexError"] = indexErr.Error()
		delete(doc.Metadata, "indexRetryAt")
	default:
		delete(doc.Metadata, "indexAttempts")
		delete(doc.Metadata, "indexRetryAt")
		doc.Status = "INDEXED"
		doc.Metadata["indexStage"] = "ready"
		delete(doc.Metadata, "indexError")
		doc.Metadata["chunks"] = result.Chunks
		doc.Metadata["characters"] = result.Characters
		doc.Metadata["embeddingModel"] = k.EmbeddingModel()
		doc.Metadata["indexProgress"] = map[string]int{"done": result.Chunks, "total": result.Chunks}
	}
	_, err := k.jobs.UpdateKnowledgeDocument(ctx, doc)
	return err
}

// knowledgeJobTimeout bounds one indexing job.
const knowledgeJobTimeout = 2 * time.Hour

// knowledgeRetryDelays spaces automatic re-indexing after transient failures;
// the document fails for good once they are used up.
var knowledgeRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// transientIndexError: an unreachable or rate-limited vector service, or a
// job that ran out of time while the service was slow.
func transientIndexError(err error) bool {
	var content KnowledgeContentError
	if errors.As(err, &content) {
		return false
	}
	return ports.IsTransient(err) || errors.Is(err, context.DeadlineExceeded)
}

func metadataInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func (k *KnowledgeRuntime) Index(ctx context.Context, t, p, id string, b []byte) error {
	return k.current().Index(ctx, t, p, id, b)
}
func (k *KnowledgeRuntime) Search(ctx context.Context, t, q string, n int) ([]string, error) {
	return k.current().Search(ctx, t, q, n)
}
func (k *KnowledgeRuntime) Health(ctx context.Context) error { return k.current().Health(ctx) }
func (k *KnowledgeRuntime) IndexKnowledge(ctx context.Context, in ports.KnowledgeIndexInput) error {
	return k.current().(ports.FilteredKnowledgeBase).IndexKnowledge(ctx, in)
}
func (k *KnowledgeRuntime) IndexKnowledgeBatch(ctx context.Context, in []ports.KnowledgeIndexInput) error {
	return k.current().(ports.BatchKnowledgeBase).IndexKnowledgeBatch(ctx, in)
}
func (k *KnowledgeRuntime) SearchKnowledge(ctx context.Context, in ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) {
	if strings.TrimSpace(in.WorkflowID) == "" {
		return nil, errors.New("knowledge search requires the workflow the documents are bound to")
	}
	return k.current().(ports.FilteredKnowledgeBase).SearchKnowledge(ctx, in)
}
func (k *KnowledgeRuntime) ListKnowledgeChunks(ctx context.Context, t, id string) ([]model.KnowledgeChunk, error) {
	return k.current().(ports.InspectableKnowledgeBase).ListKnowledgeChunks(ctx, t, id)
}
func (k *KnowledgeRuntime) DeleteKnowledgeDocument(ctx context.Context, t, id, w string) error {
	return k.current().(ports.KnowledgeDocumentDeleter).DeleteKnowledgeDocument(ctx, t, id, w)
}
func (k *KnowledgeRuntime) EmbeddingModel() string {
	if index, ok := k.current().(interface{ EmbeddingModel() string }); ok {
		return index.EmbeddingModel()
	}
	return ""
}

// KnowledgeEvidence formats traceable excerpts as untrusted input data.
func KnowledgeEvidence(hits []ports.KnowledgeHit, limit int) string {
	out := aiprompt.KnowledgeEvidenceHeader
	used := 0
	for i, h := range hits {
		text := []rune(h.Content)
		if used >= limit {
			break
		}
		if len(text) > limit-used {
			text = text[:limit-used]
		}
		out += fmt.Sprintf("[%d] documentId=%s chunkId=%s filename=%s position=%d:%d score=%.3f\n%s\n", i+1, h.DocumentID, h.ChunkID, h.Filename, h.StartChar, h.EndChar, h.Score, string(text))
		used += len(text)
	}
	return out + aiprompt.KnowledgeEvidenceFooter
}

// AppendKnowledgeEvidence bounds the complete Harness input, not each piece
// separately. Whole excerpts keep citation markers and UTF-8 text valid.
func AppendKnowledgeEvidence(prompt string, hits []ports.KnowledgeHit, maxBytes int) (string, error) {
	encoded, _ := json.Marshal(prompt)
	available := maxBytes - len(encoded) - 2048
	if available < 256 {
		return "", errors.New("AI 输入过长，无法在上下文预算内附带知识证据")
	}
	low, high := 0, min(8000, available/4)
	for low < high {
		middle := (low + high + 1) / 2
		if validateAIInput(prompt+KnowledgeEvidence(hits, middle), maxBytes-1024, 19500) == nil {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if low == 0 {
		return "", errors.New("AI 输入过长，无法在上下文预算内附带知识证据")
	}
	return prompt + KnowledgeEvidence(hits, low), nil
}

// ErrAIInputTooLarge marks an AI input over the character or JSON budget; the
// caller should narrow the input rather than retry.
var ErrAIInputTooLarge = errors.New("AI 输入超过字符或 JSON 请求预算，请缩小范围后重试")

// ValidateAIInput shares the gateway's UTF-16 character limit and leaves JSON
// wire space for request metadata. Escaped control characters count as bytes.
func ValidateAIInput(prompt string, maxBytes int) error {
	return validateAIInput(prompt, maxBytes, 20000)
}

// WithinAIInputBudget reports whether prompt fits maxBytes of JSON and
// maxUnits UTF-16 characters, for callers that shrink their own material.
func WithinAIInputBudget(prompt string, maxBytes, maxUnits int) bool {
	return validateAIInput(prompt, maxBytes, maxUnits) == nil
}

func validateAIInput(prompt string, maxBytes, maxUnits int) error {
	if !utf8.ValidString(prompt) {
		return errors.New("AI 输入必须为有效 UTF-8")
	}
	units := 0
	for _, r := range prompt {
		units++
		if utf16.IsSurrogate(r) || r > 0xffff {
			units++
		}
	}
	encoded, err := json.Marshal(prompt)
	if err != nil || units > maxUnits || len(encoded) > maxBytes {
		return ErrAIInputTooLarge
	}
	return nil
}
