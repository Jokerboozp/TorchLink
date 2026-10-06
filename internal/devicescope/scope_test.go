package devicescope

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

func TestScopeCoversGrantedDevicesAndUnits(t *testing.T) {
	state := model.SiteState{
		Units:  []model.SiteUnit{{SiteRecord: model.SiteRecord{ID: "unit"}}},
		Points: []model.SitePoint{{SiteRecord: model.SiteRecord{ID: "point"}, UnitID: "unit", DeviceID: "placed"}},
	}
	scope := Scope{Tenant: "t", IDs: map[string]bool{"granted": true}, Units: map[string]bool{"unit": true}, Sites: NewUnitIndex(state)}
	ctx := With(context.Background(), scope)
	for id, want := range map[string]bool{"granted": true, "placed": true, "other": false} {
		if got := Allowed(ctx, "t", id); got != want {
			t.Errorf("Allowed(%s) = %v, want %v", id, got, want)
		}
	}
	if Allowed(ctx, "other-tenant", "granted") {
		t.Fatal("a scope never covers another tenant")
	}
	if got := GrantedIDs(ctx, "t"); len(got) != 2 || got[0] != "granted" || got[1] != "placed" {
		t.Fatalf("GrantedIDs = %v", got)
	}
	if !Limited(ctx) || Limited(context.Background()) || Limited(With(context.Background(), Scope{Tenant: "t", All: true})) {
		t.Fatal("only a scope without All limits a request")
	}
	if !Allowed(context.Background(), "t", "anything") {
		t.Fatal("requests without a scope are not limited")
	}
}

func TestWrapIsIdempotentAndHidesOtherDevices(t *testing.T) {
	base := memory.NewRepository()
	ctx := context.Background()
	for _, id := range []string{"mine", "theirs"} {
		if err := base.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: "t", Name: id, AccessKey: "key-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	repo := Wrap(base)
	if Wrap(repo) != repo || Unscoped(repo) != base || Unscoped(base) != base {
		t.Fatal("Wrap must wrap once and Unscoped must return the inner repository")
	}
	limited := With(ctx, Scope{Tenant: "t", IDs: map[string]bool{"mine": true}})
	if _, err := repo.GetManagedDevice(limited, "t", "theirs"); !errors.Is(err, ErrDenied) {
		t.Fatalf("GetManagedDevice outside the scope = %v, want ErrDenied", err)
	}
	rows, total, err := repo.ListManagedDevicesPage(limited, "t", 20, 0)
	if err != nil || total != 1 || rows[0].ID != "mine" {
		t.Fatalf("ListManagedDevicesPage = %v %d %v", rows, total, err)
	}
}
