package repositorytest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// StandardClaim checks cross-worker claiming: one live holder at a time,
// takeover after the lease, and completion fenced to the latest token.
func StandardClaim(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	msg := model.StandardMessage{TenantID: "claim-t", MessageID: fmt.Sprintf("claim-%d", time.Now().UnixNano()), RawMessageID: "raw", ProductID: "p", DeviceID: "d", MessageType: model.PropertyReport, Timestamp: 1}
	var wg sync.WaitGroup
	results := make(chan model.StandardClaim, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := repo.ClaimStandardMessage(ctx, msg, fmt.Sprintf("worker-%d", i), 2*time.Second)
			if err != nil {
				t.Error(err)
			}
			results <- c
		}(i)
	}
	wg.Wait()
	close(results)
	holders, created := 0, 0
	var first model.StandardClaim
	for c := range results {
		if c.ShouldProcess {
			holders++
			first = c
		} else if !c.Busy {
			t.Fatal("unprocessed message reported neither held nor busy", c)
		}
		if c.Created {
			created++
		}
	}
	if holders != 1 || created != 1 {
		t.Fatalf("holders=%d created=%d, want exactly one", holders, created)
	}
	// After the lease another worker takes over; the old token is fenced.
	time.Sleep(2100 * time.Millisecond)
	takeover, err := repo.ClaimStandardMessage(ctx, msg, "worker-new", time.Minute)
	if err != nil || !takeover.ShouldProcess || takeover.Token <= first.Token {
		t.Fatalf("takeover after lease failed: %+v %v", takeover, err)
	}
	if err = repo.MarkStandardMessageProcessed(ctx, msg.TenantID, msg.MessageID, first.Token); !errors.Is(err, model.ErrStaleClaim) {
		t.Fatalf("fenced holder recorded completion: %v", err)
	}
	if err = repo.MarkStandardMessageProcessed(ctx, msg.TenantID, msg.MessageID, takeover.Token); err != nil {
		t.Fatal(err)
	}
	if again, err := repo.ClaimStandardMessage(ctx, msg, "worker-late", time.Minute); err != nil || again.ShouldProcess || again.Busy {
		t.Fatalf("completed message claimed again: %+v %v", again, err)
	}
}

// VersionedWrites checks optimistic concurrency of alarms and device states.
func VersionedWrites(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	state := model.DeviceState{TenantID: "cas-t", DeviceID: "d-" + suffix, ProductID: "p", BusinessStatus: "ONLINE"}
	if ok, err := repo.UpsertDeviceStateIf(ctx, state); err != nil || !ok {
		t.Fatal("insert-if-absent failed", ok, err)
	}
	if ok, err := repo.UpsertDeviceStateIf(ctx, state); err != nil || ok {
		t.Fatal("second insert-if-absent must not overwrite", ok, err)
	}
	fresh, err := repo.GetDeviceStateFresh(ctx, state.TenantID, state.DeviceID)
	if err != nil || fresh.Version == 0 {
		t.Fatal("fresh read lacks version", fresh.Version, err)
	}
	next := fresh
	next.BusinessStatus = "ALARM"
	if ok, err := repo.UpsertDeviceStateIf(ctx, next); err != nil || !ok {
		t.Fatal("versioned update failed", ok, err)
	}
	stale := fresh
	stale.BusinessStatus = "OFFLINE"
	if ok, err := repo.UpsertDeviceStateIf(ctx, stale); err != nil || ok {
		t.Fatal("stale version overwrote a newer state", ok, err)
	}
	if got, _ := repo.GetDeviceStateFresh(ctx, state.TenantID, state.DeviceID); got.BusinessStatus != "ALARM" || got.Version != fresh.Version+1 {
		t.Fatalf("unexpected state %+v", got)
	}
	alarm := model.Alarm{ID: "a-" + suffix, TenantID: "cas-t", RuleID: "r", DeviceID: state.DeviceID, Status: "ACTIVE", AlarmLevel: "HIGH", Source: "device", LastTriggeredAt: 1}
	if _, _, err = repo.UpsertAlarm(ctx, alarm); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetAlarm(ctx, alarm.TenantID, alarm.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A concurrent trigger bumps the version; a write based on the old read fails.
	if _, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "other", TenantID: alarm.TenantID, RuleID: "r", DeviceID: alarm.DeviceID, Status: "ACTIVE", LastTriggeredAt: 2, TriggerID: "t2"}); err != nil {
		t.Fatal(err)
	}
	acked := stored
	acked.Status = "ACKED"
	if ok, err := repo.UpdateAlarmIf(ctx, acked); err != nil || ok {
		t.Fatal("stale alarm write succeeded", ok, err)
	}
	current, _ := repo.GetAlarm(ctx, alarm.TenantID, alarm.ID)
	current.Status = "ACKED"
	if ok, err := repo.UpdateAlarmIf(ctx, current); err != nil || !ok {
		t.Fatal("current alarm write failed", ok, err)
	}
	if final, _ := repo.GetAlarm(ctx, alarm.TenantID, alarm.ID); final.Status != "ACKED" || final.TriggerCount != current.TriggerCount {
		t.Fatalf("concurrent trigger lost: %+v", final)
	}
	if _, err = repo.UpdateAlarmIf(ctx, model.Alarm{ID: "missing-" + suffix, TenantID: "cas-t"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("missing alarm", err)
	}
}
