package memory

import (
	"context"
	"testing"

	"iot-platform/internal/model"
)

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
