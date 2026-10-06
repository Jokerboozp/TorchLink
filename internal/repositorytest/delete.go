package repositorytest

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// DeleteResourceBlocksReferences checks that a referenced product, protocol,
// device, camera or active alarm is not deleted, that deletion stays inside
// its tenant and that a repeated delete reports not found.
func DeleteResourceBlocksReferences(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	for _, tenant := range []string{"del-one", "del-two"} {
		must(t, r.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "product", Name: "产品", Status: "ENABLED"}))
		must(t, r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: "device", ProductID: "product", Name: "设备", Status: "ENABLED", AccessKey: tenant + "-device", SecretHash: "h"}))
	}
	wantErr(t, "product with device", r.DeleteResource(ctx, "del-one", "product", "product"), model.ErrResourceInUse)
	must(t, r.DeleteResource(ctx, "del-one", "device", "device"))
	must(t, r.DeleteResource(ctx, "del-one", "product", "product"))
	if _, err := r.GetProduct(ctx, "del-two", "product"); err != nil {
		t.Fatalf("other tenant changed: %v", err)
	}
	wantErr(t, "repeat delete", r.DeleteResource(ctx, "del-one", "device", "device"), model.ErrNotFound)

	const tenant = "del-refs"
	must(t, r.SaveProtocolDefinition(ctx, model.ProtocolDefinition{TenantID: tenant, ID: "proto", Name: "协议"}))
	must(t, r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: tenant, ID: "profile", ProtocolID: "proto"}))
	wantErr(t, "bound protocol", r.DeleteResource(ctx, tenant, "protocol", "proto"), model.ErrResourceInUse)
	must(t, r.DeleteResource(ctx, tenant, "profile", "profile"))
	must(t, r.DeleteResource(ctx, tenant, "protocol", "proto"))
	must(t, r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: "device", ProductID: "p", Name: "设备", Status: "ENABLED", AccessKey: tenant + "-device", SecretHash: "h"}))
	must(t, r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: tenant, CameraID: "camera", DeviceID: "device"}))
	wantErr(t, "mapped device", r.DeleteResource(ctx, tenant, "device", "device"), model.ErrResourceInUse)
	wantErr(t, "mapped camera", r.DeleteResource(ctx, tenant, "camera", "camera"), model.ErrResourceInUse)
	must(t, r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: tenant, CameraID: "camera"}))
	must(t, r.DeleteResource(ctx, tenant, "camera", "camera"))
	must(t, r.DeleteResource(ctx, tenant, "device", "device"))
	if _, _, err := r.UpsertAlarm(ctx, model.Alarm{TenantID: tenant, ID: "alarm", RuleID: "rule", DeviceID: "device", Status: "ACTIVE", AlarmLevel: "HIGH"}); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "active alarm", r.DeleteResource(ctx, tenant, "alarm", "alarm"), model.ErrResourceInUse)
	must(t, r.UpdateAlarm(ctx, model.Alarm{TenantID: tenant, ID: "alarm", RuleID: "rule", DeviceID: "device", Status: "CLOSED", AlarmLevel: "HIGH"}))
	must(t, r.DeleteResource(ctx, tenant, "alarm", "alarm"))
}

// DeleteProtocolReleaseKeepsOtherVersions checks that a release used as a
// product's rollback version or by an access profile is kept, and that
// deleting one version leaves the others.
func DeleteProtocolReleaseKeepsOtherVersions(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const tenant = "del-release"
	for _, version := range []string{"1.0.0", "2.0.0"} {
		must(t, r.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: tenant, ProtocolID: "protocol", Version: version, Status: "PUBLISHED"}))
	}
	must(t, r.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: tenant, ProductID: "product", ProtocolID: "protocol", Version: "2.0.0", PreviousVersion: "1.0.0"}))
	wantErr(t, "rollback version", r.DeleteProtocolRelease(ctx, tenant, "protocol", "1.0.0"), model.ErrResourceInUse)
	must(t, r.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: tenant, ProductID: "product", ProtocolID: "protocol", Version: "2.0.0"}))
	must(t, r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: tenant, ID: "profile", ProtocolID: "protocol", ProtocolVersion: "1.0.0"}))
	wantErr(t, "profile version", r.DeleteProtocolRelease(ctx, tenant, "protocol", "1.0.0"), model.ErrResourceInUse)
	must(t, r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: tenant, ID: "profile", ProtocolID: "protocol", ProtocolVersion: "2.0.0"}))
	must(t, r.DeleteProtocolRelease(ctx, tenant, "protocol", "1.0.0"))
	if _, err := r.GetProtocolRelease(ctx, tenant, "protocol", "2.0.0"); err != nil {
		t.Fatalf("other version was deleted: %v", err)
	}
	wantErr(t, "repeat delete", r.DeleteProtocolRelease(ctx, tenant, "protocol", "1.0.0"), model.ErrNotFound)
}

// DeleteAlarmRemovesEveryAnalysisScope checks that deleting an alarm removes
// its AI analyses in every knowledge scope and no other alarm's.
func DeleteAlarmRemovesEveryAnalysisScope(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const tenant = "del-analysis"
	for _, id := range []string{"alarm-1", "alarm-10"} {
		if _, _, err := r.UpsertAlarm(ctx, model.Alarm{TenantID: tenant, ID: id, RuleID: id, DeviceID: "device", Status: "CLOSED", AlarmLevel: "HIGH"}); err != nil {
			t.Fatal(err)
		}
	}
	scopes := []string{model.AIAnalysisScopeNone, model.AlarmAnalysisWorkflowID, model.AIAnalysisScopeLegacyTenant}
	for _, scope := range scopes {
		must(t, r.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: tenant, AlarmID: "alarm-1", KnowledgeScope: scope}))
	}
	must(t, r.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: tenant, AlarmID: "alarm-10"}))
	must(t, r.DeleteResource(ctx, tenant, "alarm", "alarm-1"))
	for _, scope := range scopes {
		if _, err := r.GetAIAnalysis(ctx, tenant, "alarm-1", scope); err == nil {
			t.Fatalf("analysis scope %q survived alarm deletion", scope)
		}
	}
	if _, err := r.GetAIAnalysis(ctx, tenant, "alarm-10", model.AIAnalysisScopeNone); err != nil {
		t.Fatalf("deleting alarm-1 removed another alarm's analysis: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: got %v, want %v", what, err, want)
	}
}
