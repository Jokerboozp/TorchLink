package core

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

type countingProtocolRepo struct {
	*memory.Repository
	bindingReads, releaseReads atomic.Int32
}

func (r *countingProtocolRepo) GetProductProtocolBinding(ctx context.Context, tenant, product string) (model.ProductProtocolBinding, error) {
	r.bindingReads.Add(1)
	return r.Repository.GetProductProtocolBinding(ctx, tenant, product)
}

func (r *countingProtocolRepo) GetProtocolRelease(ctx context.Context, tenant, protocol, version string) (model.ProtocolRelease, error) {
	r.releaseReads.Add(1)
	return r.Repository.GetProtocolRelease(ctx, tenant, protocol, version)
}

// Bindings and releases are read once per window, missing bindings included,
// and a change made through this process is visible at once.
func TestProtocolMetadataIsCachedUntilChanged(t *testing.T) {
	ctx := context.Background()
	repo := &countingProtocolRepo{Repository: memory.NewRepository()}
	e := &Engine{Repo: repo}
	for i := 0; i < 3; i++ {
		if _, err := e.productBinding(ctx, "t1", "p1"); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("missing binding: %v", err)
		}
	}
	if repo.bindingReads.Load() != 1 {
		t.Fatalf("missing binding read %d times, want once", repo.bindingReads.Load())
	}
	if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t1", ProtocolID: "proto", Version: "1", Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t1", ProductID: "p1", ProtocolID: "proto", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	e.ProtocolsChanged("t1")
	binding, err := e.productBinding(ctx, "t1", "p1")
	if err != nil || binding.Version != "1" {
		t.Fatalf("binding change not visible after ProtocolsChanged: %#v %v", binding, err)
	}
	for i := 0; i < 3; i++ {
		if _, err = e.protocolRelease(ctx, "t1", "proto", "1"); err != nil {
			t.Fatal(err)
		}
	}
	if repo.releaseReads.Load() != 1 {
		t.Fatalf("release read %d times, want once", repo.releaseReads.Load())
	}
	if err = repo.UpdateProtocolReleaseStatus(ctx, "t1", "proto", "1", "REVOKED", 0); err != nil {
		t.Fatal(err)
	}
	e.ProtocolsChanged("t1")
	if release, _ := e.protocolRelease(ctx, "t1", "proto", "1"); release.Status != "REVOKED" {
		t.Fatalf("revocation not visible after ProtocolsChanged: %s", release.Status)
	}
}
