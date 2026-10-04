package repositorytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// StandardCompletion checks the combined state read and the combined state
// write with processed mark used by message processing.
func StandardCompletion(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	tenant, device := "completion", "dev"
	if _, open, err := repo.LoadDeviceStateWithAlarms(ctx, tenant, device); !errors.Is(err, model.ErrNotFound) || open {
		t.Fatalf("absent state: open=%v err=%v", open, err)
	}
	alarm := model.Alarm{ID: "a1", TenantID: tenant, DeviceID: device, RuleID: "r", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", TriggerID: "t1", FirstTriggeredAt: 1, LastTriggeredAt: 1, TriggerCount: 1}
	if _, _, err := repo.UpsertAlarm(ctx, alarm); err != nil {
		t.Fatal(err)
	}
	claim := func(id string) int64 {
		t.Helper()
		c, err := repo.ClaimStandardMessage(ctx, model.StandardMessage{TenantID: tenant, MessageID: id, RawMessageID: "raw-" + id, ProductID: "p", DeviceID: device, MessageType: model.PropertyReport, Timestamp: 1000, Properties: map[string]any{"v": 1}}, "worker", time.Minute)
		if err != nil || !c.ShouldProcess {
			t.Fatalf("claim %s: %+v %v", id, c, err)
		}
		return c.Token
	}
	token := claim("m1")
	state, open, err := repo.LoadDeviceStateWithAlarms(ctx, tenant, device)
	if !errors.Is(err, model.ErrNotFound) || !open {
		t.Fatalf("an alarm without a state row still counts as open: open=%v err=%v", open, err)
	}
	state = model.DeviceState{TenantID: tenant, DeviceID: device, ProductID: "p", BusinessStatus: "ALARM", LastSeenAt: 1000}
	if ok, err := repo.CompleteStandardMessage(ctx, &state, tenant, "m1", token); err != nil || !ok {
		t.Fatalf("first completion: %v %v", ok, err)
	}
	stored, open, err := repo.LoadDeviceStateWithAlarms(ctx, tenant, device)
	if err != nil || !open || stored.Version != 1 || stored.BusinessStatus != "ALARM" {
		t.Fatalf("stored state: %+v open=%v err=%v", stored, open, err)
	}
	// Closing the alarm clears the open flag.
	closed := alarm
	closed.Status, closed.Version = "CLOSED", 1
	if err = repo.UpdateAlarm(ctx, closed); err != nil {
		t.Fatal(err)
	}
	if _, open, err = repo.LoadDeviceStateWithAlarms(ctx, tenant, device); err != nil || open {
		t.Fatalf("closed alarm still open: %v %v", open, err)
	}
	// A stale state version writes nothing and leaves the message pending,
	// so the retry can complete it.
	token = claim("m2")
	stale := stored
	stale.Version = 0
	if ok, err := repo.CompleteStandardMessage(ctx, &stale, tenant, "m2", token); err != nil || ok {
		t.Fatalf("conflicting state accepted: %v %v", ok, err)
	}
	next := stored
	next.BusinessStatus, next.LastSeenAt = "ONLINE", 2000
	if ok, err := repo.CompleteStandardMessage(ctx, &next, tenant, "m2", token); err != nil || !ok {
		t.Fatalf("retried completion: %v %v", ok, err)
	}
	// A message that needs no state write is still marked, once.
	token = claim("m3")
	if ok, err := repo.CompleteStandardMessage(ctx, nil, tenant, "m3", token); err != nil || !ok {
		t.Fatalf("mark only: %v %v", ok, err)
	}
	if _, err := repo.CompleteStandardMessage(ctx, nil, tenant, "m3", token); !errors.Is(err, model.ErrStaleClaim) {
		t.Fatalf("second completion must be stale: %v", err)
	}
	if c, err := repo.ClaimStandardMessage(ctx, model.StandardMessage{TenantID: tenant, MessageID: "m2", DeviceID: device, ProductID: "p", MessageType: model.PropertyReport, Timestamp: 1000}, "other", time.Minute); err != nil || c.ShouldProcess {
		t.Fatalf("completed message claimed again: %+v %v", c, err)
	}
}
