package repositorytest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// PreparedEnrollment models the gap between planning a device and committing
// it: configuration or acceptance changes in that gap must reject the write.
func PreparedEnrollment(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	for _, field := range []string{"unchanged", "product", "shared-profile", "record-revision", "runtime-status"} {
		t.Run(field, func(t *testing.T) {
			tenant := "prepared-" + field
			p := model.Product{TenantID: tenant, ID: "template", Name: "标准设备模板", Status: "ENABLED", ProtocolPackageID: "iot-standard@1.0.0", Transport: "HTTP", PayloadFormat: "json"}
			if err := repo.SaveProduct(ctx, p); err != nil {
				t.Fatal(err)
			}
			profile := model.DeviceAccessProfile{TenantID: tenant, ID: "shared", ProductID: p.ID, Mode: "listener", Network: "tcp", Port: 29005, Host: "127.0.0.1"}
			if err := repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
				t.Fatal(err)
			}
			rec, err := repo.SaveOnboardingRecord(ctx, model.OnboardingRecord{TenantID: tenant, ID: "template:" + p.ID, OwnerID: "template", Kind: "template-preparation", Status: "READY", Body: json.RawMessage(`{"status":"READY"}`)}, 0)
			if err != nil {
				t.Fatal(err)
			}
			bundle := model.OnboardingBundle{Prepared: &model.PreparedEnrollment{Product: p, Profiles: []model.DeviceAccessProfile{profile}, RecordRevision: rec.Revision}, Device: model.ManagedDevice{TenantID: tenant, ID: "device", ProductID: p.ID, Name: "新设备", Status: "ENABLED", AccessKey: tenant + "-key"}}
			switch field {
			case "product":
				p.Name = "读取后被修改"
				err = repo.SaveProduct(ctx, p)
			case "shared-profile":
				profile.Port++
				err = repo.SaveDeviceAccessProfile(ctx, profile)
			case "record-revision":
				// Even a fresh READY result belongs to a new acceptance revision.
				_, err = repo.SaveOnboardingRecord(ctx, rec, rec.Revision)
			case "runtime-status":
				profile.RuntimeStatus = "LISTENING"
				profile.LastSuccessAt = 12345
				err = repo.SaveDeviceAccessProfile(ctx, profile)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = repo.SaveOnboarding(ctx, bundle)
			wantSuccess := field == "unchanged" || field == "runtime-status"
			if wantSuccess {
				if err != nil {
					t.Fatal("unchanged prepared enrollment rejected", err)
				}
				if d, e := repo.GetManagedDevice(ctx, tenant, bundle.Device.ID); e != nil || d.ProductID != p.ID {
					t.Fatal(d, e)
				}
			} else {
				if !errors.Is(err, model.ErrOnboardingChanged) {
					t.Fatal("stale preparation accepted", err)
				}
				if _, e := repo.GetManagedDevice(ctx, tenant, bundle.Device.ID); !errors.Is(e, model.ErrNotFound) {
					t.Fatal("failed prepared enrollment persisted a device", e)
				}
			}
		})
	}
}
