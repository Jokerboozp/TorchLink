package repositorytest

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"sync/atomic"
	"testing"
)

func CatalogCamera(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	v := model.VideoCameraMapping{TenantID: "catalog-tenant", CameraID: "34020000001310000001", CameraName: "Catalog camera", VideoPlatformID: "gb28181/node/recorder", Enabled: true}
	var wg sync.WaitGroup
	var winners atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := r.CreateCatalogCamera(ctx, v)
			if err != nil {
				t.Error(err)
			}
			if created {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("duplicate imports", winners.Load())
	}
	stored, err := r.GetVideoCameraMapping(ctx, v.TenantID, v.CameraID)
	if err != nil || stored.CameraName != v.CameraName || stored.DeviceID != "" {
		t.Fatal("stored catalog camera", stored, err)
	}
	stored.CameraName = "User name"
	stored.DeviceID = "user-device"
	if err := r.SaveVideoCameraMapping(ctx, stored); err != nil {
		t.Fatal(err)
	}
	if created, err := r.CreateCatalogCamera(ctx, v); err != nil || created {
		t.Fatal("repeat import", created, err)
	}
	stored, err = r.GetVideoCameraMapping(ctx, v.TenantID, v.CameraID)
	if err != nil || stored.CameraName != "User name" || stored.DeviceID != "user-device" {
		t.Fatal("user changes overwritten", stored, err)
	}
	v.TenantID = "catalog-other"
	if created, err := r.CreateCatalogCamera(ctx, v); err != nil || !created {
		t.Fatal("tenant isolation", created, err)
	}
}
