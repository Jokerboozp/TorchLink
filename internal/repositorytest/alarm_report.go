package repositorytest

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// AlarmReports checks that the store's statistics match the in-memory
// summary of the same alarms, and that EachAlarm returns every alarm once,
// newest first, across batch boundaries and equal timestamps.
func AlarmReports(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const n = 1205
	// Every ten alarms share a timestamp; the alarms span about two weeks so
	// days are counted in the report time zone.
	base, step := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC).UnixMilli(), int64(3*time.Hour/time.Millisecond)
	for i := range n {
		a := model.Alarm{ID: fmt.Sprintf("a%04d", i), TenantID: "report", DeviceID: fmt.Sprintf("d%d", i%7), DeviceName: fmt.Sprintf("设备%d", i%7), RuleID: fmt.Sprintf("r%d", i),
			AlarmType: []string{"FIRE", "DEVICE_FAULT", "SMOKE_DETECTED"}[i%3], AlarmLevel: []string{"HIGH", "CRITICAL", "LOW"}[i%3/2+i%2], Status: "CLOSED",
			FirstTriggeredAt: base + int64(i/10)*step, LastTriggeredAt: base + int64(i/10)*step, TriggerCount: 1}
		if i%4 == 0 {
			a.AckedAt = a.FirstTriggeredAt + int64(i%50)*1000
		}
		if i%3 != 1 && i%5 != 0 {
			result := []string{model.DispositionFalseAlarm, model.DispositionRealFire, model.DispositionTest}[i%3/2+i%2]
			a.Disposition = &model.AlarmDisposition{Result: result, Handler: "h", VerifiedAt: a.FirstTriggeredAt + int64(i%37)*1000}
		}
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	other := model.Alarm{ID: "x", TenantID: "other", DeviceID: "d0", RuleID: "r", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", FirstTriggeredAt: base, LastTriggeredAt: base, TriggerCount: 1}
	if _, _, err := repo.UpsertAlarm(ctx, other); err != nil {
		t.Fatal(err)
	}
	filter := ports.AlarmFilter{TenantID: "report", Start: base, End: base + n*step}
	var seen []model.Alarm
	ids := map[string]bool{}
	if err := repo.EachAlarm(ctx, filter, func(a model.Alarm) error {
		if ids[a.ID] {
			return fmt.Errorf("alarm %s returned twice", a.ID)
		}
		ids[a.ID] = true
		if len(seen) > 0 {
			prev := seen[len(seen)-1]
			if a.LastTriggeredAt > prev.LastTriggeredAt {
				return fmt.Errorf("alarm %s out of order", a.ID)
			}
		}
		seen = append(seen, a)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != n {
		t.Fatalf("each alarm returned %d, want %d", len(seen), n)
	}
	got, err := repo.AlarmDispositionStats(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if want := model.SummarizeAlarms(seen); !reflect.DeepEqual(got, want) {
		t.Fatalf("statistics\n got %+v\nwant %+v", got, want)
	}
	if got.Total != n || got.Verified == 0 || got.Unverified == 0 || got.Acknowledge.Count == 0 || len(got.TopFalseAlarmDevices) == 0 {
		t.Fatalf("fixture does not exercise the statistics: %+v", got)
	}
	breakdown, err := repo.AlarmBreakdown(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	if want := model.BreakdownAlarms(seen); !reflect.DeepEqual(breakdown, want) {
		t.Fatalf("breakdown\n got %+v\nwant %+v", breakdown, want)
	}
	if len(breakdown.ByDay) < 10 || breakdown.ByDay[0].Day != "2026-09-02" {
		t.Fatalf("days are not counted in the report zone: %+v", breakdown.ByDay)
	}
	// Device and period filters apply.
	scoped, err := repo.AlarmDispositionStats(ctx, ports.AlarmFilter{TenantID: "report", DeviceIDs: []string{"d1"}, Start: base, End: base + 5*step})
	if err != nil || scoped.Total != 9 {
		t.Fatalf("filtered statistics %+v %v", scoped, err)
	}
	empty, err := repo.AlarmDispositionStats(ctx, ports.AlarmFilter{TenantID: "none"})
	if err != nil || !reflect.DeepEqual(empty, model.SummarizeAlarms(nil)) {
		t.Fatalf("empty statistics %+v %v", empty, err)
	}
	if empty, err := repo.AlarmBreakdown(ctx, ports.AlarmFilter{TenantID: "none"}); err != nil || !reflect.DeepEqual(empty, model.BreakdownAlarms(nil)) {
		t.Fatalf("empty breakdown %+v %v", empty, err)
	}
}
