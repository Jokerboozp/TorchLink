package repositorytest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// FireSafety checks the shared persistence contract for management aggregates.
func FireSafety(t *testing.T, repo ports.FireSafetyStore) {
	t.Helper()
	ctx := context.Background()
	t.Run("revision_and_isolation", func(t *testing.T) {
		tenant := "fire-safety-revision"
		state, err := repo.LoadFireSafetyState(ctx, tenant)
		if err != nil || !reflect.DeepEqual(state, model.FireSafetyState{}) {
			t.Fatalf("initial state: %+v, %v", state, err)
		}
		state.Revision = 4
		if ok, err := repo.SaveFireSafetyState(ctx, tenant, state); err != nil || ok {
			t.Fatalf("nonzero revision created an absent state: %v, %v", ok, err)
		}
		state = fireSafetySample()
		if ok, err := repo.SaveFireSafetyState(ctx, tenant, state); err != nil || !ok {
			t.Fatalf("initial save: %v, %v", ok, err)
		}
		state.Revision = 1
		actual, err := repo.LoadFireSafetyState(ctx, tenant)
		if err != nil || !reflect.DeepEqual(actual, state) {
			t.Fatalf("initial persisted state: %+v, %v", actual, err)
		}
		stale := state
		stale.Revision = 0
		stale.Stations = []model.FireStation{{Name: "stale"}}
		if ok, err := repo.SaveFireSafetyState(ctx, tenant, stale); err != nil || ok {
			t.Fatalf("stale revision accepted: %v, %v", ok, err)
		}
		state.Stations[0].Name = "updated station"
		if ok, err := repo.SaveFireSafetyState(ctx, tenant, state); err != nil || !ok {
			t.Fatalf("current revision rejected: %v, %v", ok, err)
		}
		state.Revision = 2
		actual, err = repo.LoadFireSafetyState(ctx, tenant)
		if err != nil || !reflect.DeepEqual(actual, state) {
			t.Fatalf("revision did not advance: %+v, %v", actual, err)
		}
		other, err := repo.LoadFireSafetyState(ctx, tenant+"-other")
		if err != nil || !reflect.DeepEqual(other, model.FireSafetyState{}) {
			t.Fatalf("another tenant exposed records: %+v, %v", other, err)
		}
		other.Stations = []model.FireStation{{FireRecord: model.FireRecord{ID: state.Stations[0].ID}, Name: "other station"}}
		if ok, err := repo.SaveFireSafetyState(ctx, tenant+"-other", other); err != nil || !ok {
			t.Fatalf("same record ID in another tenant rejected: %v, %v", ok, err)
		}
		actual, err = repo.LoadFireSafetyState(ctx, tenant)
		if err != nil || !reflect.DeepEqual(actual, state) {
			t.Fatalf("another tenant changed records: %+v, %v", actual, err)
		}
	})
	t.Run("deep_copy", func(t *testing.T) {
		tenant := "fire-safety-copy"
		state := fireSafetySample()
		if ok, err := repo.SaveFireSafetyState(ctx, tenant, state); err != nil || !ok {
			t.Fatalf("save: %v, %v", ok, err)
		}
		mutateFireSafetySample(&state)
		actual, err := repo.LoadFireSafetyState(ctx, tenant)
		expected := fireSafetySample()
		expected.Revision = 1
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("save retained mutable aliases: %+v, %v", actual, err)
		}
		mutateFireSafetySample(&actual)
		actual, err = repo.LoadFireSafetyState(ctx, tenant)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("load returned mutable aliases: %+v, %v", actual, err)
		}
	})
	t.Run("concurrent_compare_and_swap", func(t *testing.T) {
		tenant := "fire-safety-concurrent"
		for revision := int64(0); revision < 2; revision++ {
			const writers = 16
			start := make(chan struct{})
			type outcome struct {
				name string
				ok   bool
				err  error
			}
			results := make(chan outcome, writers)
			for i := 0; i < writers; i++ {
				name := fmt.Sprintf("writer-%d", i)
				go func() {
					<-start
					state := model.FireSafetyState{Revision: revision, Stations: []model.FireStation{{Name: name}}}
					ok, err := repo.SaveFireSafetyState(ctx, tenant, state)
					results <- outcome{name, ok, err}
				}()
			}
			close(start)
			winners, winner := 0, ""
			for i := 0; i < writers; i++ {
				result := <-results
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.ok {
					winners++
					winner = result.name
				}
			}
			actual, err := repo.LoadFireSafetyState(ctx, tenant)
			if err != nil || winners != 1 || actual.Revision != revision+1 || len(actual.Stations) != 1 || actual.Stations[0].Name != winner {
				t.Fatalf("CAS did not preserve its only winner: winners=%d, state=%+v, err=%v", winners, actual, err)
			}
		}
	})
	t.Run("cancelled_context", func(t *testing.T) {
		tenant := "fire-safety-cancelled"
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := repo.LoadFireSafetyState(cancelled, tenant); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled load: %v", err)
		}
		if ok, err := repo.SaveFireSafetyState(cancelled, tenant, fireSafetySample()); ok || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled save: %v, %v", ok, err)
		}
		actual, err := repo.LoadFireSafetyState(ctx, tenant)
		if err != nil || !reflect.DeepEqual(actual, model.FireSafetyState{}) {
			t.Fatalf("cancelled save changed state: %+v, %v", actual, err)
		}
	})
}

func fireSafetySample() model.FireSafetyState {
	return model.FireSafetyState{
		Stations:      []model.FireStation{{FireRecord: model.FireRecord{ID: "station", Version: 1}, Name: "station"}},
		Personnel:     []model.FirePersonnel{{FireRecord: model.FireRecord{ID: "person", Version: 1}, Name: "person", StationID: "station"}},
		Equipment:     []model.FireEquipment{{FireRecord: model.FireRecord{ID: "equipment", Version: 1}, StationID: "station", Quantity: 4}},
		Dispatches:    []model.FireDispatch{{PersonnelIDs: []string{"person"}, Equipment: []model.FireEquipmentUsage{{EquipmentID: "equipment", Quantity: 2}}}},
		Shifts:        []model.DutyShift{{Name: "night", StartTime: "20:00", EndTime: "08:00"}},
		Assignments:   []model.DutyAssignment{{PersonnelIDs: []string{"person"}}},
		Swaps:         []model.DutySwap{{Status: "pending"}},
		Extinguishers: []model.Extinguisher{{Code: "F-001", Status: "active"}},
		Inspections:   []model.FireInspection{{Checks: []model.FireInspectionCheck{{Name: "pressure", Passed: true}}, Rectifications: []model.FireRectification{{Action: "replace"}}}},
	}
}

func mutateFireSafetySample(state *model.FireSafetyState) {
	state.Stations[0].Name = "changed"
	state.Personnel[0].Name = "changed"
	state.Equipment[0].Quantity = 99
	state.Dispatches[0].PersonnelIDs[0] = "changed"
	state.Dispatches[0].Equipment[0].Quantity = 99
	state.Shifts[0].Name = "changed"
	state.Assignments[0].PersonnelIDs[0] = "changed"
	state.Swaps[0].Status = "changed"
	state.Extinguishers[0].Code = "changed"
	state.Inspections[0].Checks[0].Passed = false
	state.Inspections[0].Rectifications[0].Action = "changed"
}
