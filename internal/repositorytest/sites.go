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
	if ok, err := repo.SaveSiteState(ctx, tenant, state); err != nil || !ok {
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
	stale := state
	stale.Revision = 0
	if ok, err := repo.SaveSiteState(ctx, tenant, stale); err != nil || ok {
		t.Fatalf("stale revision accepted: %v, %v", ok, err)
	}
	state.Points = nil
	state.Units[0].Name = "改名单位"
	if ok, err := repo.SaveSiteState(ctx, tenant, state); err != nil || !ok {
		t.Fatalf("second save: %v, %v", ok, err)
	}
	state.Revision = 2
	actual, err = repo.LoadSiteState(ctx, tenant)
	if err != nil || actual.Revision != 2 || len(actual.Points) != 0 || actual.Units[0].Name != "改名单位" {
		t.Fatalf("diff save: %+v, %v", actual, err)
	}
	if other, err := repo.LoadSiteState(ctx, tenant+"-other"); err != nil || len(other.Units) != 0 {
		t.Fatalf("another tenant exposed sites: %+v, %v", other, err)
	}
}
