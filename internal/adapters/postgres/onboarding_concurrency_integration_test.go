package postgres

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"iot-platform/internal/model"
)

// Plain enrollments into one product run in parallel, while two tenants that
// reserve the same listener port at once still get exactly one reservation.
func TestOnboardingParallelEnrollmentAndExclusivePort(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	if err := r.SaveProduct(ctx, model.Product{TenantID: "tenant-a", ID: "sensor", Name: "sensor", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := model.ManagedDevice{TenantID: "tenant-a", ID: fmt.Sprintf("device-%d", i), ProductID: "sensor", Name: "d", Status: "ENABLED", AccessKey: fmt.Sprintf("dk_%d", i)}
			errs <- r.SaveOnboarding(ctx, model.OnboardingBundle{Device: d})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("parallel enrollment failed: %v", err)
		}
	}

	results := make(chan error, 2)
	for _, tenant := range []string{"tenant-b", "tenant-c"} {
		if err := r.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "gateway", Name: "gateway", Status: "ENABLED"}); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(tenant string) {
			defer wg.Done()
			results <- r.SaveOnboarding(ctx, model.OnboardingBundle{
				Device:  model.ManagedDevice{TenantID: tenant, ID: "gw", ProductID: "gateway", Name: "gw", Status: "ENABLED", AccessKey: "dk_" + tenant},
				Binding: &model.ProductProtocolBinding{TenantID: tenant, ProductID: "gateway", ProtocolID: "gb", Version: "1.0.0"},
				Profile: &model.DeviceAccessProfile{TenantID: tenant, ID: "listener-" + tenant, ProductID: "gateway", ProtocolID: "gb", ProtocolVersion: "1.0.0", Mode: "listener", Network: "tcp", Port: 26999, Enabled: true},
			})
		}(tenant)
	}
	wg.Wait()
	close(results)
	succeeded, reserved := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case strings.Contains(err.Error(), "listener port is already reserved"):
			reserved++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 || reserved != 1 {
		t.Fatalf("one listener must win the port: succeeded=%d reserved=%d", succeeded, reserved)
	}
}
