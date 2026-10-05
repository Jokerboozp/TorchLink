package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

// signalFixture reports a day of data: a stuck sensor, one that reports far
// less often than configured, one outside its valid range, and one that
// stands out among six devices of a template.
func signalFixture(t *testing.T) (*Engine, time.Time) {
	t.Helper()
	e, repo, _ := newBusinessEngine(t, nil)
	e.DeviceSignals, e.TelemetryStats = repo, repo
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	e.Clock = fixedClock{now}
	ctx := context.Background()
	maximum := 100.0
	for _, p := range []model.Product{
		{ID: "temp", TenantID: "t1", ReportIntervalSec: 300, ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "temperature", DataType: "number", Max: &maximum}}}},
		{ID: "press", TenantID: "t1", ReportIntervalSec: 60},
	} {
		if err := repo.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	save := func(device, product string, at time.Time, properties map[string]any) {
		t.Helper()
		msg := model.StandardMessage{TenantID: "t1", MessageID: fmt.Sprintf("%s-%d", device, at.UnixNano()), RawMessageID: "raw", ProductID: product, DeviceID: device, MessageType: model.PropertyReport, Timestamp: at.UnixMilli(), Properties: properties}
		if err := repo.SaveStandardMessage(ctx, msg); err != nil {
			t.Fatal(err)
		}
		interval := int64(300)
		if product == "press" {
			interval = 60
		}
		if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t1", DeviceID: device, ProductID: product, ReportIntervalSec: interval, OfflineToleranceSec: 60, LastSeenAt: at.UnixMilli()}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 12 {
		at := now.Add(-time.Duration(i) * 5 * time.Minute)
		save("stuck", "temp", at, map[string]any{"temperature": 21.0})
		save("normal", "temp", at, map[string]any{"temperature": 20.0 + float64(i%3)})
		save("hot", "temp", at, map[string]any{"temperature": 150.0 + float64(i)})
	}
	for i := range 4 {
		save("slow", "temp", now.Add(-time.Duration(i)*time.Hour), map[string]any{"temperature": 20.0 + float64(i)})
	}
	for d := range 9 {
		for i := range 3 {
			value := 0.30 + float64(d%3)*0.01 + float64(i)*0.001
			if d == 0 {
				value = 1.5
			}
			save(fmt.Sprintf("p%d", d), "press", now.Add(-time.Duration(i)*time.Minute), map[string]any{"pressure": value})
		}
	}
	return e, now
}

func TestDeviceSignalsFromReportedData(t *testing.T) {
	e, now := signalFixture(t)
	signals, err := e.ComputeTenantSignals(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]model.DeviceSignal{}
	for _, s := range signals {
		found[s.DeviceID+"/"+s.SignalType+"/"+s.Property] = s
		if s.WindowEnd != now.UnixMilli() || s.Strength <= 0 || s.Strength > 1 {
			t.Fatalf("signal window or strength: %+v", s)
		}
	}
	for _, key := range []string{"stuck/STUCK_VALUE/temperature", "hot/OUT_OF_RANGE/temperature", "slow/REPORT_DRIFT/", "p0/PEER_OUTLIER/pressure"} {
		if _, ok := found[key]; !ok {
			t.Errorf("missing signal %s in %v", key, signals)
		}
	}
	for _, key := range []string{"normal/STUCK_VALUE/temperature", "normal/REPORT_DRIFT/", "p1/PEER_OUTLIER/pressure"} {
		if _, ok := found[key]; ok {
			t.Errorf("unexpected signal %s", key)
		}
	}
	if drift := found["slow/REPORT_DRIFT/"]; drift.Evidence["expectedIntervalSec"] != int64(300) || drift.Evidence["averageIntervalSec"] != 3600.0 {
		t.Fatalf("drift evidence %v", drift.Evidence)
	}
}

func TestDeviceSignalsJobStoresAndOptionallyAlarms(t *testing.T) {
	e, _ := signalFixture(t)
	ctx := context.Background()
	if err := e.ComputeDeviceSignalsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := e.DeviceSignals.ListDeviceSignals(ctx, "t1", []string{"stuck"}, 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored %+v %v", stored, err)
	}
	alarms, _ := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Source: signalAlarmSource})
	if len(alarms) != 0 {
		t.Fatal("signals must not raise alarms unless enabled")
	}
	if items := e.deviceSignalsContext(ctx, "t1", "hot"); len(items) != 1 || items[0]["name"] != "数值超出有效范围" {
		t.Fatalf("analysis context %v", items)
	}
	e.SignalOptions.RaiseAlarms = true
	if err = e.ComputeDeviceSignalsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	alarms, _ = e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Source: signalAlarmSource, Status: "ACTIVE"})
	byDevice := map[string]model.Alarm{}
	for _, a := range alarms {
		byDevice[a.DeviceID] = a
	}
	if a, ok := byDevice["hot"]; !ok || a.AlarmType != SignalAlarmType || a.AlarmLevel != "LOW" {
		t.Fatalf("strong signal alarm missing: %+v", alarms)
	}
	// A second run keeps the open alarms instead of duplicating them.
	if err = e.ComputeDeviceSignalsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Source: signalAlarmSource, Status: "ACTIVE"})
	if len(again) != len(alarms) {
		t.Fatalf("alarms duplicated: %d then %d", len(alarms), len(again))
	}
	// Signals that disappear recover their alarms.
	e.Clock = fixedClock{e.Clock.Now().Add(72 * time.Hour)}
	if err = e.ComputeDeviceSignalsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if open, _ := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Source: signalAlarmSource, Status: "ACTIVE"}); len(open) != 0 {
		t.Fatalf("signal alarms not recovered: %+v", open)
	}
}
