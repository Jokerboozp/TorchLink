package clickhouse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

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
			_ = json.NewEncoder(w).Encode(map[string]int32{"total": telemetryRows.Load()})
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
			_ = json.NewEncoder(w).Encode(map[string]int{"total": 0})
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
