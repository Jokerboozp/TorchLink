package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestHealthChecksClickHouseAfterInitialization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	repo, err := New(context.Background(), server.URL, memory.NewRepository())
	if err != nil {
		t.Fatal(err)
	}
	server.Close()

	if err := repo.Health(context.Background()); err == nil {
		t.Fatal("ClickHouse is unreachable but repository health still reports success")
	}
}

func TestClaimRetriesMissingTelemetryAfterTransientInsertFailure(t *testing.T) {
	var telemetryAttempts atomic.Int32
	server := newClickHouseTestServer(t, func(query string, w http.ResponseWriter) bool {
		if strings.Contains(query, "INSERT INTO iot_telemetry") && telemetryAttempts.Add(1) == 1 {
			http.Error(w, "temporary outage", http.StatusServiceUnavailable)
			return true
		}
		return false
	})
	defer server.Close()

	repo, err := New(context.Background(), server.URL, memory.NewRepository())
	if err != nil {
		t.Fatal(err)
	}
	message := testTelemetryMessage()
	if _, _, err = repo.ClaimStandardMessage(context.Background(), message); err == nil {
		t.Fatal("first ClickHouse insert unexpectedly succeeded")
	}
	shouldProcess, _, err := repo.ClaimStandardMessage(context.Background(), message)
	if err != nil || !shouldProcess {
		t.Fatalf("retry claim failed: shouldProcess=%v err=%v", shouldProcess, err)
	}
	if got := telemetryAttempts.Load(); got != 2 {
		t.Fatalf("transient ClickHouse insert was not retried: attempts=%d want=2", got)
	}
}

func TestClaimDoesNotDuplicateExistingTelemetryDuringBusinessRetry(t *testing.T) {
	var telemetryRows atomic.Int32
	server := newClickHouseTestServer(t, func(query string, w http.ResponseWriter) bool {
		switch {
		case strings.Contains(query, "INSERT INTO iot_telemetry"):
			telemetryRows.Add(1)
		case strings.Contains(query, "SELECT count() AS total FROM iot_telemetry"):
			_ = json.NewEncoder(w).Encode(map[string]string{"total": fmt.Sprint(telemetryRows.Load())})
			return true
		}
		return false
	})
	defer server.Close()

	repo, err := New(context.Background(), server.URL, memory.NewRepository())
	if err != nil {
		t.Fatal(err)
	}
	message := testTelemetryMessage()
	if shouldProcess, _, claimErr := repo.ClaimStandardMessage(context.Background(), message); claimErr != nil || !shouldProcess {
		t.Fatalf("initial claim failed: shouldProcess=%v err=%v", shouldProcess, claimErr)
	}
	if shouldProcess, _, claimErr := repo.ClaimStandardMessage(context.Background(), message); claimErr != nil || !shouldProcess {
		t.Fatalf("business retry claim failed: shouldProcess=%v err=%v", shouldProcess, claimErr)
	}
	if got := telemetryRows.Load(); got != 1 {
		t.Fatalf("business retry duplicated telemetry: rows=%d want=1", got)
	}
}

func newClickHouseTestServer(t *testing.T, handle func(string, http.ResponseWriter) bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if handle(query, w) {
			return
		}
		if strings.Contains(query, "SELECT count() AS total FROM iot_telemetry") {
			_ = json.NewEncoder(w).Encode(map[string]string{"total": "0"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}

func testTelemetryMessage() model.StandardMessage {
	return model.StandardMessage{
		TenantID: "tenant-test", MessageID: "message-test", DeviceID: "device-test",
		MessageType: model.PropertyReport, Properties: map[string]any{"temperature": 25.0},
	}
}

// Raw and telemetry lookups by message ID include the device, so ClickHouse
// reads one device's range of the sort key instead of the whole tenant.
func TestMessageLookupsNarrowToDevice(t *testing.T) {
	var queries []string
	server := newClickHouseTestServer(t, func(query string, w http.ResponseWriter) bool {
		if strings.Contains(query, "FROM iot_raw_message") || strings.Contains(query, "FROM iot_telemetry") {
			queries = append(queries, query)
			if strings.Contains(query, "count()") {
				_ = json.NewEncoder(w).Encode(map[string]int{"total": 1})
			} else {
				body, _ := json.Marshal(model.RawMessage{TenantID: "tenant-a", MessageID: "raw-1", DeviceID: "device-a"})
				_ = json.NewEncoder(w).Encode(map[string]string{"body": string(body)})
			}
			return true
		}
		return false
	})
	defer server.Close()
	repo, err := New(context.Background(), server.URL, memory.NewRepository())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetDeviceRawMessage(context.Background(), "tenant-a", "device-a", "raw-1"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.telemetryExists(context.Background(), "tenant-a", "device-a", "message-1"); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Fatalf("queries: %v", queries)
	}
	for _, q := range queries {
		if !strings.Contains(q, "device_id='device-a'") {
			t.Fatalf("lookup does not use the device prefix: %s", q)
		}
	}
}

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

func TestPropertyHistoryPageDecodesQuotedTotal(t *testing.T) {
	server := newClickHouseTestServer(t, func(query string, w http.ResponseWriter) bool {
		if strings.Contains(query, "SELECT count()") {
			fmt.Fprintln(w, `{"total":"42"}`)
			return true
		}
		if strings.Contains(query, "SELECT toUnixTimestamp64Milli") {
			fmt.Fprintln(w, `{"timestamp":"1700000000000","value":25,"messageId":"m"}`)
			return true
		}
		return false
	})
	defer server.Close()
	repo, err := New(context.Background(), server.URL, memory.NewRepository())
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := repo.PropertyHistoryPage(context.Background(), "tenant-test", "device-test", "temperature", 1, 2, 20, 0)
	if err != nil || total != 42 || len(rows) != 1 {
		t.Fatalf("ClickHouse count lost: total=%d rows=%d err=%v", total, len(rows), err)
	}
}

func TestDecodeCountContracts(t *testing.T) {
	for _, input := range []string{`{"total":42}`, `{"total":"42"}`} {
		if total, err := decodeCount([]byte(input)); err != nil || total != 42 {
			t.Fatalf("%s: %d %v", input, total, err)
		}
	}
	for _, input := range []string{`{}`, `{"total":null}`, `{"total":"bad"}`, `{"total":-1}`, `{"total":1.5}`, `{"total":"18446744073709551615"}`} {
		if _, err := decodeCount([]byte(input)); err == nil {
			t.Fatalf("accepted invalid count %s", input)
		}
	}
}
