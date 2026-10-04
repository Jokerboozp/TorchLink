package repositorytest

import (
	"context"
	"reflect"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Sites checks the site store's revision, isolation and record diffing.
func Sites(t *testing.T, repo ports.SiteStore) {
	t.Helper()
	ctx := context.Background()
	tenant := "sites-contract"
	state, err := repo.LoadSiteState(ctx, tenant)
	if err != nil || !reflect.DeepEqual(state, model.SiteState{}) {
		t.Fatalf("initial state: %+v, %v", state, err)
	}
	x, y := 0.25, 0.75
	state = model.SiteState{
		Units:     []model.SiteUnit{{SiteRecord: model.SiteRecord{ID: "u1", Version: 1}, Name: "示例单位"}},
		Buildings: []model.SiteBuilding{{SiteRecord: model.SiteRecord{ID: "b1", Version: 1}, UnitID: "u1", Name: "1 号楼"}},
		Floors:    []model.SiteFloor{{SiteRecord: model.SiteRecord{ID: "f1", Version: 1}, BuildingID: "b1", Name: "3F", Level: 3, Plan: &model.FloorPlan{ContentType: "image/png", SHA256: "abc", Size: 10, Width: 100, Height: 50}}},
		Points:    []model.SitePoint{{SiteRecord: model.SiteRecord{ID: "p1", Version: 1}, UnitID: "u1", BuildingID: "b1", FloorID: "f1", DeviceID: "d1", X: &x, Y: &y}},
	}
	if ok, err := repo.SaveSiteState(ctx, tenant, model.SiteState{}, state); err != nil || !ok {
		t.Fatalf("initial save: %v, %v", ok, err)
	}
	state.Revision = 1
	actual, err := repo.LoadSiteState(ctx, tenant)
	if err != nil || !reflect.DeepEqual(actual, state) {
		t.Fatalf("persisted state: %+v, %v", actual, err)
	}
	if revision, err := repo.SiteRevision(ctx, tenant); err != nil || revision != 1 {
		t.Fatalf("revision: %d, %v", revision, err)
	}
	if ok, err := repo.SaveSiteState(ctx, tenant, model.SiteState{}, state); err != nil || ok {
		t.Fatalf("stale revision accepted: %v, %v", ok, err)
	}
	// Only the records that differ from the base are written: the point is
	// deleted, the unit renamed, and the other records kept unchanged.
	base := state
	next := state.Clone()
	next.Points = nil
	next.Units[0].Name = "改名单位"
	next.Buildings = append(next.Buildings, model.SiteBuilding{SiteRecord: model.SiteRecord{ID: "b2", Version: 1}, UnitID: "u1", Name: "2 号楼"})
	if changes := model.SiteChanges(base, next); len(changes) != 3 {
		t.Fatalf("changes %+v", changes)
	}
	if base.Units[0].Name != "示例单位" {
		t.Fatal("a change modified the base snapshot")
	}
	if ok, err := repo.SaveSiteState(ctx, tenant, base, next); err != nil || !ok {
		t.Fatalf("second save: %v, %v", ok, err)
	}
	state.Revision = 2
	actual, err = repo.LoadSiteState(ctx, tenant)
	if err != nil || actual.Revision != 2 || len(actual.Points) != 0 || actual.Units[0].Name != "改名单位" || len(actual.Buildings) != 2 || actual.Floors[0].Plan == nil || actual.Floors[0].Plan.SHA256 != "abc" {
		t.Fatalf("diff save: %+v, %v", actual, err)
	}
	if other, err := repo.LoadSiteState(ctx, tenant+"-other"); err != nil || len(other.Units) != 0 {
		t.Fatalf("another tenant exposed sites: %+v, %v", other, err)
	}
}
