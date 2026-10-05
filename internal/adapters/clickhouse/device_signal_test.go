package clickhouse

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

// Property statistics computed in ClickHouse match the definition used by the
// other stores: numeric values only, range counts from the thing model, and
// report counts from the underlying store. Needs IOT_TEST_CLICKHOUSE_URL.
func TestDevicePropertyStatsInClickHouse(t *testing.T) {
	base := os.Getenv("IOT_TEST_CLICKHOUSE_URL")
	if base == "" {
		t.Skip("IOT_TEST_CLICKHOUSE_URL is not configured")
	}
	ctx := context.Background()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	database := fmt.Sprintf("signals_test_%d", time.Now().UnixNano())
	admin := &Repository{base: base, http: &http.Client{Timeout: 30 * time.Second}}
	if _, err = admin.query(ctx, "CREATE DATABASE "+database, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.query(context.Background(), "DROP DATABASE IF EXISTS "+database, nil) })
	q := u.Query()
	q.Set("database", database)
	u.RawQuery = q.Encode()
	inner := memory.NewRepository()
	repo, err := NewWithOptions(ctx, u.String(), inner, Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Minute).UnixMilli()
	for i, v := range []float64{10, 20, 30, 150} {
		msg := model.StandardMessage{TenantID: "signals", MessageID: fmt.Sprintf("sig-%d", i), RawMessageID: fmt.Sprintf("raw-%d", i), ProductID: "p", DeviceID: "d1", MessageType: model.PropertyReport, Timestamp: now + int64(i*1000),
			Properties: map[string]any{"temperature": v, "label": "x", "door": true}}
		if err = repo.SaveStandardMessage(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}
	other := model.StandardMessage{TenantID: "other", MessageID: "sig-other", RawMessageID: "raw-other", ProductID: "p", DeviceID: "d9", MessageType: model.PropertyReport, Timestamp: now, Properties: map[string]any{"temperature": 1.0}}
	if err = repo.SaveStandardMessage(ctx, other); err != nil {
		t.Fatal(err)
	}
	maximum := 100.0
	stats, err := repo.DevicePropertyStats(ctx, "signals", now, now+3000, []model.PropertyRange{{ProductID: "p", Property: "temperature", Max: &maximum}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats %+v", stats)
	}
	s := stats[0]
	if s.DeviceID != "d1" || s.ProductID != "p" || s.Property != "temperature" || s.Count != 4 || s.Min != 10 || s.Max != 150 || s.Mean != 52.5 || s.OutOfRange != 1 || math.Abs(s.StdDev-56.734) > 0.001 {
		t.Fatalf("stats %+v", s)
	}
	if empty, err := repo.DevicePropertyStats(ctx, "signals", now, now+3000, nil); err != nil || len(empty) != 1 || empty[0].OutOfRange != 0 {
		t.Fatalf("without ranges %+v %v", empty, err)
	}
	reports, err := repo.DeviceReportStats(ctx, "signals", now, now+3000)
	if err != nil || len(reports) != 1 || reports[0].Count != 4 {
		t.Fatalf("reports %+v %v", reports, err)
	}
}
