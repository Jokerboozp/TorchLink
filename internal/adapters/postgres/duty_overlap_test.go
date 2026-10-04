package postgres

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
)

func duty(id string, start, end int64, people ...string) model.DutyAssignment {
	return model.DutyAssignment{FireRecord: model.FireRecord{ID: id, Version: 1}, StationID: "s", ShiftID: "shift", PersonnelIDs: people, StartAt: start, EndAt: end}
}

// TestDutyOverlapIsRejectedByTheDatabase saves states directly, bypassing
// the service checks, as a concurrent or faulty writer would.
func TestDutyOverlapIsRejectedByTheDatabase(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	before := []migration{}
	for _, m := range all {
		if m.version < 13 {
			before = append(before, m)
		}
	}
	if err = migrateWith(ctx, pool, before); err != nil {
		t.Fatal(err)
	}
	r := &Repository{pool: pool}
	// Overlapping duty stored before the constraint must not stop the upgrade.
	legacy := model.FireSafetyState{Assignments: []model.DutyAssignment{duty("old-a", 0, 100, "p1"), duty("old-b", 50, 150, "p1")}}
	if ok, err := r.SaveFireSafetyState(ctx, "legacy", legacy); err != nil || !ok {
		t.Fatalf("legacy save: %v %v", ok, err)
	}
	if err = r.Migrate(ctx); err != nil {
		t.Fatalf("upgrade with existing overlap: %v", err)
	}

	state := model.FireSafetyState{Assignments: []model.DutyAssignment{duty("a", 1000, 2000, "p1", "p2")}}
	if ok, err := r.SaveFireSafetyState(ctx, "t", state); err != nil || !ok {
		t.Fatalf("first save: %v %v", ok, err)
	}
	state.Revision = 1
	overlap := state
	overlap.Assignments = append(append([]model.DutyAssignment(nil), state.Assignments...), duty("b", 1500, 2500, "p2"))
	if ok, err := r.SaveFireSafetyState(ctx, "t", overlap); !errors.Is(err, model.ErrDutyOverlap) || ok {
		t.Fatalf("overlap accepted: %v %v", ok, err)
	}
	// Adjacent duty and other people do not conflict.
	next := state
	next.Assignments = append(append([]model.DutyAssignment(nil), state.Assignments...), duty("c", 2000, 3000, "p2"), duty("d", 1500, 2500, "p3"))
	if ok, err := r.SaveFireSafetyState(ctx, "t", next); err != nil || !ok {
		t.Fatalf("adjacent duty: %v %v", ok, err)
	}
	// Replacing an assignment by an overlapping one in the same save is fine:
	// the constraint is checked at commit.
	next.Revision = 2
	replaced := next
	replaced.Assignments = []model.DutyAssignment{duty("a2", 1200, 2200, "p1"), next.Assignments[1], next.Assignments[2]}
	if ok, err := r.SaveFireSafetyState(ctx, "t", replaced); err != nil || !ok {
		t.Fatalf("replace within one save: %v %v", ok, err)
	}
	// The same person in another tenant is independent.
	if ok, err := r.SaveFireSafetyState(ctx, "other", model.FireSafetyState{Assignments: []model.DutyAssignment{duty("x", 1500, 2500, "p1")}}); err != nil || !ok {
		t.Fatalf("other tenant: %v %v", ok, err)
	}
}
