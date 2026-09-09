// Package repositorytest contains storage contract checks shared by adapters.
package repositorytest

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"testing"
)

func AccessStatus(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	original := model.DeviceAccessProfile{TenantID: "status-test", ID: "profile", ProductID: "product", DeviceID: "device", Host: "127.0.0.1", Port: 502, Enabled: true, UpdatedAt: 10}
	if err := repo.SaveDeviceAccessProfile(ctx, original); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.UpdateDeviceAccessStatus(ctx, original, "ONLINE", "", 20); err != nil || !ok {
		t.Fatal("status update rejected", err)
	}
	current, err := repo.GetDeviceAccessProfile(ctx, original.TenantID, original.ID)
	if err != nil || current.LastSuccessAt != 20 || current.UpdatedAt != 10 {
		t.Fatal("status changed configuration revision", err)
	}
	for _, sample := range []struct {
		status               string
		at, success, failure int64
	}{
		{"LISTENING", 0, 20, 0}, {"LISTENING", 25, 25, 0}, {"ERROR", 26, 25, 26}, {"LISTENING", 0, 25, 26}, {"PENDING", 0, 25, 26},
	} {
		if ok, err := repo.UpdateDeviceAccessStatus(ctx, original, sample.status, "", sample.at); err != nil || !ok {
			t.Fatal("listener status update", err)
		}
		current, err = repo.GetDeviceAccessProfile(ctx, original.TenantID, original.ID)
		if err != nil || current.RuntimeStatus != sample.status || current.LastSuccessAt != sample.success || current.LastErrorAt != sample.failure {
			t.Fatalf("listener status timestamps: %+v %v", current, err)
		}
	}
	current.Enabled, current.Host, current.EdgeNodeID, current.UpdatedAt = false, "192.0.2.1", "remote", 30
	if err := repo.SaveDeviceAccessProfile(ctx, current); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"ONLINE", "ERROR", "LISTENING", "PENDING"} {
		if ok, err := repo.UpdateDeviceAccessStatus(ctx, original, status, "late observation", 40); err != nil || ok {
			t.Fatal("stale status accepted", status, err)
		}
	}
	actual, err := repo.GetDeviceAccessProfile(ctx, current.TenantID, current.ID)
	if err != nil || actual != current {
		t.Fatal("stale observation replaced user edit", err)
	}
	other := current
	other.TenantID = "other-tenant"
	if ok, err := repo.UpdateDeviceAccessStatus(ctx, other, "ERROR", "cross tenant", 50); err != nil || ok {
		t.Fatal("cross tenant status accepted", err)
	}
}
