package repositorytest

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"iot-platform/internal/model"
)

type pagingRepository interface {
	SaveManagedDevice(context.Context, model.ManagedDevice) error
	ListManagedDevicesPage(context.Context, string, int, int) ([]model.ManagedDevice, int, error)
	SaveVideoPlaySession(context.Context, model.VideoPlaySession) error
	ListActiveVideoPlaySessions(context.Context, int64) ([]model.VideoPlaySession, error)
	PruneVideoPlaySessions(context.Context, int64) error
}

// DevicePagesAndPlaySessions checks the device list pages newest first with
// a stable order for equal times, stays within its tenant, and that pruning
// play sessions removes only expired or long-revoked ones.
func DevicePagesAndPlaySessions(t *testing.T, repo pagingRepository) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		updated := int64(100 + i/2) // dev0/dev1 and dev2/dev3 share update times
		d := model.ManagedDevice{ID: fmt.Sprintf("page-dev%d", i), TenantID: "paging", ProductID: "p", Name: "设备", Status: "ENABLED", AccessKey: fmt.Sprintf("paging-ak-%d", i), SecretHash: "h", CreatedAt: 1, UpdatedAt: updated}
		if err := repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "page-other", TenantID: "paging-other", ProductID: "p", Name: "其他", Status: "ENABLED", AccessKey: "paging-ak-other", SecretHash: "h", CreatedAt: 1, UpdatedAt: 999}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for offset := 0; offset < 6; offset += 2 {
		items, total, err := repo.ListManagedDevicesPage(ctx, "paging", 2, offset)
		if err != nil || total != 5 {
			t.Fatalf("page %d: total=%d err=%v", offset, total, err)
		}
		for _, d := range items {
			ids = append(ids, d.ID)
		}
	}
	if got := strings.Join(ids, ","); got != "page-dev4,page-dev3,page-dev2,page-dev1,page-dev0" {
		t.Fatalf("device pages overlap, skip or leave the tenant: %s", got)
	}

	sessions := []model.VideoPlaySession{
		{ID: "prune-expired", TenantID: "paging", CameraID: "cam", ExpiresAt: 1000},
		{ID: "prune-revoked", TenantID: "paging", CameraID: "cam", ExpiresAt: 1 << 50, RevokedAt: 1500},
		{ID: "prune-recently-revoked", TenantID: "paging", CameraID: "cam", ExpiresAt: 1 << 50, RevokedAt: 2500},
		{ID: "prune-active", TenantID: "paging", CameraID: "cam", ExpiresAt: 1 << 50},
	}
	for _, s := range sessions {
		if err := repo.SaveVideoPlaySession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.PruneVideoPlaySessions(ctx, 2000); err != nil {
		t.Fatal(err)
	}
	active, err := repo.ListActiveVideoPlaySessions(ctx, 3000)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, s := range active {
		found[s.ID] = true
	}
	if !found["prune-active"] || found["prune-expired"] || found["prune-revoked"] || found["prune-recently-revoked"] {
		t.Fatalf("active sessions after pruning: %+v", active)
	}
}
