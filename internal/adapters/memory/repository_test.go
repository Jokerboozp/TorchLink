package memory

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestCapacityCleanupModuleOwnershipAndPending(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	id := "cap-20260930-120000-abcdef"
	q := model.CapacityCleanupBatch{RunID: id, Product: "p", Resources: []model.CapacityCleanupResource{{Kind: "inspection", ID: "own"}, {Kind: "inspection", ID: "business"}, {Kind: "alarm-analysis", ID: "job"}, {Kind: "replay", ID: "replay"}}}
	_, _ = r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{TenantID: "t", ID: "own", CapacityRunID: id, Status: "running"})
	_, _ = r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{TenantID: "t", ID: "business", Status: "succeeded"})
	if _, err := r.CapacityMessageIDs(ctx, "t", q); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatal("running module cleanup allowed", err)
	}
	_, _ = r.UpdateRunningHealthInspectionJob(ctx, model.HealthInspectionJob{TenantID: "t", ID: "own", CapacityRunID: id, Status: "succeeded"})
	_, _ = r.CreateAlarmAnalysisJob(ctx, model.AlarmAnalysisJob{TenantID: "t", AlarmID: "alarm", ID: "job", CapacityRunID: id, Status: "succeeded"})
	_ = r.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: "t", AlarmID: "alarm", CapacityRunID: id})
	_ = r.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: "t", AlarmID: "alarm", KnowledgeScope: "business"})
	_ = r.SaveReplay(ctx, model.ReplayRequest{TenantID: "t", ID: "replay", CapacityRunID: id, Status: "COMPLETED"})
	if _, err := r.CapacityMessageIDs(ctx, "t", q); err != nil {
		t.Fatal(err)
	}
	n, err := r.CleanupCapacityData(ctx, "t", q)
	if err != nil || n.Resources != 3 {
		t.Fatalf("cleanup %+v %v", n, err)
	}
	if _, err := r.GetAIAnalysis(ctx, "t", "alarm", ""); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("own AI result retained", err)
	}
	if _, err := r.GetAIAnalysis(ctx, "t", "alarm", "business"); err != nil {
		t.Fatal("business result deleted", err)
	}
	job, err := r.LatestHealthInspectionJob(ctx, "t", "")
	if err != nil || job.ID != "business" {
		t.Fatal("business inspection changed", job, err)
	}
	if _, err := r.GetReplay(ctx, "replay"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("own replay retained", err)
	}
}

func TestUpdateVideoEventReplacesPendingRecord(t *testing.T) {
	repo := NewRepository()
	event := model.VideoAlarmEvent{
		TenantID: "tenant_001",
		EventID:  "event_001",
		Raw:      map[string]any{"mediaTransferStatus": "PENDING"},
	}
	created, err := repo.SaveVideoEvent(context.Background(), event)
	if err != nil || !created {
		t.Fatalf("SaveVideoEvent() created=%v err=%v", created, err)
	}

	event.Raw["mediaTransferStatus"] = "COMPLETED"
	if err := repo.UpdateVideoEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListPendingVideoEvents(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("updated event remained pending: %#v", pending)
	}
}

func TestAccessStatusDoesNotOverwriteConfiguration(t *testing.T) {
	repositorytest.AccessStatus(t, NewRepository())
}

func TestExecutionLeaseOwnership(t *testing.T) { repositorytest.ExecutionLease(t, NewRepository()) }
func TestStandardClaimFencing(t *testing.T)    { repositorytest.StandardClaim(t, NewRepository()) }
func TestVersionedWrites(t *testing.T)         { repositorytest.VersionedWrites(t, NewRepository()) }

func TestRawReservationBeforeArchive(t *testing.T) { repositorytest.RawReservation(t, NewRepository()) }

func TestProtocolRegistration(t *testing.T) { repositorytest.ProtocolRegistration(t, NewRepository()) }

func TestBatchLookupsKeepTenantAndRequestedIDs(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository()
	for _, tenant := range []string{"allowed", "other"} {
		if err := repo.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "p", Name: tenant}); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: "d", BusinessStatus: tenant}); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveStandardMessage(ctx, model.StandardMessage{TenantID: tenant, MessageID: "m", RawMessageID: "raw", DeviceID: "d", Parser: tenant}); err != nil {
			t.Fatal(err)
		}
		if err := repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: tenant, CameraID: "camera", DeviceID: "d", CameraName: tenant}); err != nil {
			t.Fatal(err)
		}
	}
	products, err := repo.GetProductsByIDs(ctx, "allowed", []string{"p", "missing"})
	if err != nil || len(products) != 1 || products["p"].Name != "allowed" {
		t.Fatalf("products: %+v, %v", products, err)
	}
	states, err := repo.GetDeviceStatesByIDs(ctx, "allowed", []string{"d", "missing"})
	if err != nil || len(states) != 1 || states["d"].BusinessStatus != "allowed" {
		t.Fatalf("states: %+v, %v", states, err)
	}
	messages, err := repo.GetStandardMessagesByRawIDs(ctx, "allowed", []string{"raw", "missing"})
	if err != nil || len(messages) != 1 || messages["raw"].Parser != "allowed" {
		t.Fatalf("messages: %+v, %v", messages, err)
	}
	cameras, err := repo.ListVideoCameraMappingsByDeviceIDs(ctx, "allowed", []string{"d", "missing"})
	if err != nil || len(cameras) != 1 || len(cameras["d"]) != 1 || cameras["d"][0].CameraName != "allowed" {
		t.Fatalf("cameras: %+v, %v", cameras, err)
	}
}

func TestProtocolChildren(t *testing.T) { repositorytest.ProtocolChildren(t, NewRepository()) }

func TestTemplateSwitch(t *testing.T) { repositorytest.TemplateSwitch(t, NewRepository()) }

func TestPreparedEnrollment(t *testing.T) { repositorytest.PreparedEnrollment(t, NewRepository()) }

func TestSwitchProductProtocolIsAtomicAndDetectsChanges(t *testing.T) {
	ctx := context.Background()
	r := NewRepository()
	product := model.Product{TenantID: "t", ID: "p", Name: "模板", Status: "ENABLED", ProtocolPackageID: "fire@1"}
	switchTo := func(version string, expected *model.ProductProtocolBinding) error {
		next := product
		next.ProtocolPackageID = "fire@" + version
		return r.SwitchProductProtocol(ctx, model.ProtocolSwitch{
			Product:  next,
			Package:  model.ProtocolPackage{TenantID: "t", ID: next.ProtocolPackageID, Protocol: "fire", Version: version, Status: "PUBLISHED"},
			Binding:  model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "fire", Version: version},
			Expected: expected,
		})
	}
	if err := switchTo("1", nil); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing template: %v", err)
	}
	_ = r.SaveProduct(ctx, product)
	if err := switchTo("1", nil); err != nil {
		t.Fatal(err)
	}
	if err := switchTo("2", nil); !errors.Is(err, model.ErrBindingChanged) {
		t.Fatalf("a first bind raced with another bind: %v", err)
	}
	stale := &model.ProductProtocolBinding{ProtocolID: "fire", Version: "0"}
	if err := switchTo("2", stale); !errors.Is(err, model.ErrBindingChanged) {
		t.Fatalf("stale binding accepted: %v", err)
	}
	if err := switchTo("2", &model.ProductProtocolBinding{ProtocolID: "fire", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	saved, _ := r.GetProduct(ctx, "t", "p")
	binding, _ := r.GetProductProtocolBinding(ctx, "t", "p")
	pkg, err := r.GetProtocolPackage(ctx, "t", "fire@2")
	if saved.ProtocolPackageID != "fire@2" || binding.Version != "2" || err != nil || pkg.Version != "2" {
		t.Fatalf("switch not written together: %+v %+v %+v %v", saved, binding, pkg, err)
	}
}

func TestRawFilters(t *testing.T) { repositorytest.RawFilters(t, NewRepository()) }

func TestVideoCameraRelationsEnforceOneDevicePerCamera(t *testing.T) {
	repo := NewRepository()
	camera := model.VideoCameraMapping{TenantID: "tenant-001", CameraID: "camera-001", CameraName: "一号摄像头", DeviceID: "device-001", Brand: "大华", CameraPoint: "东侧入口", Building: "A", Floor: "1", Room: "大厅"}
	if err := repo.SaveVideoCameraMapping(context.Background(), camera); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveVideoCameraMapping(context.Background(), model.VideoCameraMapping{TenantID: "tenant-001", CameraID: "camera-002", CameraName: "二号摄像头", DeviceID: "device-001"}); err != nil {
		t.Fatal(err)
	}
	relations, err := repo.ListVideoCameraRelationsByTarget(context.Background(), "tenant-001", "device", "device-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(relations) != 2 || relations[0].CameraID != "camera-001" || relations[1].CameraID != "camera-002" {
		t.Fatalf("reverse device lookup = %#v", relations)
	}
	otherDevice, err := repo.ListVideoCameraRelationsByTarget(context.Background(), "tenant-001", "device", "device-002")
	if err != nil {
		t.Fatal(err)
	}
	if len(otherDevice) != 0 {
		t.Fatalf("unexpected second device relation = %#v", otherDevice)
	}
	cameraRelations, err := repo.ListVideoCameraRelations(context.Background(), "tenant-001", "camera-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(cameraRelations) != 1 || cameraRelations[0].TargetID != "device-001" {
		t.Fatalf("camera relations = %#v", cameraRelations)
	}
}

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

func TestDeleteProtocolReleaseKeepsOtherVersionsAndBlocksBindings(t *testing.T) {
	ctx := context.Background()
	r := NewRepository()
	for _, version := range []string{"1.0.0", "2.0.0"} {
		if err := r.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "protocol", Version: version}); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "protocol", Version: "2.0.0", PreviousVersion: "1.0.0"})
	if err := r.DeleteProtocolRelease(ctx, "tenant", "protocol", "1.0.0"); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("rollback version must be protected: %v", err)
	}
	_ = r.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "protocol", Version: "2.0.0"})
	_ = r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", ProtocolID: "protocol", ProtocolVersion: "1.0.0"})
	if err := r.DeleteProtocolRelease(ctx, "tenant", "protocol", "1.0.0"); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("profile version must be protected: %v", err)
	}
	_ = r.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", ProtocolID: "protocol", ProtocolVersion: "2.0.0"})
	if err := r.DeleteProtocolRelease(ctx, "tenant", "protocol", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetProtocolRelease(ctx, "tenant", "protocol", "2.0.0"); err != nil {
		t.Fatalf("other version was deleted: %v", err)
	}
	if err := r.DeleteProtocolRelease(ctx, "tenant", "protocol", "1.0.0"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("repeat delete: %v", err)
	}
}

func TestDeleteAlarmRemovesEveryAnalysisScope(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository()
	if _, _, err := repo.UpsertAlarm(ctx, model.Alarm{TenantID: "t1", ID: "alarm-1", Status: "CLOSED"}); err != nil {
		t.Fatal(err)
	}
	scopes := []string{model.AIAnalysisScopeNone, model.AlarmAnalysisWorkflowID, model.AIAnalysisScopeLegacyTenant}
	for _, scope := range scopes {
		if err := repo.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: "t1", AlarmID: "alarm-1", KnowledgeScope: scope}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: "t1", AlarmID: "alarm-10"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteResource(ctx, "t1", "alarm", "alarm-1"); err != nil {
		t.Fatal(err)
	}
	for _, scope := range scopes {
		if _, err := repo.GetAIAnalysis(ctx, "t1", "alarm-1", scope); err == nil {
			t.Fatalf("analysis scope %q survived alarm deletion", scope)
		}
	}
	if _, err := repo.GetAIAnalysis(ctx, "t1", "alarm-10", model.AIAnalysisScopeNone); err != nil {
		t.Fatalf("deleting alarm-1 removed another alarm's analysis: %v", err)
	}
}
