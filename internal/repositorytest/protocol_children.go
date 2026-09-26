package repositorytest

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"sync/atomic"
	"testing"
)

func ProtocolChildren(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	tenant := "children-test"
	for _, id := range []string{"main", "sensor"} {
		if e := repo.SaveProduct(ctx, model.Product{ID: id, TenantID: tenant, Status: "ENABLED"}); e != nil {
			t.Fatal(e)
		}
	}
	if e := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: tenant, ProtocolID: "sensor", Version: "1", Status: "PUBLISHED", PayloadFormat: "hex"}); e != nil {
		t.Fatal(e)
	}
	if e := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: tenant, ProductID: "sensor", ProtocolID: "sensor", Version: "1"}); e != nil {
		t.Fatal(e)
	}
	p := model.DeviceAccessProfile{ID: "profile", TenantID: tenant, ProductID: "main", Mode: "listener", Network: "tcp", Enabled: true, ChildProducts: []model.ChildProductBinding{{Type: "smoke", ProductID: "sensor"}}}
	if e := repo.SaveDeviceAccessProfile(ctx, p); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"main-1", "main-2"} {
		if e := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: tenant, ProductID: "main", DeviceRole: "DIRECT", Status: "ENABLED", AccessKey: "children-" + id, SecretHash: "keep"}); e != nil {
			t.Fatal(e)
		}
	}
	identity := model.ChildIdentity{Address: "01", Type: "smoke", Name: "探测器"}
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, yes, err := repo.RegisterProtocolChild(ctx, p, "main-1", identity)
			if err != nil {
				t.Error(err)
			}
			if yes {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("non atomic registration", created.Load())
	}
	items, total, err := repo.ListManagedDeviceChildren(ctx, tenant, "main-1", 20, 0)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatal(items, total, err)
	}
	child := items[0]
	if child.ProductID != "sensor" || child.GatewayID != "main-1" || child.SecretHash != "" {
		t.Fatal(child)
	}
	second, _, err := repo.RegisterProtocolChild(ctx, p, "main-2", identity)
	if err != nil || second.ID == child.ID {
		t.Fatal("parent address collision", err)
	}
	parent, err := repo.GetManagedDevice(ctx, tenant, "main-1")
	if err != nil || parent.DeviceRole != "GATEWAY" || parent.SecretHash != "keep" {
		t.Fatal("parent credentials or role changed", parent, err)
	}
	identity.Name = "更新名称"
	updated, yes, err := repo.RegisterProtocolChild(ctx, p, "main-1", identity)
	if err != nil || yes || updated.Name != identity.Name {
		t.Fatal(updated, yes, err)
	}
	identity.Type = "unconfigured"
	if _, _, err = repo.RegisterProtocolChild(ctx, p, "main-1", identity); err == nil {
		t.Fatal("unknown child type accepted")
	}
	identity.Type = "smoke"
	stale := p
	stale.Enabled = false
	if _, _, err = repo.RegisterProtocolChild(ctx, stale, "main-1", identity); err == nil {
		t.Fatal("stale config accepted")
	}
	if _, _, err = repo.RegisterProtocolChild(ctx, p, child.ID, identity); err == nil {
		t.Fatal("child became parent")
	}
	if list, n, e := repo.ListManagedDeviceChildren(ctx, "other", parent.ID, 20, 0); e != nil || n != 0 || len(list) != 0 {
		t.Fatal("cross tenant child leaked")
	}
	child.Status = "DISABLED"
	repo.SaveManagedDevice(ctx, child)
	if _, _, err = repo.RegisterProtocolChild(ctx, p, "main-1", identity); err == nil {
		t.Fatal("disabled child reenabled")
	}
}
