package postgres

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
)

// Uses only its own temporary schema in an explicitly configured test database.
func TestSwitchProductProtocolDetectsChangedBinding(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	var err error

	product := model.Product{TenantID: "t", ID: "p", Name: "模板", Status: "ENABLED", ProtocolPackageID: "fire@1"}
	switchTo := func(version string, expected *model.ProductProtocolBinding) error {
		next := product
		next.ProtocolPackageID = "fire@" + version
		return r.SwitchProductProtocol(ctx, model.ProtocolSwitch{
			Product:  next,
			Package:  model.ProtocolPackage{TenantID: "t", ID: next.ProtocolPackageID, Protocol: "fire", Version: version, Status: "PUBLISHED", ParserType: "go-protocol-v2"},
			Binding:  model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "fire", Version: version},
			Expected: expected,
		})
	}
	if err = switchTo("1", nil); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing template: %v", err)
	}
	if err = r.SaveProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	if err = switchTo("1", nil); err != nil {
		t.Fatal(err)
	}
	if err = switchTo("2", nil); !errors.Is(err, model.ErrBindingChanged) {
		t.Fatalf("a first bind raced with another bind: %v", err)
	}
	if err = switchTo("2", &model.ProductProtocolBinding{ProtocolID: "fire", Version: "0"}); !errors.Is(err, model.ErrBindingChanged) {
		t.Fatalf("stale binding accepted: %v", err)
	}
	if saved, _ := r.GetProduct(ctx, "t", "p"); saved.ProtocolPackageID != "fire@1" {
		t.Fatalf("a rejected switch changed the template: %+v", saved)
	}
	if err = switchTo("2", &model.ProductProtocolBinding{ProtocolID: "fire", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	saved, _ := r.GetProduct(ctx, "t", "p")
	binding, _ := r.GetProductProtocolBinding(ctx, "t", "p")
	pkg, err := r.GetProtocolPackage(ctx, "t", "fire@2")
	if saved.ProtocolPackageID != "fire@2" || binding.Version != "2" || err != nil || pkg.Version != "2" {
		t.Fatalf("switch not written together: %+v %+v %+v %v", saved, binding, pkg, err)
	}
}
