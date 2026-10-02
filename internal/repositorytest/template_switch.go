package repositorytest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// TemplateSwitch exercises the same durable switch contract in each adapter.
func TemplateSwitch(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	t.Run("direct-shared-edit-cannot-bypass-template-trial", func(t *testing.T) {
		p := model.Product{TenantID: "guard-profile", ID: "p", Status: "ENABLED"}
		if err := repo.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
		binding := model.ProductProtocolBinding{TenantID: p.TenantID, ProductID: p.ID, ProtocolID: "wire", Version: "1"}
		if err := repo.SaveProductProtocolBinding(ctx, binding); err != nil {
			t.Fatal(err)
		}
		profile := model.DeviceAccessProfile{TenantID: p.TenantID, ID: "shared", ProductID: p.ID, ProtocolID: "wire", ProtocolVersion: "1", Mode: "listener", Network: "tcp", Host: "0.0.0.0", Port: 29009, Enabled: true}
		guard := model.AccessProfileSaveOptions{GuardTemplate: true}
		if err := repo.SaveDeviceAccessProfile(ctx, profile, guard); err != nil {
			t.Fatal("initial setup", err)
		}
		if err := repo.SaveOnboarding(ctx, model.OnboardingBundle{Device: model.ManagedDevice{TenantID: p.TenantID, ID: "d", ProductID: p.ID, Status: "ENABLED"}}); err != nil {
			t.Fatal(err)
		}
		profile.Port++
		if err := repo.SaveDeviceAccessProfile(ctx, profile, guard); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("shared edit bypassed trial", err)
		}
		stored, err := repo.GetDeviceAccessProfile(ctx, p.TenantID, profile.ID)
		if err != nil || stored.Port != 29009 {
			t.Fatal("rejected change persisted", stored, err)
		}
		stop := stored
		stop.Enabled = false
		if err := repo.SaveDeviceAccessProfile(ctx, stop, guard); err != nil {
			t.Fatal("operational stop blocked", err)
		}
		if err := repo.SaveDeviceAccessProfile(ctx, stored, guard); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("direct restart bypassed preparation", err)
		}
		profile.DeviceID = "d"
		if err := repo.SaveDeviceAccessProfile(ctx, profile, guard); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("shared profile became device-specific", err)
		}
		profile.ID, profile.ConnectionMode = "private", "dial"
		if err := repo.SaveDeviceAccessProfile(ctx, profile, guard); err != nil {
			t.Fatal("device correction blocked", err)
		}
		binding.Version = "2"
		if err := repo.SaveProductProtocolBinding(ctx, binding); err != nil {
			t.Fatal(err)
		}
		profile.Host = "changed.local"
		if err := repo.SaveDeviceAccessProfile(ctx, profile, guard); !errors.Is(err, model.ErrBindingChanged) {
			t.Fatal("stale protocol edited connection", err)
		}
	})
	change := func(id string) model.ProtocolSwitch {
		p := model.Product{TenantID: "template-switch", ID: id, Name: id, Status: "ENABLED", ProtocolPackageID: id + "@1"}
		return model.ProtocolSwitch{Product: p,
			Package: model.ProtocolPackage{TenantID: p.TenantID, ID: p.ProtocolPackageID, Protocol: id, Version: "1", ParserType: "go-protocol-v2", Status: "PUBLISHED"},
			Binding: model.ProductProtocolBinding{TenantID: p.TenantID, ProductID: id, ProtocolID: id, Version: "1"},
			Preparation: &model.TemplateSwitch{CreateProduct: true, ExpectedProduct: p,
				Record:   model.OnboardingRecord{TenantID: p.TenantID, ID: "template:" + id, OwnerID: "template", Kind: "template-preparation", Status: "AWAITING_VALIDATION", Body: json.RawMessage(`{"status":"AWAITING_VALIDATION"}`)},
				Profiles: []model.DeviceAccessProfile{{TenantID: p.TenantID, ID: id + "-listener", ProductID: id, Mode: "listener", Network: "tcp", Port: 29001}}}}
	}
	assertAbsent := func(v model.ProtocolSwitch) {
		t.Helper()
		if _, err := repo.GetProduct(ctx, v.Product.TenantID, v.Product.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("failed switch left a product", err)
		}
		if _, err := repo.GetProductProtocolBinding(ctx, v.Product.TenantID, v.Product.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("failed switch left a binding", err)
		}
		if _, err := repo.GetProtocolPackage(ctx, v.Package.TenantID, v.Package.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("failed switch left a package", err)
		}
		if _, err := repo.GetOnboardingRecord(ctx, v.Preparation.Record.TenantID, v.Preparation.Record.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("failed switch left preparation", err)
		}
		if _, err := repo.GetDeviceAccessProfile(ctx, v.Product.TenantID, v.Preparation.Profiles[0].ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("failed switch left a listener", err)
		}
	}

	t.Run("failed-create-rolls-back-every-resource", func(t *testing.T) {
		v := change("failed")
		v.Preparation.ExpectedProduct.Name = "stale snapshot"
		if err := repo.SwitchProductProtocol(ctx, v); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("stale snapshot", err)
		}
		assertAbsent(v)
		v.Preparation.ExpectedProduct = v.Product
		v.Preparation.ExpectedRevision = 3
		if err := repo.SwitchProductProtocol(ctx, v); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("stale revision", err)
		}
		assertAbsent(v)
		v.Preparation.ExpectedRevision = 0
		v.Preparation.Profiles[0].Enabled = true
		occupied := model.DeviceAccessProfile{TenantID: "other", ID: "occupied", ProductID: "other", Mode: "listener", Network: "tcp", Port: 29001, Enabled: true}
		if err := repo.SaveDeviceAccessProfile(ctx, occupied); err != nil {
			t.Fatal(err)
		}
		if err := repo.SwitchProductProtocol(ctx, v); !errors.Is(err, model.ErrBindingChanged) {
			t.Fatal("occupied endpoint", err)
		}
		assertAbsent(v)
	})
	t.Run("concurrent-create-has-one-winner-and-cannot-overwrite", func(t *testing.T) {
		v := change("created")
		var won atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := repo.SwitchProductProtocol(ctx, v); err == nil {
					won.Add(1)
				} else if !errors.Is(err, model.ErrOnboardingChanged) {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if won.Load() != 1 {
			t.Fatal("create winners", won.Load())
		}
		p, err := repo.GetProduct(ctx, v.Product.TenantID, v.Product.ID)
		if err != nil || p.ProtocolPackageID != v.Package.ID {
			t.Fatal(p, err)
		}
		b, err := repo.GetProductProtocolBinding(ctx, p.TenantID, p.ID)
		if err != nil || b.Version != "1" {
			t.Fatal(b, err)
		}
		if _, err = repo.GetProtocolPackage(ctx, p.TenantID, v.Package.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.GetDeviceAccessProfile(ctx, p.TenantID, v.Preparation.Profiles[0].ID); err != nil {
			t.Fatal(err)
		}
		rec, err := repo.GetOnboardingRecord(ctx, p.TenantID, v.Preparation.Record.ID)
		if err != nil || rec.Revision != 1 {
			t.Fatal(rec, err)
		}
		v.Product.Name = "must not overwrite"
		if err = repo.SwitchProductProtocol(ctx, v); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal(err)
		}
		p, _ = repo.GetProduct(ctx, p.TenantID, p.ID)
		if p.Name != "created" {
			t.Fatal("duplicate create overwrote template", p)
		}
	})
	t.Run("shared-listener-cannot-overwrite-device-profile", func(t *testing.T) {
		v := change("existing")
		v.Preparation.CreateProduct = false
		if err := repo.SaveProduct(ctx, v.Product); err != nil {
			t.Fatal(err)
		}
		private := v.Preparation.Profiles[0]
		private.DeviceID, private.Host, private.Mode = "device-1", "device.local", "polling"
		if err := repo.SaveDeviceAccessProfile(ctx, private); err != nil {
			t.Fatal(err)
		}
		if err := repo.SwitchProductProtocol(ctx, v); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("device profile overwritten", err)
		}
		saved, err := repo.GetDeviceAccessProfile(ctx, private.TenantID, private.ID)
		if err != nil || saved.DeviceID != private.DeviceID || saved.Host != private.Host {
			t.Fatal(saved, err)
		}
		if _, err = repo.GetProductProtocolBinding(ctx, v.Product.TenantID, v.Product.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("failed switch left binding", err)
		}
		if _, err = repo.GetOnboardingRecord(ctx, v.Product.TenantID, v.Preparation.Record.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("failed switch advanced revision", err)
		}
	})
	t.Run("direct-binding-cannot-change-after-device-registration", func(t *testing.T) {
		v := change("initial-only")
		v.Preparation = nil
		v.RequireUnused = true
		if err := repo.SaveProduct(ctx, v.Product); err != nil {
			t.Fatal(err)
		}
		if err := repo.SwitchProductProtocol(ctx, v); err != nil {
			t.Fatal("first bind rejected", err)
		}
		previous := v.Binding
		if err := repo.SaveOnboarding(ctx, model.OnboardingBundle{Device: model.ManagedDevice{TenantID: v.Product.TenantID, ID: "registered", ProductID: v.Product.ID, Status: "ENABLED", AccessKey: "initial-only-key"}}); err != nil {
			t.Fatal(err)
		}
		v.Expected = &previous
		v.Product.ProtocolPackageID = "initial-only@2"
		v.Package.ID, v.Package.Version, v.Binding.Version = v.Product.ProtocolPackageID, "2", "2"
		if err := repo.SwitchProductProtocol(ctx, v); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("direct bind accepted after registration", err)
		}
		product, err := repo.GetProduct(ctx, v.Product.TenantID, v.Product.ID)
		if err != nil || product.ProtocolPackageID != "initial-only@1" {
			t.Fatal("rejected switch changed template", product, err)
		}
		binding, err := repo.GetProductProtocolBinding(ctx, v.Product.TenantID, v.Product.ID)
		if err != nil || binding.Version != "1" {
			t.Fatal("rejected switch changed binding", binding, err)
		}
		if _, err := repo.GetProtocolPackage(ctx, v.Package.TenantID, v.Package.ID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("rejected switch installed package", err)
		}
	})
}
