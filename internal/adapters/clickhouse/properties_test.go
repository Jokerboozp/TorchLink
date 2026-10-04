package clickhouse

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

// Messages stored without properties (as PostgreSQL does while ClickHouse is
// configured) read back with exactly the reported properties, and an
// existing telemetry table gains the properties_text column. Needs
// IOT_TEST_CLICKHOUSE_URL like the TTL test.
func TestTelemetryPropertiesRestoredFromClickHouse(t *testing.T) {
	base := os.Getenv("IOT_TEST_CLICKHOUSE_URL")
	if base == "" {
		t.Skip("IOT_TEST_CLICKHOUSE_URL is not configured")
	}
	ctx := context.Background()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	database := fmt.Sprintf("props_test_%d", time.Now().UnixNano())
	admin := &Repository{base: base, http: &http.Client{Timeout: 30 * time.Second}}
	if _, err = admin.query(ctx, "CREATE DATABASE "+database, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.query(context.Background(), "DROP DATABASE IF EXISTS "+database, nil) })
	// A table from before properties_text existed.
	if _, err = admin.query(ctx, "CREATE TABLE "+database+".iot_telemetry (tenant_id String, device_id String, product_id String, message_id String, ts DateTime64(3), properties JSON) ENGINE=MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (tenant_id,device_id,ts,message_id)", nil); err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("database", database)
	u.RawQuery = q.Encode()
	inner := memory.NewRepository()
	repo, err := NewWithOptions(ctx, u.String(), inner, Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	properties := map[string]any{"temperature": 26.5, "count": float64(3), "温度-1": "高", "nested": map[string]any{"a": []any{float64(1), "x"}}, "empty": nil}
	msg := model.StandardMessage{TenantID: "t", MessageID: "m1", RawMessageID: "raw-1", ProductID: "p", DeviceID: "d", MessageType: model.PropertyReport, Timestamp: now, Properties: properties}
	stripped := msg
	stripped.Properties = nil
	if _, err = inner.SaveStandardMessageIfAbsent(ctx, stripped); err != nil {
		t.Fatal(err)
	}
	if err = repo.ensureTelemetry(ctx, msg, true); err != nil {
		t.Fatal(err)
	}
	event := model.StandardMessage{TenantID: "t", MessageID: "m2", RawMessageID: "raw-2", ProductID: "p", DeviceID: "d", MessageType: model.EventReport, Timestamp: now - 1, Properties: map[string]any{"kept": true}}
	if _, err = inner.SaveStandardMessageIfAbsent(ctx, event); err != nil {
		t.Fatal(err)
	}

	byRaw, err := repo.GetStandardMessageByRaw(ctx, "t", "raw-1")
	if err != nil || !reflect.DeepEqual(byRaw.Properties, properties) {
		t.Fatalf("by raw: %+v %v", byRaw.Properties, err)
	}
	if latest, err := repo.GetLatestMessage(ctx, "t", "d"); err != nil || !reflect.DeepEqual(latest.Properties, properties) {
		t.Fatalf("latest: %+v %v", latest.Properties, err)
	}
	list, total, err := repo.ListDeviceMessages(ctx, "t", "d", "", 10, 0)
	if err != nil || total != 2 || !reflect.DeepEqual(list[0].Properties, properties) || list[1].Properties["kept"] != true {
		t.Fatalf("device messages: %+v %d %v", list, total, err)
	}
	batch, err := repo.GetStandardMessagesByRawIDs(ctx, "t", []string{"raw-1", "raw-2"})
	if err != nil || !reflect.DeepEqual(batch["raw-1"].Properties, properties) || batch["raw-2"].Properties["kept"] != true {
		t.Fatalf("by raw IDs: %+v %v", batch, err)
	}
	// A property name that is not a plain identifier is read from the text.
	history, total, err := repo.PropertyHistoryPage(ctx, "t", "d", "温度-1", now-1000, now+1000, 10, 0)
	if err != nil || total != 1 || len(history) != 1 || history[0]["value"] != "高" || history[0]["timestamp"] != now {
		t.Fatalf("text history: %+v %d %v", history, total, err)
	}
}
