package repositorytest

import (
	"context"
	"fmt"
	"reflect"
	"testing"

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
	for i := range n {
		a := model.Alarm{ID: fmt.Sprintf("a%04d", i), TenantID: "report", DeviceID: fmt.Sprintf("d%d", i%7), DeviceName: fmt.Sprintf("设备%d", i%7), RuleID: fmt.Sprintf("r%d", i),
			AlarmType: []string{"FIRE", "DEVICE_FAULT", "SMOKE_DETECTED"}[i%3], AlarmLevel: []string{"HIGH", "CRITICAL", "LOW"}[i%3/2+i%2], Status: "CLOSED",
			// Every ten alarms share a timestamp.
			FirstTriggeredAt: int64(1000 + i/10*100), LastTriggeredAt: int64(1000 + i/10*100), TriggerCount: 1}
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
	other := model.Alarm{ID: "x", TenantID: "other", DeviceID: "d0", RuleID: "r", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", FirstTriggeredAt: 5000, LastTriggeredAt: 5000, TriggerCount: 1}
	if _, _, err := repo.UpsertAlarm(ctx, other); err != nil {
		t.Fatal(err)
	}
	filter := ports.AlarmFilter{TenantID: "report", Start: 1, End: 1_000_000}
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
	// Device and period filters apply.
	scoped, err := repo.AlarmDispositionStats(ctx, ports.AlarmFilter{TenantID: "report", DeviceIDs: []string{"d1"}, Start: 1000, End: 1500})
	if err != nil || scoped.Total != 9 {
		t.Fatalf("filtered statistics %+v %v", scoped, err)
	}
	empty, err := repo.AlarmDispositionStats(ctx, ports.AlarmFilter{TenantID: "none"})
	if err != nil || !reflect.DeepEqual(empty, model.SummarizeAlarms(nil)) {
		t.Fatalf("empty statistics %+v %v", empty, err)
	}
}
