package clickhouse

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
)

// Set IOT_TEST_CLICKHOUSE_URL (without a database) to run against a
// disposable ClickHouse; the test creates and drops its own database.
func TestTableTTLIsAppliedOnceAndRemovable(t *testing.T) {
	base := os.Getenv("IOT_TEST_CLICKHOUSE_URL")
	if base == "" {
		t.Skip("IOT_TEST_CLICKHOUSE_URL is not configured")
	}
	ctx := context.Background()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	database := fmt.Sprintf("ttl_test_%d", time.Now().UnixNano())
	q := u.Query()
	q.Set("database", database)
	u.RawQuery = q.Encode()
	open := func(telemetry, raw int) *Repository {
		t.Helper()
		r, err := NewWithOptions(ctx, u.String(), memory.NewRepository(), Options{TelemetryTTLDays: telemetry, RawTTLDays: raw})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := open(365, 180)
	t.Cleanup(func() { _, _ = r.query(context.Background(), "DROP DATABASE IF EXISTS "+database, nil) })
	create := func(table string) string {
		t.Helper()
		body, err := r.query(ctx, "SELECT create_table_query FROM system.tables WHERE database=currentDatabase() AND name='"+table+"' FORMAT TSVRaw", nil)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	if ddl := create("iot_telemetry"); !strings.Contains(ddl, "TTL toDateTime(ts) + toIntervalDay(365)") || !strings.Contains(ddl, "ttl_only_drop_parts = 1") {
		t.Fatalf("telemetry TTL missing: %s", ddl)
	}
	if ddl := create("iot_raw_message"); !strings.Contains(ddl, "toIntervalDay(180)") {
		t.Fatalf("raw TTL missing: %s", ddl)
	}
	comments, err := r.ttlComments(ctx)
	if err != nil || comments["iot_telemetry"] != 365 || comments["iot_raw_message"] != 180 {
		t.Fatalf("comments=%v err=%v", comments, err)
	}
	// Reopening with the same values is a no-op; 0 removes the TTL.
	open(365, 180)
	open(30, 0)
	if ddl := create("iot_telemetry"); !strings.Contains(ddl, "toIntervalDay(30)") {
		t.Fatalf("telemetry TTL not updated: %s", ddl)
	}
	if ddl := create("iot_raw_message"); strings.Contains(ddl, " TTL ") {
		t.Fatalf("raw TTL not removed: %s", ddl)
	}
}
