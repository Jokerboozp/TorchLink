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

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type transientTestError struct{}

func (transientTestError) Error() string   { return "embedding service returned HTTP 429" }
func (transientTestError) Transient() bool { return true }

// flakyIndex fails indexing with err until failures run out.
type flakyIndex struct {
	*knowledge.Local
	mu       sync.Mutex
	failures int
	err      error
}

func (f *flakyIndex) IndexKnowledge(ctx context.Context, in ports.KnowledgeIndexInput) error {
	f.mu.Lock()
	if f.failures > 0 {
		f.failures--
		f.mu.Unlock()
		return f.err
	}
	f.mu.Unlock()
	return f.Local.IndexKnowledge(ctx, in)
}

// oneDocJobs mirrors the PostgreSQL claim: a waiting document is claimed only
// once its retry time has passed.
type oneDocJobs struct {
	mu  sync.Mutex
	doc model.KnowledgeDoc
}

func (j *oneDocJobs) ClaimKnowledgeDocument(context.Context) (model.KnowledgeDoc, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.doc.Status != "UPLOADED" || int64(metadataInt(j.doc.Metadata["indexRetryAt"])) > time.Now().UnixMilli() {
		return model.KnowledgeDoc{}, false, nil
	}
	j.doc.Status = "INDEXING"
	return j.doc, true, nil
}

func (j *oneDocJobs) UpdateKnowledgeDocument(_ context.Context, doc model.KnowledgeDoc) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	metadata := map[string]any{}
	for k, v := range doc.Metadata {
		metadata[k] = v
	}
	doc.Metadata = metadata
	j.doc = doc
	return true, nil
}

func (j *oneDocJobs) waitOver() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.doc.Metadata["indexRetryAt"] = time.Now().UnixMilli() - 1
}

func TestKnowledgeIndexingRetriesTransientFailuresBeforeFailing(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		failures int
		err      error
		want     string
		passes   int
	}{
		{"recovers after an outage", 2, transientTestError{}, "INDEXED", 3},
		{"fails once retries are used up", 99, transientTestError{}, "INDEX_FAILED", len(knowledgeRetryDelays) + 1},
		{"fails at once on a permanent error", 99, errors.New("embedding service returned HTTP 401"), "INDEX_FAILED", 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			archive, err := local.NewArchive(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			body := []byte("烟感离线时先检查电源，再复位。")
			if _, err = archive.PutObject(ctx, "docs", "sop.txt", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
				t.Fatal(err)
			}
			index := &flakyIndex{Local: knowledge.NewLocal(), failures: scenario.failures, err: scenario.err}
			factory := func(ports.EmbeddingConfig) (ports.KnowledgeBase, error) { return index, nil }
			jobs := &oneDocJobs{doc: model.KnowledgeDoc{ID: "d1", TenantID: "t1", WorkflowID: "ops-assistant", ObjectBucket: "docs", ObjectKey: "sop.txt", Filename: "sop.txt", Status: "UPLOADED", Metadata: map[string]any{}}}
			runtime, err := NewKnowledgeRuntime(ports.EmbeddingConfig{}, ports.EmbeddingConfig{}, factory, nil, memory.NewRepository(), memory.NewRepository(), jobs, archive, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			for pass := 1; pass <= scenario.passes; pass++ {
				if err = runtime.processDocument(ctx); err != nil {
					t.Fatal(err)
				}
				if pass < scenario.passes {
					if jobs.doc.Status != "UPLOADED" || jobs.doc.Metadata["indexStage"] != "retry_wait" {
						t.Fatalf("pass %d: transient failure not queued for retry: %+v", pass, jobs.doc)
					}
					// Not claimed again before its retry time.
					if err = runtime.processDocument(ctx); err != nil || jobs.doc.Status != "UPLOADED" {
						t.Fatalf("retried before its time: %+v %v", jobs.doc, err)
					}
					jobs.waitOver()
				}
			}
			if jobs.doc.Status != scenario.want {
				t.Fatalf("status %s, want %s: %+v", jobs.doc.Status, scenario.want, jobs.doc.Metadata)
			}
			if _, waiting := jobs.doc.Metadata["indexRetryAt"]; waiting {
				t.Fatal("finished document still scheduled for retry")
			}
			if scenario.want == "INDEX_FAILED" && !strings.Contains(jobs.doc.Metadata["indexError"].(string), "HTTP") {
				t.Fatalf("failure reason lost: %+v", jobs.doc.Metadata)
			}
		})
	}
}

// processDocument runs one queued document job as a tick would after the
// first one: with the configuration resync.
func (k *KnowledgeRuntime) processDocument(ctx context.Context) error {
	_, err := k.processNextDocument(ctx, true)
	return err
}
