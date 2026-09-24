package memory

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
)

func TestDeleteResourceBlocksReferencesAndKeepsTenantsSeparate(t *testing.T) {
	ctx := context.Background()
	r := NewRepository()
	for _, tenant := range []string{"one", "two"} {
		_ = r.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "product"})
		_ = r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: "device", ProductID: "product"})
	}
	if err := r.DeleteResource(ctx, "one", "product", "product"); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("product with device: %v", err)
	}
	if err := r.DeleteResource(ctx, "one", "device", "device"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteResource(ctx, "one", "product", "product"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetProduct(ctx, "two", "product"); err != nil {
		t.Fatalf("other tenant changed: %v", err)
	}
	if err := r.DeleteResource(ctx, "one", "device", "device"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("repeat delete: %v", err)
	}
}

func TestDeleteResourceChecksProfileProtocolCameraAndAlarm(t *testing.T) {
	ctx := context.Background()
	r := NewRepository()
	_ = r.SaveProtocolDefinition(ctx, model.ProtocolDefinition{TenantID: "t", ID: "proto"})
	_ = r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "profile", ProtocolID: "proto"})
	if err := r.DeleteResource(ctx, "t", "protocol", "proto"); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("bound protocol: %v", err)
	}
	if err := r.DeleteResource(ctx, "t", "profile", "profile"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteResource(ctx, "t", "protocol", "proto"); err != nil {
		t.Fatal(err)
	}
	_ = r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "device"})
	_ = r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "device"})
	if err := r.DeleteResource(ctx, "t", "device", "device"); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("mapped device: %v", err)
	}
	if err := r.DeleteResource(ctx, "t", "camera", "camera"); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("mapped camera: %v", err)
	}
	_ = r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera"})
	if err := r.DeleteResource(ctx, "t", "camera", "camera"); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteResource(ctx, "t", "device", "device"); err != nil {
		t.Fatal(err)
	}
	_, _, _ = r.UpsertAlarm(ctx, model.Alarm{TenantID: "t", ID: "alarm", Status: "ACTIVE"})
	if err := r.DeleteResource(ctx, "t", "alarm", "alarm"); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("active alarm: %v", err)
	}
	_ = r.UpdateAlarm(ctx, model.Alarm{TenantID: "t", ID: "alarm", Status: "CLOSED"})
	if err := r.DeleteResource(ctx, "t", "alarm", "alarm"); err != nil {
		t.Fatal(err)
	}
}
