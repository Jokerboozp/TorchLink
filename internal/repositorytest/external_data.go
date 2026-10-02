package repositorytest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/externaldata"
)

// ExternalData runs the same tenant isolation, pagination and worker fencing
// scenarios against memory and PostgreSQL implementations.
func ExternalData(t *testing.T, store externaldata.Store) {
	t.Helper()
	ctx := context.Background()
	put := func(e externaldata.Entry) externaldata.Entry {
		t.Helper()
		if e.Body == nil {
			e.Body = json.RawMessage(`{"value":1}`)
		}
		got, err := store.Put(ctx, e, 0)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Run("tenant isolation and CAS", func(t *testing.T) {
		e := put(externaldata.Entry{TenantID: "tenant-one", Kind: "source", ID: "shared", CreatedAt: 1, UpdatedAt: 1, Revision: 100})
		other := put(externaldata.Entry{TenantID: "tenant-two", Kind: "source", ID: "shared"})
		if e.Revision != 1 || e.CreatedAt <= 1 || e.UpdatedAt != e.CreatedAt {
			t.Fatalf("invalid created metadata: %+v", e)
		}
		if _, err := store.Put(ctx, e, 0); !errors.Is(err, externaldata.ErrConflict) {
			t.Fatalf("duplicate create: %v", err)
		}
		if _, err := store.Get(ctx, "missing", "source", "shared"); !errors.Is(err, externaldata.ErrNotFound) {
			t.Fatalf("tenant leak: %v", err)
		}
		if _, err := store.Get(ctx, "", "source", "shared"); !errors.Is(err, externaldata.ErrInvalid) {
			t.Fatalf("empty tenant: %v", err)
		}
		if _, _, err := store.List(ctx, externaldata.Query{}); !errors.Is(err, externaldata.ErrInvalid) {
			t.Fatalf("unscoped list: %v", err)
		}
		if err := store.Delete(ctx, "", "source", "shared", 1); !errors.Is(err, externaldata.ErrInvalid) {
			t.Fatalf("unscoped delete: %v", err)
		}
		e.Body = json.RawMessage(`{"value":2}`)
		updated, err := store.Put(ctx, e, e.Revision)
		if err != nil || updated.Revision != 2 || updated.CreatedAt != e.CreatedAt || updated.UpdatedAt < e.UpdatedAt {
			t.Fatalf("update: %+v %v", updated, err)
		}
		if _, err := store.Put(ctx, e, e.Revision); !errors.Is(err, externaldata.ErrConflict) {
			t.Fatalf("stale writer: %v", err)
		}
		if err := store.Delete(ctx, e.TenantID, e.Kind, e.ID, e.Revision); !errors.Is(err, externaldata.ErrConflict) {
			t.Fatalf("stale delete: %v", err)
		}
		if err := store.Delete(ctx, e.TenantID, e.Kind, e.ID, updated.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Get(ctx, other.TenantID, other.Kind, other.ID); err != nil {
			t.Fatalf("other tenant modified: %v", err)
		}
		if err := store.Delete(ctx, e.TenantID, e.Kind, e.ID, updated.Revision); !errors.Is(err, externaldata.ErrNotFound) {
			t.Fatalf("missing delete: %v", err)
		}
	})
	t.Run("real pagination and filters", func(t *testing.T) {
		for i := 0; i < 107; i++ {
			put(externaldata.Entry{TenantID: "pages", Kind: "record", ID: fmt.Sprintf("%03d", i), SourceID: "source-a", EndpointID: "endpoint-a", Status: "SUCCESS"})
		}
		put(externaldata.Entry{TenantID: "pages", Kind: "record", ID: "other-endpoint", SourceID: "source-a", EndpointID: "endpoint-b", Status: "RETRY"})
		put(externaldata.Entry{TenantID: "other-pages", Kind: "record", ID: "hidden", SourceID: "source-a", EndpointID: "endpoint-a", Status: "SUCCESS"})
		q := externaldata.Query{TenantID: "pages", Kind: "record", SourceID: "source-a", EndpointID: "endpoint-a", Status: "SUCCESS", Limit: 1000}
		first, total, err := store.List(ctx, q)
		if err != nil || total != 107 || len(first) != 100 {
			t.Fatalf("first page: %d/%d %v", len(first), total, err)
		}
		q.Offset = 100
		last, total, err := store.List(ctx, q)
		if err != nil || total != 107 || len(last) != 7 {
			t.Fatalf("last page: %d/%d %v", len(last), total, err)
		}
		ids := map[string]bool{}
		for _, e := range append(first, last...) {
			if ids[e.ID] {
				t.Fatalf("duplicate page ID %s", e.ID)
			}
			ids[e.ID] = true
		}
		q.Offset = 999
		empty, total, err := store.List(ctx, q)
		if err != nil || total != 107 || len(empty) != 0 {
			t.Fatalf("empty page: %d/%d %v", len(empty), total, err)
		}
	})
	t.Run("job membership and status sets", func(t *testing.T) {
		jobID := "job-'exact%"
		for i, status := range []string{"PROCESSED", "DUPLICATE", "IGNORED", "FILTERED", "FAILED", "WAITING_BINDING", "CONFLICT", "PENDING", "RETRY", "RUNNING"} {
			b, _ := json.Marshal(externaldata.Record{JobID: jobID, Error: "fixture error"})
			put(externaldata.Entry{TenantID: "summary", Kind: "record", ID: fmt.Sprintf("record-%02d", i), SourceID: "source", EndpointID: "endpoint", Status: status, Body: b})
		}
		for i, b := range []string{`{"jobId":"job-'exact%-other"}`, `{"jobId":"different"}`, `{"nested":{"jobId":"job-'exact%"}}`, `{}`} {
			put(externaldata.Entry{TenantID: "summary", Kind: "record", ID: fmt.Sprintf("unrelated-%02d", i), SourceID: "source", EndpointID: "endpoint", Status: "FAILED", Body: json.RawMessage(b)})
		}
		b, _ := json.Marshal(externaldata.Record{JobID: jobID})
		put(externaldata.Entry{TenantID: "another-summary", Kind: "record", ID: "other-tenant", SourceID: "source", EndpointID: "endpoint", Status: "FAILED", Body: b})
		q := externaldata.Query{TenantID: "summary", Kind: "record", SourceID: "source", EndpointID: "endpoint", JobID: jobID, Limit: 1}
		for _, group := range []struct {
			statuses string
			total    int
		}{{"", 10}, {"PROCESSED,DUPLICATE,IGNORED,FILTERED", 4}, {"FAILED,WAITING_BINDING,CONFLICT", 3}, {"PENDING,RETRY,RUNNING", 3}, {"FAILED,FAILED,CONFLICT", 2}} {
			q.Status = group.statuses
			rows, total, err := store.List(ctx, q)
			if err != nil || total != group.total || len(rows) != 1 {
				t.Fatalf("statuses %s: rows=%d total=%d err=%v", group.statuses, len(rows), total, err)
			}
			q.Offset = group.total
			rows, total, err = store.List(ctx, q)
			if err != nil || total != group.total || len(rows) != 0 {
				t.Fatalf("status pagination %s: rows=%d total=%d err=%v", group.statuses, len(rows), total, err)
			}
			q.Offset = 0
		}
		// Exercise the exact same summary path used by the API, including JSON
		// job membership in counts rather than all records of an endpoint.
		b, _ = json.Marshal(externaldata.Job{Received: 10})
		put(externaldata.Entry{TenantID: "summary", Kind: "job", ID: jobID, SourceID: "source", EndpointID: "endpoint", Status: "COMPLETED", Body: b})
		service, err := externaldata.New(store, "summary-test-key", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		rows, total, err := service.List(ctx, externaldata.Query{TenantID: "summary", Kind: "job"})
		if err != nil || total != 1 || len(rows) != 1 {
			t.Fatalf("job summary list: %d %d %v", len(rows), total, err)
		}
		entry := rows[0].(externaldata.Entry)
		var job externaldata.Job
		if err = json.Unmarshal(entry.Body, &job); err != nil {
			t.Fatal(err)
		}
		if job.Processed != 4 || job.Failed != 3 || job.Pending != 3 || job.Received != 10 {
			t.Fatalf("job summary included unrelated records or omitted statuses: %+v", job)
		}
	})
	t.Run("latest update order and runtime summary", func(t *testing.T) {
		// This clock-controlled Claim updates the older row after the newer
		// row's creation, without relying on wall-clock sleeps.
		old := put(externaldata.Entry{TenantID: "updated-order", Kind: "record", ID: "z-old", SourceID: "source", EndpointID: "endpoint", Status: "PENDING"})
		newer := put(externaldata.Entry{TenantID: "updated-order", Kind: "record", ID: "a-new", SourceID: "source", EndpointID: "endpoint", Status: "PROCESSED"})
		now := max(old.UpdatedAt, newer.UpdatedAt) + 1000
		claimed, err := store.Claim(ctx, "record", "ordering-worker", now, 60000)
		// Other subtests may leave pending "record" entries. If one is claimed
		// first, keep leasing them until the target becomes the oldest ready row.
		for attempt := 0; err == nil && claimed.ID != old.ID && attempt < 20; attempt++ {
			claimed, err = store.Claim(ctx, "record", "ordering-worker", now, 60000)
		}
		if err != nil || claimed.ID != old.ID {
			t.Fatalf("order claim: %+v %v", claimed, err)
		}
		q := externaldata.Query{TenantID: "updated-order", Kind: "record", Limit: 1}
		rows, total, err := store.List(ctx, q)
		if err != nil || total != 2 || rows[0].ID != newer.ID {
			t.Fatalf("created order: %+v %d %v", rows, total, err)
		}
		q.UpdatedOrder = true
		rows, total, err = store.List(ctx, q)
		if err != nil || total != 2 || rows[0].ID != old.ID || rows[0].UpdatedAt != now {
			t.Fatalf("updated order: %+v %d %v", rows, total, err)
		}
		q.Offset = 1
		rows, total, err = store.List(ctx, q)
		if err != nil || total != 2 || rows[0].ID != newer.ID {
			t.Fatalf("updated second page: %+v %d %v", rows, total, err)
		}
		body, _ := json.Marshal(externaldata.Source{ID: "source", Name: "summary source"})
		put(externaldata.Entry{TenantID: "updated-order", Kind: "source", ID: "source", Body: body})
		failedBody, _ := json.Marshal(externaldata.Record{Error: "关联失败"})
		failed := put(externaldata.Entry{TenantID: "updated-order", Kind: "record", ID: "failed", SourceID: "source", EndpointID: "endpoint", Status: "WAITING_BINDING", Body: failedBody})
		oldReceipt := put(externaldata.Entry{TenantID: "updated-order", Kind: "receipt", ID: "old-receipt", SourceID: "source", EndpointID: "endpoint", Status: "PENDING"})
		// Reception time must not move backwards when an old durable receipt
		// is retried after a newer callback has already arrived.
		time.Sleep(2 * time.Millisecond)
		receipt := put(externaldata.Entry{TenantID: "updated-order", Kind: "receipt", ID: "new-receipt", SourceID: "source", EndpointID: "endpoint", Status: "RECEIVED"})
		if claimed, err := store.Claim(ctx, "receipt", "recovery-worker", receipt.CreatedAt+1000, 60000); err != nil || claimed.ID != oldReceipt.ID {
			t.Fatalf("receipt retry fixture: %+v %v", claimed, err)
		}
		pull := put(externaldata.Entry{TenantID: "updated-order", Kind: "job", ID: "pull", SourceID: "source", EndpointID: "endpoint", Status: "COMPLETED"})
		service, err := externaldata.New(store, "summary-test-key", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		items, total, err := service.List(ctx, externaldata.Query{TenantID: "updated-order", Kind: "source"})
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("source summary: %d %d %v", len(items), total, err)
		}
		source := items[0].(externaldata.Source)
		v := source.Runtime
		if v == nil || v.PendingRecords != 1 || v.FailedRecords != 1 || v.LastReceivedAt != receipt.CreatedAt || v.LastProcessedAt != newer.UpdatedAt || v.LastPullAt != pull.UpdatedAt || v.LastError != "关联失败" || v.LastErrorAt != failed.UpdatedAt {
			t.Fatalf("runtime summary: %+v", v)
		}
	})
	t.Run("concurrent create and claim", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 24)
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := store.Put(ctx, externaldata.Entry{TenantID: "workers", Kind: "job", ID: "only", Status: "PENDING", Body: json.RawMessage(`{}`)}, 0)
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		created := 0
		for err := range results {
			if err == nil {
				created++
			} else if !errors.Is(err, externaldata.ErrConflict) {
				t.Fatal(err)
			}
		}
		if created != 1 {
			t.Fatalf("created %d times", created)
		}
		claimed := make(chan externaldata.Entry, 24)
		errs := make(chan error, 24)
		now := time.Now().UnixMilli()
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				e, err := store.Claim(ctx, "job", fmt.Sprintf("worker-%d", i), now, 60000)
				if err == nil {
					claimed <- e
				} else {
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(claimed)
		close(errs)
		for err := range errs {
			if !errors.Is(err, externaldata.ErrNotFound) {
				t.Fatal(err)
			}
		}
		if len(claimed) != 1 {
			t.Fatalf("claimed %d times", len(claimed))
		}
		e := <-claimed
		if e.Revision != 2 || e.Status != "RUNNING" || e.Owner == "" || e.LeaseUntil != now+60000 {
			t.Fatalf("invalid claim: %+v", e)
		}
		e.Status = "SUCCESS"
		if _, err := store.Put(ctx, e, e.Revision); err != nil {
			t.Fatalf("complete current lease: %v", err)
		}
		if _, err := store.Claim(ctx, "job", "worker", now+120000, 1000); !errors.Is(err, externaldata.ErrNotFound) {
			t.Fatalf("completed reclaimed: %v", err)
		}
	})
	t.Run("expired lease and retry fencing", func(t *testing.T) {
		now := time.Now().UnixMilli()
		put(externaldata.Entry{TenantID: "leases", Kind: "lease", ID: "future", Status: "PENDING", DueAt: now + 300000})
		put(externaldata.Entry{TenantID: "leases", Kind: "unrelated", ID: "ignore", Status: "PENDING"})
		e := put(externaldata.Entry{TenantID: "leases", Kind: "lease", ID: "expired", Status: "RUNNING", Owner: "old", LeaseUntil: now - 1})
		e.Status = "SUCCESS"
		if _, err := store.Put(ctx, e, e.Revision); !errors.Is(err, externaldata.ErrConflict) {
			t.Fatalf("expired lease committed: %v", err)
		}
		claimed, err := store.Claim(ctx, "lease", "new", now, 60000)
		if err != nil || claimed.ID != "expired" || claimed.Revision != e.Revision+1 || claimed.Owner != "new" {
			t.Fatalf("expired reclaim: %+v %v", claimed, err)
		}
		if _, err := store.Put(ctx, e, e.Revision); !errors.Is(err, externaldata.ErrConflict) {
			t.Fatalf("old owner committed: %v", err)
		}
		claimed.Status = "RETRY"
		claimed.DueAt = now + 120000
		if _, err := store.Put(ctx, claimed, claimed.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Claim(ctx, "lease", "new", now, 60000); !errors.Is(err, externaldata.ErrNotFound) {
			t.Fatalf("claimed before due: %v", err)
		}
		if got, err := store.Claim(ctx, "lease", "next", now+120000, 60000); err != nil || got.ID != "expired" {
			t.Fatalf("due retry: %+v %v", got, err)
		}
	})
}
