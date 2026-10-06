package repositorytest

import (
	"context"
	"fmt"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// ListContract checks the list semantics both stores share: device pages hold
// 20 rows by default and at most 100, and an empty tenant lists nothing
// instead of every tenant.
func ListContract(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const tenant = "list-contract"
	for i := 0; i < 105; i++ {
		d := model.ManagedDevice{ID: fmt.Sprintf("lc-%03d", i), TenantID: tenant, ProductID: "p", Name: "设备", Status: "ENABLED", AccessKey: fmt.Sprintf("list-contract-%03d", i), SecretHash: "h", CreatedAt: 1, UpdatedAt: 1}
		if err := repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveProduct(ctx, model.Product{ID: "p", TenantID: tenant, Name: "产品", Status: "ENABLED", CreatedAt: 1, UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		limit, want int
	}{{0, 20}, {-1, 20}, {500, 100}, {7, 7}} {
		items, total, err := repo.ListManagedDevicesPage(ctx, tenant, tc.limit, 0)
		if err != nil || total != 105 || len(items) != tc.want {
			t.Fatalf("page limit %d: %d rows of %d, want %d (%v)", tc.limit, len(items), total, tc.want, err)
		}
		filtered, total, err := repo.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: tenant}, tc.limit, -3)
		if err != nil || total != 105 || len(filtered) != tc.want {
			t.Fatalf("filtered limit %d: %d rows of %d, want %d (%v)", tc.limit, len(filtered), total, tc.want, err)
		}
	}
	if devices, err := repo.ListManagedDevices(ctx, ""); err != nil || len(devices) != 0 {
		t.Fatalf("empty tenant listed %d devices (%v)", len(devices), err)
	}
	if products, err := repo.ListProducts(ctx, ""); err != nil || len(products) != 0 {
		t.Fatalf("empty tenant listed %d products (%v)", len(products), err)
	}
	if products, err := repo.ListProducts(ctx, tenant); err != nil || len(products) != 1 {
		t.Fatalf("tenant products %d (%v)", len(products), err)
	}
}
