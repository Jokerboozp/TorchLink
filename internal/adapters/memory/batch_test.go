package memory

import (
	"context"
	"testing"

	"iot-platform/internal/model"
)

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
