package repositorytest

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// DeviceOverview checks the store's device and state counts, including
// unregistered states and a device restriction.
func DeviceOverview(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const tenant = "overview"
	for i := range 6 {
		d := model.ManagedDevice{ID: fmt.Sprintf("dev%d", i), TenantID: tenant, ProductID: "p", Name: fmt.Sprintf("设备%d", i), Status: []string{"ENABLED", "DISABLED"}[i%2],
			DeviceRole: []string{"DIRECT", "GATEWAY", ""}[i%3], AutoRegistered: i == 4, AccessKey: fmt.Sprintf("overview-ak-%d", i), SecretHash: "h", CreatedAt: 1, UpdatedAt: 1}
		if err := repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	// dev5 has never reported; ghost is a state without a registered device.
	for i, id := range []string{"dev0", "dev1", "dev2", "dev3", "dev4", "ghost"} {
		s := model.DeviceState{TenantID: tenant, DeviceID: id, ProductID: "p", BusinessStatus: []string{"ONLINE", "OFFLINE", "ALARM"}[i%3], ConnectionStatus: []string{"CONNECTED", ""}[i%2],
			DataStatus: "ACTIVE", LastSeenAt: int64(1000 + i), ReportIntervalSec: 300, OfflineToleranceSec: 60}
		if err := repo.UpsertDeviceState(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.DeviceOverviewCounts(ctx, tenant, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := model.DeviceOverview{Total: 6, ByStatus: map[string]int{"ENABLED": 3, "DISABLED": 3}, ByRole: map[string]int{"DIRECT": 2, "GATEWAY": 2, "UNKNOWN": 2}, AutoRegistered: 1,
		Reported: 5, DiscoveredUnregistered: 1, ConnectionStatus: map[string]int{"CONNECTED": 3, "UNKNOWN": 2}, DataStatus: map[string]int{"ACTIVE": 5},
		BusinessStatus: map[string]int{"ONLINE": 2, "OFFLINE": 2, "ALARM": 1}, LatestSeenAt: 1004}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overview\n got %+v\nwant %+v", got, want)
	}
	scoped, err := repo.DeviceOverviewCounts(ctx, tenant, true, []string{"dev1", "dev5"})
	if err != nil {
		t.Fatal(err)
	}
	if scoped.Total != 2 || scoped.Reported != 1 || scoped.DiscoveredUnregistered != 0 || scoped.BusinessStatus["OFFLINE"] != 1 || scoped.LatestSeenAt != 1001 {
		t.Fatalf("scoped overview %+v", scoped)
	}
	if none, err := repo.DeviceOverviewCounts(ctx, tenant, true, nil); err != nil || none.Total != 0 || none.Reported != 0 {
		t.Fatalf("empty restriction %+v %v", none, err)
	}
}
