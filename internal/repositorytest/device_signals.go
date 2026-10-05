package repositorytest

import (
	"context"
	"fmt"
	"math"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// DeviceSignalRepository is a store that keeps standard messages, states and
// device health signals.
type DeviceSignalRepository interface {
	ports.Repository
	ports.DeviceSignalStore
	ports.DeviceTelemetryStats
}

// DeviceSignals checks the telemetry statistics behind health signals and
// that a tenant's signals are replaced as a whole.
func DeviceSignals(t *testing.T, repo DeviceSignalRepository) {
	t.Helper()
	ctx := context.Background()
	for i, v := range []float64{10, 20, 30, 150} {
		msg := model.StandardMessage{TenantID: "signals", MessageID: fmt.Sprintf("sig-%d", i), RawMessageID: fmt.Sprintf("raw-%d", i), ProductID: "p", DeviceID: "d1", MessageType: model.PropertyReport, Timestamp: int64(1000 * (i + 1)),
			Properties: map[string]any{"temperature": v, "label": "x", "door": true}}
		if err := repo.SaveStandardMessage(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}
	outside := model.StandardMessage{TenantID: "signals", MessageID: "sig-late", RawMessageID: "raw-late", ProductID: "p", DeviceID: "d1", MessageType: model.PropertyReport, Timestamp: 99_000, Properties: map[string]any{"temperature": 1000.0}}
	other := model.StandardMessage{TenantID: "other", MessageID: "sig-other", RawMessageID: "raw-other", ProductID: "p", DeviceID: "d9", MessageType: model.PropertyReport, Timestamp: 1000, Properties: map[string]any{"temperature": 1.0}}
	for _, msg := range []model.StandardMessage{outside, other} {
		if err := repo.SaveStandardMessage(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}
	maximum := 100.0
	stats, err := repo.DevicePropertyStats(ctx, "signals", 1000, 4000, []model.PropertyRange{{ProductID: "p", Property: "temperature", Max: &maximum}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("only numeric properties are counted: %+v", stats)
	}
	s := stats[0]
	if s.DeviceID != "d1" || s.ProductID != "p" || s.Property != "temperature" || s.Count != 4 || s.Min != 10 || s.Max != 150 || s.Mean != 52.5 || s.OutOfRange != 1 || math.Abs(s.StdDev-56.734) > 0.001 {
		t.Fatalf("stats %+v", s)
	}
	reports, err := repo.DeviceReportStats(ctx, "signals", 1000, 4000)
	if err != nil || len(reports) != 1 || reports[0].Count != 4 || reports[0].FirstAt != 1000 || reports[0].LastAt != 4000 {
		t.Fatalf("reports %+v %v", reports, err)
	}
	first := []model.DeviceSignal{
		{TenantID: "signals", DeviceID: "d1", ProductID: "p", SignalType: model.SignalOutOfRange, Property: "temperature", Strength: 0.25, WindowStart: 1, WindowEnd: 2, Evidence: map[string]any{"outOfRange": float64(1)}, UpdatedAt: 2},
		{TenantID: "signals", DeviceID: "d2", ProductID: "p", SignalType: model.SignalReportDrift, Strength: 0.9, WindowStart: 1, WindowEnd: 2, UpdatedAt: 2},
	}
	if err = repo.ReplaceDeviceSignals(ctx, "signals", first); err != nil {
		t.Fatal(err)
	}
	listed, err := repo.ListDeviceSignals(ctx, "signals", nil, 10)
	if err != nil || len(listed) != 2 || listed[0].DeviceID != "d2" || listed[1].Evidence["outOfRange"] != float64(1) {
		t.Fatalf("listed %+v %v", listed, err)
	}
	if err = repo.ReplaceDeviceSignals(ctx, "signals", first[:1]); err != nil {
		t.Fatal(err)
	}
	if listed, err = repo.ListDeviceSignals(ctx, "signals", []string{"d2"}, 10); err != nil || len(listed) != 0 {
		t.Fatalf("replaced signals remain: %+v %v", listed, err)
	}
	if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "signals", DeviceID: "d1", ProductID: "p"}); err != nil {
		t.Fatal(err)
	}
	tenants, err := repo.SignalTenants(ctx)
	found := false
	for _, tenant := range tenants {
		found = found || tenant == "signals"
	}
	if err != nil || !found {
		t.Fatalf("tenants %v %v", tenants, err)
	}
}
