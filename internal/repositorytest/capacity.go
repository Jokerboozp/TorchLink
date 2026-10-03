package repositorytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type CapacityRepository interface {
	ports.Repository
	ports.CapacityDataCleaner
	ports.AccessStore
}

func capacityProduct(id string) model.Product {
	return model.Product{TenantID: "t", ID: id, Name: model.CapacityFixtureProductName(id), ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED", Description: model.CapacityFixtureDescription + "，用于容量测试设备"}
}

func capacityDevice(tenant, product, id string) model.ManagedDevice {
	return model.ManagedDevice{TenantID: tenant, ID: id, ProductID: product, Name: "容量测试 " + id, RegistrationSource: "ONBOARDING", Status: "ENABLED", AccessKey: "key-" + tenant + "-" + id}
}

// CapacityFixtureRecognition lists only tool-created products and devices.
func CapacityFixtureRecognition(t *testing.T, r CapacityRepository) {
	t.Helper()
	ctx := context.Background()
	fixture := capacityProduct("cap-standard")
	business := model.Product{TenantID: "t", ID: "cap-business", Name: "容量测试标准设备 cap-business", ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED", Description: "现场温感"}
	other := capacityProduct("other-cap")
	other.TenantID = "other"
	for _, p := range []model.Product{fixture, business, other} {
		if err := r.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	devices := []model.ManagedDevice{capacityDevice("t", fixture.ID, "cap-000001"), capacityDevice("t", fixture.ID, "cap-000002"), capacityDevice("t", business.ID, "business-1")}
	renamed := capacityDevice("t", fixture.ID, "cap-000003")
	renamed.Name = "三楼烟感"
	for _, d := range append(devices, renamed) {
		if err := r.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	products, err := r.ListCapacityFixtureProducts(ctx, "t")
	if err != nil || len(products) != 1 || products[0].ProductID != fixture.ID || products[0].DeviceCount != 3 {
		t.Fatal("fixture product listing", products, err)
	}
	ids, err := r.ListCapacityFixtureDevices(ctx, "t", fixture.ID, "", 1)
	if err != nil || len(ids) != 1 || ids[0] != "cap-000001" {
		t.Fatal("first device page", ids, err)
	}
	ids, err = r.ListCapacityFixtureDevices(ctx, "t", fixture.ID, ids[0], 10)
	if err != nil || len(ids) != 1 || ids[0] != "cap-000002" {
		t.Fatal("repurposed device listed as fixture", ids, err)
	}
	if _, err = r.ListCapacityFixtureDevices(ctx, "t", business.ID, "", 10); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("business product treated as fixture", err)
	}
	if ids, err = r.ListCapacityFixtureDevices(ctx, "t", "missing", "", 10); err != nil || len(ids) != 0 {
		t.Fatal("missing product", ids, err)
	}
	for _, q := range []model.CapacityCleanupBatch{
		{Product: business.ID, Devices: []string{"business-1"}},
		{Product: fixture.ID, Devices: []string{renamed.ID}},
		{Devices: []string{"cap-000001"}},
	} {
		if _, err = r.CleanupCapacityData(ctx, "t", q); !errors.Is(err, model.ErrResourceInUse) {
			t.Fatal("cleanup accepted a non-fixture scope", q, err)
		}
	}
}

// CapacityFixtureCleanup removes fixture devices with all their data, run
// tasks by their marker and finally the empty product.
func CapacityFixtureCleanup(t *testing.T, r CapacityRepository) {
	t.Helper()
	ctx := context.Background()
	run := "cap-20261002-120000-abcdef"
	p := capacityProduct("cap-standard")
	if err := r.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"t", "other"} {
		for _, id := range []string{"d1", "d2"} {
			if err := r.SaveManagedDevice(ctx, capacityDevice(tenant, p.ID, id)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := r.SaveRule(ctx, model.AlarmRule{TenantID: "t", ID: "cap-stress-alarm", ProductID: p.ID, Name: "容量测试告警", AlarmType: "CAPACITY_TEST", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	jobs := []model.HealthInspectionJob{{TenantID: "t", ID: "owned", CapacityRunID: run, Status: "succeeded", StartedAt: 1, Report: model.DeviceHealthReport{GeneratedAt: 111}}, {TenantID: "t", ID: "business", Status: "succeeded", StartedAt: 2, Report: model.DeviceHealthReport{GeneratedAt: 222}}}
	for _, job := range jobs {
		if ok, err := r.CreateHealthInspectionJob(ctx, job); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	for _, job := range []model.ReplayRequest{{TenantID: "t", ID: "owned-replay", CapacityRunID: run, Status: "COMPLETED"}, {TenantID: "t", ID: "other-run-replay", CapacityRunID: "cap-20261003-120000-fedcba", Status: "COMPLETED"}, {TenantID: "t", ID: "business-replay", DeviceID: "d1", Status: "COMPLETED"}} {
		if err := r.SaveReplay(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	for _, audit := range []model.AuditLog{{TenantID: "t", ID: "audit-device", TargetType: "device", TargetID: "d1"}, {TenantID: "t", ID: "audit-inspection", TargetType: "device-health", TargetID: "inspection_111"}, {TenantID: "t", ID: "audit-business", TargetType: "device-health", TargetID: "inspection_222"}, {TenantID: "other", ID: "audit-other", TargetType: "device", TargetID: "d1"}} {
		if err := r.SaveAudit(ctx, audit); err != nil {
			t.Fatal(err)
		}
	}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "all", DeviceScope: "all", DeviceIDs: []string{"d1", "business"}, SessionVersion: 20}, {Username: "selected", DeviceScope: "selected", DeviceIDs: []string{"d1", "business"}, SessionVersion: 30}}, Roles: []model.PlatformRole{{ID: "role", DeviceScope: "selected", DeviceIDs: []string{"d2", "business"}}}}
	if ok, err := r.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := r.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", ProductID: p.ID, DeviceID: "d1", MessageID: "raw", ObjectBucket: "postgres"}); err != nil {
		t.Fatal(err)
	}
	q := model.CapacityCleanupBatch{RunID: run, Product: p.ID, Devices: []string{"d1", "d2"}}
	// Unparsed reports keep every device until processing finishes.
	if _, err := r.CleanupCapacityData(ctx, "t", q); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("cleanup accepted unprocessed data", err)
	}
	if err := r.MarkRawParseResult(ctx, "t", "raw", 1, ""); err != nil {
		t.Fatal(err)
	}
	msg := model.StandardMessage{TenantID: "t", ProductID: p.ID, DeviceID: "d1", MessageID: "standard", RawMessageID: "raw", MessageType: model.PropertyReport}
	claim, err := r.ClaimStandardMessage(ctx, msg, "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.MarkStandardMessageProcessed(ctx, "t", msg.MessageID, claim.Token); err != nil {
		t.Fatal(err)
	}
	if err = r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "d2"}); err != nil {
		t.Fatal(err)
	}
	if _, err = r.CleanupCapacityData(ctx, "t", q); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("camera reference ignored", err)
	}
	if err = r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera"}); err != nil {
		t.Fatal(err)
	}
	n, err := r.CleanupCapacityData(ctx, "t", q)
	if err != nil || n.Devices != 2 || n.Raw != 1 || n.Standard != 1 || n.Resources != 2 || n.Audits != 2 || n.AccessReferences != 3 {
		t.Fatal("device cleanup", n, err)
	}
	if _, err = r.HealthInspectionPage(ctx, "t", "owned", 20, 0); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("run inspection retained", err)
	}
	if _, err = r.HealthInspectionPage(ctx, "t", "business", 20, 0); err != nil {
		t.Fatal("unmarked inspection deleted", err)
	}
	for _, id := range []string{"other-run-replay", "business-replay"} {
		if _, err = r.GetReplay(ctx, id); err != nil {
			t.Fatal("replay outside this run deleted", id, err)
		}
	}
	if _, err = r.GetManagedDevice(ctx, "other", "d1"); err != nil {
		t.Fatal("other tenant device deleted", err)
	}
	access, err := r.LoadAccessState(ctx, "t")
	if err != nil || access.Users[0].SessionVersion != 20 || access.Users[1].SessionVersion != 31 || len(access.Users[0].DeviceIDs) != 1 || len(access.Roles[0].DeviceIDs) != 1 || access.Revision != 2 {
		t.Fatal("access references/session scope", access, err)
	}
	n, err = r.CleanupCapacityData(ctx, "t", model.CapacityCleanupBatch{AllRuns: true, Product: p.ID, RemoveProduct: true})
	if err != nil || n.Products != 1 || n.Rules != 1 || n.Resources != 1 {
		t.Fatal("final product, rule and remaining run task cleanup", n, err)
	}
	if _, err = r.GetProduct(ctx, "t", p.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("fixture product retained", err)
	}
	if _, err = r.GetReplay(ctx, "business-replay"); err != nil {
		t.Fatal("unmarked replay deleted", err)
	}
	// Removing an already removed product is a no-op for retries.
	if n, err = r.CleanupCapacityData(ctx, "t", model.CapacityCleanupBatch{Product: p.ID, RemoveProduct: true}); err != nil || n.Products != 0 {
		t.Fatal("retry after removal", n, err)
	}
}

// CapacityProductKeptWhileShared keeps a product that still has devices or
// business references, reporting why instead of failing.
func CapacityProductKeptWhileShared(t *testing.T, r CapacityRepository) {
	t.Helper()
	ctx := context.Background()
	p := capacityProduct("cap-shared")
	if err := r.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveManagedDevice(ctx, capacityDevice("t", p.ID, "kept")); err != nil {
		t.Fatal(err)
	}
	n, err := r.CleanupCapacityData(ctx, "t", model.CapacityCleanupBatch{Product: p.ID, RemoveProduct: true})
	if err != nil || n.Products != 0 || len(n.Warnings) != 1 {
		t.Fatal("product with remaining devices", n, err)
	}
	if _, err = r.CleanupCapacityData(ctx, "t", model.CapacityCleanupBatch{Product: p.ID, Devices: []string{"kept"}}); err != nil {
		t.Fatal(err)
	}
	if err = r.SaveRule(ctx, model.AlarmRule{TenantID: "t", ID: "business-rule", ProductID: p.ID, Name: "业务规则", AlarmType: "FIRE", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	n, err = r.CleanupCapacityData(ctx, "t", model.CapacityCleanupBatch{Product: p.ID, RemoveProduct: true})
	if err != nil || n.Products != 0 || len(n.Warnings) != 1 {
		t.Fatal("product with business rule", n, err)
	}
	if _, err = r.GetProduct(ctx, "t", p.ID); err != nil {
		t.Fatal("referenced product removed", err)
	}
}
