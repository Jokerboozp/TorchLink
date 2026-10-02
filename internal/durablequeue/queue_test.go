package durablequeue

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscardMatchingReclaimsActiveAndRejectedCapacityAndPreservesUnknown(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "unknown.json.corrupt"), []byte("unreadable evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	q, err := OpenQueue(root, 1<<20, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	first := model.RawMessage{TenantID: "t", MessageID: "first", DeviceID: "fixture", Payload: json.RawMessage(`1`)}
	rejected := first
	rejected.MessageID = "rejected"
	kept := first
	kept.MessageID = "kept"
	kept.DeviceID = "current"
	for _, raw := range []model.RawMessage{first, rejected, kept} {
		if err := q.Put(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Reject(rejected); err != nil {
		t.Fatal(err)
	}
	before := q.used
	if err := q.Put(model.RawMessage{TenantID: "t", MessageID: "replacement"}); !errors.Is(err, ErrQueueFull) {
		t.Fatal("expected full queue", err)
	}
	removed, skipped, err := q.DiscardMatching(context.Background(), func(raw model.RawMessage) bool { return raw.TenantID == "t" && raw.DeviceID == "fixture" })
	if err != nil || removed != 2 || skipped != 2 || q.items != 2 {
		t.Fatalf("discard accounting: removed=%d skipped=%d items=%d err=%v", removed, skipped, q.items, err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(rejected)
	if q.used != before-int64(len(a)+len(b)) {
		t.Fatal("used byte capacity was not reclaimed")
	}
	if _, err := os.Stat(filepath.Join(root, "unknown.json.corrupt")); err != nil {
		t.Fatal("unknown evidence removed", err)
	}
	if err := q.Put(model.RawMessage{TenantID: "t", MessageID: "replacement"}); err != nil {
		t.Fatal("runtime capacity still full", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if removed, _, err := q.DiscardMatching(ctx, func(model.RawMessage) bool { return true }); !errors.Is(err, context.Canceled) || removed != 0 {
		t.Fatal("cancelled discard mutated queue", removed, err)
	}
	if removed, _, err := q.DiscardMatching(context.Background(), func(raw model.RawMessage) bool { return raw.DeviceID == "fixture" }); err != nil || removed != 0 {
		t.Fatal("discard retry double counted", removed, err)
	}
	q.Close()
	reopened, err := OpenQueue(root, 1<<20, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.items != q.items || reopened.used != q.used {
		t.Fatal("discard accounting differs after restart")
	}
}

func TestQueueRestartRetryAndCapacity(t *testing.T) {
	root := t.TempDir()
	q, err := OpenQueue(root, 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := OpenQueue(root, 1<<20, 1); err == nil {
		other.Close()
		t.Fatal("two clients opened one queue")
	}
	raw := model.RawMessage{TenantID: "tenant", MessageID: "first", Payload: json.RawMessage(`{"x":1}`)}
	if err = q.Put(raw); err != nil {
		t.Fatal(err)
	}
	if err = q.Put(raw); err != nil || q.Depth() != 1 {
		t.Fatal("duplicate queued", err)
	}
	conflict := raw
	conflict.Payload = json.RawMessage(`{"x":2}`)
	if err = q.Put(conflict); !errors.Is(err, model.ErrRawConflict) {
		t.Fatal("conflicting raw overwritten", err)
	}
	next := raw
	next.MessageID = "second"
	if err = q.Put(next); !errors.Is(err, ErrQueueFull) {
		t.Fatal("capacity silently exceeded", err)
	}
	if err = q.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := OpenQueue(root, 1<<20, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	saved, ok, err := recovered.Next()
	if err != nil || !ok || saved.MessageID != raw.MessageID {
		t.Fatal("restart lost pending message", err)
	}
	// Simulated response loss leaves the same message available for retry.
	repeated, ok, err := recovered.Next()
	if err != nil || !ok || repeated.MessageID != saved.MessageID {
		t.Fatal("unacknowledged data removed", err)
	}
	if err = recovered.Ack(saved); err != nil || recovered.Depth() != 0 {
		t.Fatal("acknowledged data retained", err)
	}
	if err = recovered.Put(next); err != nil {
		t.Fatal("capacity not reclaimed", err)
	}
}

func TestRejectedDataIsRetainedWithoutBlockingFollowingMessages(t *testing.T) {
	q, err := OpenQueue(t.TempDir(), 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	first := model.RawMessage{TenantID: "tenant", MessageID: "rejected", Payload: json.RawMessage(`1`)}
	next := model.RawMessage{TenantID: "tenant", MessageID: "next", Payload: json.RawMessage(`2`)}
	if err = q.Put(first); err != nil {
		t.Fatal(err)
	}
	if err = q.Reject(first); err != nil {
		t.Fatal(err)
	}
	if q.Depth() != 0 || q.Rejected() != 1 {
		t.Fatal("rejected entry lost or remained active")
	}
	if err = q.Put(first); !errors.Is(err, ErrQuarantined) {
		t.Fatal("rejected duplicate silently accepted", err)
	}
	if err = q.Put(next); err != nil {
		t.Fatal(err)
	}
	raw, ok, err := q.Next()
	if err != nil || !ok || raw.MessageID != next.MessageID {
		t.Fatal("rejected entry blocked remaining queue", err)
	}
	if err = q.RetryRejected(); err != nil || q.Depth() != 2 || q.Rejected() != 0 {
		t.Fatal("explicit retry lost entries", err)
	}
}

func TestCorruptQueueEntryDoesNotBlockValidData(t *testing.T) {
	for _, body := range []string{`{broken`, `{"tenantId":"foreign","messageId":"other"}`} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			q, err := OpenQueue(root, 1<<20, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer q.Close()
			raw := model.RawMessage{TenantID: "tenant", MessageID: "first", Payload: json.RawMessage(`1`)}
			if err := q.Put(raw); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, queueName(raw))
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := q.Next(); err == nil || ok {
				t.Fatal("corruption not reported")
			}
			data, err := os.ReadFile(path + ".corrupt")
			if err != nil || string(data) != body {
				t.Fatal("original corruption lost", err)
			}
			if q.Depth() != 0 || q.Corrupt() != 1 {
				t.Fatal("quarantine not observable")
			}
			if err := q.RetryRejected(); err != nil || q.Depth() != 0 {
				t.Fatal("corrupt data requeued", err)
			}
			if err := q.Put(raw); !errors.Is(err, ErrQuarantined) {
				t.Fatal("corrupt identity overwritten", err)
			}
			good := raw
			good.MessageID = "second"
			if err := q.Put(good); err != nil {
				t.Fatal(err)
			}
			third := raw
			third.MessageID = "third"
			if err := q.Put(third); !errors.Is(err, ErrQueueFull) {
				t.Fatal("quarantine not counted in limit", err)
			}
			q.Close()
			q, err = OpenQueue(root, 1<<20, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer q.Close()
			actual, ok, err := q.Next()
			if err != nil || !ok || actual.MessageID != good.MessageID {
				t.Fatal("valid data blocked after restart", err)
			}
			if err := q.Ack(actual); err != nil {
				t.Fatal(err)
			}
			if q.Corrupt() != 1 {
				t.Fatal("ack removed corrupt evidence")
			}
		})
	}
}
