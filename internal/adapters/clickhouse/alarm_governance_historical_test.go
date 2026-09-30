package clickhouse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iot-platform/internal/ports"
)

func TestGovernanceHistoricalArchiveUsesBoundedTenantScopeAndUnresolvedMetadata(t *testing.T) {
	queries := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query().Get("query")
		for _, id := range []string{"m1", "m2"} {
			_ = json.NewEncoder(w).Encode(map[string]any{"device_id": "a", "message_id": id, "product_id": "p", "timestamp": 1000, "values": []string{`{"temperature":27}`}})
		}
	}))
	defer server.Close()
	repo := &Repository{base: server.URL, http: server.Client()}
	page, err := repo.ListGovernanceHistoricalArchiveMessages(context.Background(), "tenant", ports.AlarmObservationFilter{DeviceIDs: []string{"a"}, Start: 1000, End: 2000, Limit: 1})
	if err != nil || len(page.Items) != 1 || !page.HasMore || page.Cursor == "" || page.Complete || page.Source.Complete || page.Items[0].MessageType != "" {
		t.Fatal(page, err)
	}
	query := <-queries
	if !strings.Contains(query, "tenant_id='tenant'") || !strings.Contains(query, "device_id IN ('a')") || !strings.Contains(query, "ts<fromUnixTimestamp64Milli(2000)") || !strings.Contains(query, "LIMIT 2") {
		t.Fatal("unbounded archive query", query)
	}
	if _, err := repo.ListGovernanceHistoricalArchiveMessages(context.Background(), "tenant", ports.AlarmObservationFilter{Start: 1000, End: 2000, Limit: 1}); err == nil {
		t.Fatal("empty scope queried archive")
	}
	if _, err := repo.ListGovernanceHistoricalArchiveMessages(context.Background(), "tenant", ports.AlarmObservationFilter{DeviceIDs: []string{"a"}, Start: 1000, End: 2000, Limit: 1, Cursor: "0:bad"}); err == nil {
		t.Fatal("outside-range cursor accepted")
	}
}

func TestGovernanceHistoricalArchiveConflictingCopiesDoNotSelectArbitraryFact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"device_id": "a", "message_id": "m1", "timestamp": 1000, "values": []string{`{"temperature":27}`, `{"temperature":32}`}})
	}))
	defer server.Close()
	repo := &Repository{base: server.URL, http: server.Client()}
	if _, err := repo.ListGovernanceHistoricalArchiveMessages(context.Background(), "tenant", ports.AlarmObservationFilter{DeviceIDs: []string{"a"}, Start: 1000, End: 2000, Limit: 10}); err == nil {
		t.Fatal("conflicting archive originals became one fact")
	}
}
