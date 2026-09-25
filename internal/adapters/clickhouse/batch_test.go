package clickhouse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

// Concurrent raw message writes share INSERT requests, every row arrives and
// each caller returns only after its row was written.
func TestConcurrentRawMessagesShareInserts(t *testing.T) {
	var mu sync.Mutex
	var inserts int
	rows := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("query"), "INSERT INTO iot_raw_message") {
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		inserts++
		for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
			_, id, _ := strings.Cut(string(line), `"message_id":"`)
			id, _, _ = strings.Cut(id, `"`)
			rows[id] = true
		}
	}))
	defer server.Close()
	repo, err := New(context.Background(), server.URL, memory.NewRepository())
	if err != nil {
		t.Fatal(err)
	}

	const total = 200
	var wg sync.WaitGroup
	errs := make(chan error, total)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("raw-%d", i)
			if err := repo.SaveRawMessage(context.Background(), model.RawMessage{TenantID: "t1", MessageID: id, Payload: []byte(`{"v":1}`)}); err != nil {
				errs <- err
				return
			}
			mu.Lock()
			written := rows[id]
			mu.Unlock()
			if !written {
				errs <- fmt.Errorf("%s returned before its row was written", id)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(rows) != total {
		t.Fatalf("rows written = %d, want %d", len(rows), total)
	}
	if inserts >= total/2 {
		t.Fatalf("concurrent writes were not batched: %d INSERT requests for %d rows", inserts, total)
	}
}

// A failed INSERT is reported to every caller in that batch, so each message
// is retried instead of being acknowledged.
func TestBatchInsertFailureReachesEveryCaller(t *testing.T) {
	var calls atomic.Int32
	b := newInsertBatcher(func(context.Context, []byte) error {
		calls.Add(1)
		return errors.New("clickhouse unavailable")
	})
	var wg sync.WaitGroup
	failures := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failures <- b.add(context.Background(), []byte("{}\n"))
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err == nil {
			t.Fatal("a caller was acknowledged although its batch failed")
		}
	}
	if n := calls.Load(); n == 0 || n > 5 {
		t.Fatalf("unexpected INSERT count %d", n)
	}
}

// A full batch is written at once instead of waiting for the timer.
func TestFullBatchFlushesImmediately(t *testing.T) {
	var got [][]byte
	var mu sync.Mutex
	b := newInsertBatcher(func(_ context.Context, body []byte) error {
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
		return nil
	})
	big := bytes.Repeat([]byte("x"), batchMaxBytes)
	done := make(chan error, 1)
	go func() { done <- b.add(context.Background(), big) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("full batch was not flushed")
	}
	if len(got) != 1 || len(got[0]) != batchMaxBytes {
		t.Fatalf("unexpected batches: %d", len(got))
	}
}
