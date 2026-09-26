package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMessageQueriesFullAndDaily(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, loc)
	for _, parsed := range []bool{false, true} {
		pg, args, ch := messageQueries(time.Time{}, time.Now(), parsed)
		if strings.Contains(pg, "WHERE") || strings.Contains(ch, "WHERE") || len(args) != 0 {
			t.Fatal("full backup must include all saved device timestamps")
		}
		pg, args, ch = messageQueries(start, start.AddDate(0, 0, 1), parsed)
		if len(args) != 2 || args[0] != start.UnixMilli() || args[1] != start.AddDate(0, 0, 1).UnixMilli() {
			t.Fatalf("incorrect daily window: %v", args)
		}
		if !strings.Contains(pg, " >= $1") || !strings.Contains(pg, " < $2") || !strings.Contains(ch, fmt.Sprint(start.UnixMilli())) {
			t.Fatal("daily window must be half open and preserve timezone")
		}
		if parsed && (!strings.Contains(pg, "standard_message") || !strings.Contains(pg, "processed_at") || !strings.Contains(ch, "iot_telemetry")) {
			t.Fatal("parsed data source missing")
		}
		if !parsed && (!strings.Contains(pg, "raw_message_log") || !strings.Contains(ch, "iot_raw_message")) {
			t.Fatal("raw data source missing")
		}
	}
}

func TestClickHouseExportPreservesPayloads(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		unwrap     bool
	}{
		{"raw", `{"body":"{\"messageId\":\"raw1\",\"payload\":\"0102\"}"}`, true},
		{"parsed", `{"message_id":"parsed1","properties":{"temperature":42}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			s := &Service{cfg: Config{ClickHouseURL: server.URL}}
			var output bytes.Buffer
			count, err := s.exportClickHouseRows(context.Background(), "SELECT", tc.unwrap, json.NewEncoder(&output))
			if err != nil || count != 1 {
				t.Fatalf("count=%d err=%v", count, err)
			}
			var record rawLogRecord
			if err = json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Storage != "clickhouse" || !json.Valid(record.Message) {
				t.Fatal("invalid record", output.String())
			}
			if tc.unwrap && !bytes.Contains(record.Message, []byte("0102")) {
				t.Fatal("lost raw payload")
			}
			if !tc.unwrap && !bytes.Contains(record.Message, []byte("temperature")) {
				t.Fatal("lost parsed properties")
			}
		})
	}
}

func TestClickHouseExportFailsOnCorruptData(t *testing.T) {
	for _, body := range []string{`{"body":"not json"}`, `{"body":`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		s := &Service{cfg: Config{ClickHouseURL: server.URL}}
		var output bytes.Buffer
		_, err := s.exportClickHouseRows(context.Background(), "SELECT", true, json.NewEncoder(&output))
		server.Close()
		if err == nil {
			t.Fatal("corrupt backup reported success")
		}
	}
}
