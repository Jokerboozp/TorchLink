package postgres

import (
	"context"
	"reflect"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestFireSafetyStore(t *testing.T) {
	repositorytest.FireSafety(t, testRepository(t))
}

func TestFireSafetyMigrationPreservesRecords(t *testing.T) {
	repo := testRepository(t)
	ctx := context.Background()
	state := model.FireSafetyState{Stations: []model.FireStation{{FireRecord: model.FireRecord{ID: "station"}, Name: "existing station"}}}
	if ok, err := repo.SaveFireSafetyState(ctx, "migration", state); err != nil || !ok {
		t.Fatalf("save: %v, %v", ok, err)
	}
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state.Revision = 1
	actual, err := repo.LoadFireSafetyState(ctx, "migration")
	if err != nil || !reflect.DeepEqual(actual, state) {
		t.Fatalf("repeated migration changed records: %+v, %v", actual, err)
	}
}
