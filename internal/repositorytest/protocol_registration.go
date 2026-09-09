package repositorytest

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"sync/atomic"
	"testing"
)

func ProtocolRegistration(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const tenant = "protocol-registration-tenant"
	product := model.Product{TenantID: tenant, ID: "product", Status: "ENABLED"}
	node := model.EdgeNode{TenantID: tenant, ID: "node", Status: "ENABLED"}
	p := model.DeviceAccessProfile{TenantID: tenant, ID: "profile", ProductID: product.ID, EdgeNodeID: node.ID, Mode: "listener", Network: "tcp", AutoRegister: true, Enabled: true}
	for _, err := range []error{r.SaveProduct(ctx, product), r.SaveEdgeNode(ctx, node), r.SaveDeviceAccessProfile(ctx, p)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var winners atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, created, err := r.RegisterProtocolDevice(ctx, p, "first", "Protocol device")
			if err != nil {
				t.Error(err)
				return
			}
			if created {
				winners.Add(1)
			}
			if d.AccessKey == "" || d.SecretHash != "" || !d.AutoRegistered {
				t.Error("invalid registered inventory")
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("registration created more than once", winners.Load())
	}
	first, err := r.GetManagedDevice(ctx, tenant, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, created, err := r.RegisterProtocolDevice(ctx, p, "second", "Second")
	if err != nil || !created || first.AccessKey == second.AccessKey {
		t.Fatal("second identity key collision", second, err)
	}
	first.Name, first.AccessKey, first.SecretHash = "Operator edited", "existing-access", "existing-secret-hash"
	if err = r.SaveManagedDevice(ctx, first); err != nil {
		t.Fatal(err)
	}
	unchanged, created, err := r.RegisterProtocolDevice(ctx, p, "first", "Overwrite attempt")
	if err != nil || created || unchanged.Name != first.Name || unchanged.AccessKey != first.AccessKey || unchanged.SecretHash != first.SecretHash {
		t.Fatal("repeat registration overwrote credentials/name", err)
	}
	first.Status = "DISABLED"
	if err = r.SaveManagedDevice(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "first", "Restore attempt"); !errors.Is(err, model.ErrProtocolRegistration) {
		t.Fatal("disabled device revived", err)
	}
	second.ProductID = "other-product"
	if err = r.SaveManagedDevice(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "second", "Move attempt"); !errors.Is(err, model.ErrProtocolRegistration) {
		t.Fatal("foreign product claimed", err)
	}
	current := p
	current.AutoRegister = false
	if err = r.SaveDeviceAccessProfile(ctx, current); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "stale", ""); !errors.Is(err, model.ErrProtocolRegistration) {
		t.Fatal("stale configuration registered device", err)
	}
	if err = r.SaveDeviceAccessProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	node.Status = "DISABLED"
	if err = r.SaveEdgeNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.RegisterProtocolDevice(ctx, p, "disabled-node", ""); !errors.Is(err, model.ErrProtocolRegistration) {
		t.Fatal("disabled node registered device", err)
	}
}
