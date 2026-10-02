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
	ports.CapacityFixtureLister
	ports.CapacityDataCleaner
	ports.AccessStore
}

func CapacityFixtureRecognition(t *testing.T, r CapacityRepository) {
	t.Helper()
	ctx := context.Background()
	legacy := model.Product{TenantID: "t", ID: "a-old", Name: "容量测试标准设备", ProtocolPackageID: "iot-standard@1.0.0", Status: "DISABLED", Metadata: map[string]any{"source": "CAP"}}
	canonical := model.Product{TenantID: "t", ID: "z-new", Name: "容量测试标准设备 z-new", ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED", Description: "capacity-test 自动创建，用于容量测试设备"}
	for _, p := range []model.Product{legacy, canonical, {TenantID: "t", ID: "cap-business", Name: "正常温感", ProtocolPackageID: "iot-standard@1.0.0", Status: "DISABLED"}, {TenantID: "t", ID: "legacy-enabled", Name: "容量测试", ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED", Metadata: map[string]any{"source": "CAP"}}, {TenantID: "other", ID: "other-cap", Name: "容量测试标准设备 other-cap", ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED", Description: "capacity-test 自动创建"}} {
		if err := r.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if err := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ProductID: legacy.ID, ID: id, Name: "压测设备 " + id, RegistrationSource: "ONBOARDING", Status: "ENABLED", AccessKey: "recognition-" + id, UpdatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.ListCapacityFixtureProducts(ctx, "t", "", 1)
	if err != nil || len(page) != 1 || page[0].ProductID != "a-old" || page[0].DeviceCount != 3 || page[0].BlockedReason != "" {
		t.Fatal(page, err)
	}
	first := page[0]
	page, err = r.ListCapacityFixtureProducts(ctx, "t", first.ProductID, 1)
	if err != nil || len(page) != 1 || page[0].ProductID != "z-new" {
		t.Fatal("product keyset/ownership", page, err)
	}
	if err = r.PrepareCapacityFixture(ctx, "t", canonical.ID, page[0].Fingerprint); err != nil {
		t.Fatal("canonical enabled fixture not preparable", err)
	}
	p, err := r.GetProduct(ctx, "t", canonical.ID)
	if err != nil || p.Status != "DISABLED" {
		t.Fatal("fixture not disabled", p, err)
	}
	if err = r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ProductID: canonical.ID, ID: "normal-in-fixture", Name: "普通现场设备", Status: "ENABLED", AccessKey: "normal-in-fixture"}); err != nil {
		t.Fatal(err)
	}
	changed, err := r.ListCapacityFixtureProducts(ctx, "t", legacy.ID, 1)
	if err != nil || len(changed) != 1 || changed[0].BlockedReason == "" {
		t.Fatal("missing ownership fields did not block", changed, err)
	}
	ids, err := r.ListCapacityFixtureDevices(ctx, "t", legacy.ID, "", 2)
	if err != nil || len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatal(ids, err)
	}
	ids, err = r.ListCapacityFixtureDevices(ctx, "t", legacy.ID, "b", 2)
	if err != nil || len(ids) != 1 || ids[0] != "c" {
		t.Fatal("device keyset", ids, err)
	}
	d, _ := r.GetManagedDevice(ctx, "t", "b")
	d.UpdatedAt++
	if err = r.SaveManagedDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err = r.PrepareCapacityFixture(ctx, "t", legacy.ID, first.Fingerprint); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("stale preview was accepted", err)
	}
	page, err = r.ListCapacityFixtureProducts(ctx, "t", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "legacy-profile", ProductID: legacy.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err = r.PrepareCapacityFixture(ctx, "t", legacy.ID, page[0].Fingerprint); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("active product profile ignored", err)
	}
	if err = r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "legacy-profile", ProductID: legacy.ID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err = r.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t", CameraID: "camera", DeviceID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err = r.PrepareCapacityFixture(ctx, "t", legacy.ID, page[0].Fingerprint); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("camera reference ignored", err)
	}
	q := model.CapacityCleanupBatch{Product: "cap-business", Historical: true, Devices: []string{"manual"}, RemoveDevices: []string{"manual"}}
	if _, err = r.CleanupCapacityData(ctx, "t", q); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("name prefix allowed historical cleanup", err)
	}
}

func CapacityFixtureAssociatedCleanup(t *testing.T, r CapacityRepository) {
	t.Helper()
	ctx := context.Background()
	run := "cap-20261002-120000-abcdef"
	p := model.Product{TenantID: "t", ID: "old", Name: "容量测试修复回归设备", Status: "DISABLED", ProtocolPackageID: "iot-standard@1.0.0", Metadata: map[string]any{"source": "CAP"}}
	if err := r.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"t", "other"} {
		for _, id := range []string{"d1", "d2"} {
			if err := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: id, ProductID: p.ID, Name: "压测设备 " + id, RegistrationSource: "ONBOARDING", Status: "ENABLED", AccessKey: "associated-" + tenant + id}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "unused-product-profile", ProductID: p.ID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveRule(ctx, model.AlarmRule{TenantID: "t", ID: "test-rule", ProductID: p.ID, Name: "历史容量测试", AlarmType: "CAPACITY_TEST", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	items := []model.DeviceHealthItem{{DeviceID: "d1", DeviceName: "压测设备 d1", ProductID: p.ID}, {DeviceID: "d2", DeviceName: "压测设备 d2", ProductID: p.ID}}
	jobs := []model.HealthInspectionJob{{TenantID: "t", ID: "pure", Status: "succeeded", StartedAt: 1, Report: model.DeviceHealthReport{GeneratedAt: 111, Items: items}}, {TenantID: "t", ID: "mixed", CapacityRunID: run, Status: "succeeded", StartedAt: 2, Report: model.DeviceHealthReport{GeneratedAt: 222, Summary: "原有真实混合报告", AIAdvice: "原有AI意见", Items: []model.DeviceHealthItem{items[0], {DeviceID: "business", ProductID: "business", DeviceName: "现场烟感"}}}}}
	for _, job := range jobs {
		if ok, err := r.CreateHealthInspectionJob(ctx, job); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	for _, job := range []model.ReplayRequest{{TenantID: "t", ID: "fixture-replay", DeviceID: "d1", ProductID: p.ID, Status: "COMPLETED"}, {TenantID: "t", ID: "global-replay", Status: "COMPLETED"}, {TenantID: "other", ID: "other-replay", DeviceID: "d1", ProductID: p.ID, Status: "COMPLETED"}} {
		if err := r.SaveReplay(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	for _, audit := range []model.AuditLog{{TenantID: "t", ID: "audit-device", TargetType: "device", TargetID: "d1"}, {TenantID: "t", ID: "audit-inspection", TargetType: "device-health", TargetID: "inspection_111"}, {TenantID: "t", ID: "audit-mixed", TargetType: "device-health", TargetID: "inspection_222"}, {TenantID: "other", ID: "audit-other", TargetType: "device", TargetID: "d1"}} {
		if err := r.SaveAudit(ctx, audit); err != nil {
			t.Fatal(err)
		}
	}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "all", DeviceScope: "all", DeviceIDs: []string{"d1", "business"}, SessionVersion: 20}, {Username: "selected", DeviceScope: "selected", DeviceIDs: []string{"d1", "business"}, SessionVersion: 30}}, Roles: []model.PlatformRole{{ID: "role", DeviceScope: "selected", DeviceIDs: []string{"d2", "business"}}}}
	if ok, err := r.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := r.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", ProductID: p.ID, DeviceID: "d1", MessageID: "raw", ObjectBucket: "postgres", ParseAttemptedAt: 1}); err != nil {
		t.Fatal(err)
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
	q := model.CapacityCleanupBatch{RunID: run, Product: p.ID, Devices: []string{"d1", "d2"}, RemoveDevices: []string{"d1", "d2"}, Historical: true, Resources: []model.CapacityCleanupResource{{Kind: "inspection", ID: "mixed"}}}
	tooEarly := q
	tooEarly.Devices = nil
	tooEarly.RemoveDevices = nil
	tooEarly.Resources = nil
	tooEarly.RemoveProduct = true
	if _, err = r.CleanupCapacityData(ctx, "t", tooEarly); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("final cleanup accepted remaining devices", err)
	}
	if _, err = r.HealthInspectionPage(ctx, "t", "pure", 20, 0); err != nil {
		t.Fatal("failed final cleanup changed report", err)
	}
	active := model.HealthInspectionJob{TenantID: "t", ID: "active", Status: "running", StartedAt: 3}
	if ok, err := r.CreateHealthInspectionJob(ctx, active); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err = r.CleanupCapacityData(ctx, "t", q); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("direct cleanup bypassed active task", err)
	}
	if _, err = r.GetManagedDevice(ctx, "t", "d1"); err != nil {
		t.Fatal("failed cleanup partially deleted", err)
	}
	active.Status = "succeeded"
	if ok, err := r.UpdateRunningHealthInspectionJob(ctx, active); err != nil || !ok {
		t.Fatal(ok, err)
	}
	n, err := r.CleanupCapacityData(ctx, "t", q)
	if err != nil || n.Devices != 2 || n.Raw != 1 || n.Standard != 1 || n.Resources != 2 || n.Audits != 2 || n.AccessReferences != 3 || len(n.Warnings) != 1 {
		t.Fatal("associated cleanup", n, err)
	}
	if _, err = r.HealthInspectionPage(ctx, "t", "pure", 20, 0); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("pure report retained", err)
	}
	mixed, err := r.HealthInspectionPage(ctx, "t", "mixed", 20, 0)
	if err != nil || len(mixed.Report.Items) != 2 || mixed.Report.Summary != "原有真实混合报告" || mixed.Report.AIAdvice != "原有AI意见" {
		t.Fatal("mixed snapshot changed", mixed, err)
	}
	if _, err = r.GetReplay(ctx, "global-replay"); err != nil {
		t.Fatal("global unrelated replay deleted", err)
	}
	if _, err = r.GetReplay(ctx, "other-replay"); err != nil {
		t.Fatal("other tenant replay deleted", err)
	}
	if _, err = r.GetManagedDevice(ctx, "other", "d1"); err != nil {
		t.Fatal("other tenant device deleted", err)
	}
	access, err := r.LoadAccessState(ctx, "t")
	if err != nil || access.Users[0].SessionVersion != 20 || access.Users[1].SessionVersion != 31 || len(access.Users[0].DeviceIDs) != 1 || len(access.Roles[0].DeviceIDs) != 1 || access.Revision != 2 {
		t.Fatal("access references/session scope", access, err)
	}
	q.Devices = nil
	q.RemoveDevices = nil
	q.Resources = nil
	q.RemoveProduct = true
	n, err = r.CleanupCapacityData(ctx, "t", q)
	if err != nil || n.Products != 1 || n.Rules != 1 || n.Profiles != 1 {
		t.Fatal("final product/profile/rule cleanup", n, err)
	}
	if _, err = r.GetProduct(ctx, "t", p.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("fixture product retained", err)
	}
}

func CapacityPrivateProtocolCleanup(t *testing.T, r CapacityRepository, keep bool) {
	t.Helper()
	ctx := context.Background()
	protocol := "cap-private-gb26875"
	p := model.Product{TenantID: "t", ID: "gb-fixture", Name: "容量测试 GB26875", ProtocolPackageID: protocol + "@1.0.0", Status: "DISABLED", Metadata: map[string]any{"source": "CAP"}}
	if err := r.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveProtocolPackage(ctx, model.ProtocolPackage{TenantID: "t", ID: p.ProtocolPackageID, Protocol: protocol, Version: "1.0.0", ParserType: "go_protocol_parser", Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		r.SaveProtocolDefinition(ctx, model.ProtocolDefinition{TenantID: "t", ID: protocol, Name: "容量试验协议"}),
		r.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t", ProtocolID: protocol, Version: "1.0.0", Status: "PUBLISHED"}),
		r.CreatePointTableRelease(ctx, model.PointTableRelease{TenantID: "t", ProtocolID: protocol, Version: "1.0.0"}),
		r.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t", ProductID: p.ID, ProtocolID: protocol, Version: "1.0.0"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	d := model.ManagedDevice{TenantID: "t", ProductID: p.ID, ID: "gb26875_900100000000", Name: "GB26875 设备 900100000000", RegistrationSource: "PROTOCOL_AUTO", AutoRegistered: true, DeviceRole: "DIRECT", Status: "ENABLED", AccessKey: "gb-" + p.ID}
	if err := r.SaveManagedDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	page, err := r.ListCapacityFixtureProducts(ctx, "t", "", 10)
	if err != nil || len(page) != 1 || page[0].ProtocolID != protocol || page[0].BlockedReason != "" {
		t.Fatal("private GB ownership", page, err)
	}
	if err = r.PrepareCapacityFixture(ctx, "t", p.ID, page[0].Fingerprint); err != nil {
		t.Fatal(err)
	}
	q := model.CapacityCleanupBatch{Product: p.ID, Devices: []string{d.ID}, RemoveDevices: []string{d.ID}, Historical: true, RemoveProduct: true, KeepProtocol: keep}
	n, err := r.CleanupCapacityData(ctx, "t", q)
	if err != nil || n.Devices != 1 || n.Products != 1 {
		t.Fatal(n, err)
	}
	_, err = r.GetProtocolPackage(ctx, "t", p.ProtocolPackageID)
	if keep {
		if err != nil || n.Protocols != 0 {
			t.Fatal("unknown artifact protocol not retained", n, err)
		}
	} else if !errors.Is(err, model.ErrNotFound) || n.Protocols != 1 {
		t.Fatal("private protocol not removed", n, err)
	}
	if _, err = r.GetProductProtocolBinding(ctx, "t", p.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("fixture binding retained", err)
	}
	_, definitionErr := r.GetProtocolDefinition(ctx, "t", protocol)
	_, releaseErr := r.GetProtocolRelease(ctx, "t", protocol, "1.0.0")
	_, pointsErr := r.GetPointTableRelease(ctx, "t", protocol, "1.0.0")
	for _, err := range []error{definitionErr, releaseErr, pointsErr} {
		if (keep && err != nil) || (!keep && !errors.Is(err, model.ErrNotFound)) {
			t.Fatal("protocol metadata retention mismatch", keep, err)
		}
	}
}

func CapacityFixtureSharedProtocol(t *testing.T, r CapacityRepository, reference string) {
	t.Helper()
	ctx := context.Background()
	pkg := "cap-shared-gb26875@1.0.0"
	p := model.Product{TenantID: "t", ID: "shared-fixture", Name: "容量测试 GB26875", ProtocolPackageID: pkg, Status: "DISABLED", Metadata: map[string]any{"source": "CAP"}}
	if err := r.SaveProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveProtocolPackage(ctx, model.ProtocolPackage{TenantID: "t", ID: pkg, Protocol: "cap-shared-gb26875", ParserType: "go_protocol_parser", Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveProtocolPackage(ctx, model.ProtocolPackage{TenantID: "other", ID: "alias@1", Protocol: "cap-shared-gb26875"}); err != nil {
		t.Fatal(err)
	}
	page, err := r.ListCapacityFixtureProducts(ctx, "t", "", 10)
	if err != nil || len(page) != 1 || page[0].ProtocolID == "" {
		t.Fatal(page, err)
	}
	if reference == "product" {
		err = r.SaveProduct(ctx, model.Product{TenantID: "t", ID: "real-product", Name: "现场控制器", ProtocolPackageID: pkg, Status: "ENABLED"})
	} else {
		id := "cap-shared-gb26875@2.0.0"
		if reference == "alias" {
			id = "business-protocol@1.0.0"
		}
		err = r.SaveProtocolPackage(ctx, model.ProtocolPackage{TenantID: "t", ID: id, Protocol: "cap-shared-gb26875", ParserType: "json", Status: "PUBLISHED"})
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = r.PrepareCapacityFixture(ctx, "t", p.ID, page[0].Fingerprint); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("shared protocol accepted after preview", err)
	}
	page, err = r.ListCapacityFixtureProducts(ctx, "t", "", 10)
	if err != nil || len(page) != 1 || page[0].BlockedReason == "" || page[0].ProtocolID != "" {
		t.Fatal("shared package not protected", page, err)
	}
}
