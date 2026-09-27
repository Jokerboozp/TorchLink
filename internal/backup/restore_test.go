package backup

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"testing"
)

func TestRestoreRefusesTheLiveDatabase(t *testing.T) {
	live := "postgres://iot:pw@db1:5432/iot?sslmode=disable"
	for _, target := range []string{live, "postgres://other:pw@db1:5432/iot", "postgres://iot:pw@db2:5432,db1:5432/iot"} {
		if err := restoreTargetSafe(live, target); !errors.Is(err, ErrRestoreTargetUnsafe) {
			t.Fatalf("%s accepted: %v", target, err)
		}
	}
	if err := restoreTargetSafe(live, ""); !errors.Is(err, ErrRestoreNotConfigured) {
		t.Fatal(err)
	}
	for _, target := range []string{"postgres://iot:pw@db1:5432/iot_restore", "postgres://iot:pw@restore-db:5432/iot"} {
		if err := restoreTargetSafe(live, target); err != nil {
			t.Fatalf("%s refused: %v", target, err)
		}
	}
}

func TestRestoreReadsEveryRecordIdentity(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, rec := range []rawLogRecord{
		{Storage: "postgres", Message: json.RawMessage(`{"messageId":"raw-1"}`)},
		{Storage: "clickhouse", Message: json.RawMessage(`{"message_id":"std-1","ts":"x"}`)},
	} {
		_ = enc.Encode(rec)
	}
	_ = gz.Close()
	r, _ := gzip.NewReader(&buf)
	dec := json.NewDecoder(r)
	var ids []string
	for dec.More() {
		var rec rawLogRecord
		if err := dec.Decode(&rec); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, recordMessageID(rec.Message))
	}
	if len(ids) != 2 || ids[0] != "raw-1" || ids[1] != "std-1" {
		t.Fatal(ids)
	}
	if manifestRecords(map[string]any{"records": float64(42)}) != 42 || manifestRecords(nil) != 0 {
		t.Fatal("manifest records")
	}
}
