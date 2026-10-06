package onboarding

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

func TestSaveDeviceKeepsRegistrationAndChecksTheGateway(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	s := New(repo, nil, "", nil)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Name: "烟感", Status: "ENABLED"}))
	must(repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "other", Name: "温感", Status: "ENABLED"}))
	must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "gw", ProductID: "p", Name: "网关", Status: "ENABLED", DeviceRole: "GATEWAY", AccessKey: "k-gw", ConnectorProfileID: "listener"}))
	must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "direct", ProductID: "p", Name: "直连", Status: "ENABLED", DeviceRole: "DIRECT", AccessKey: "k-direct", SecretHash: "h"}))

	created, err := s.SaveDevice(ctx, model.ManagedDevice{TenantID: "t", Name: "新设备", ProductID: "p"})
	if err != nil || !created.Created || created.Device.ID == "" || created.Device.DeviceRole != "DIRECT" {
		t.Fatalf("new device = %+v, %v", created, err)
	}
	if _, err := repo.GetManagedDevice(ctx, "t", created.Device.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("a new device is registered through enrollment, not stored by SaveDevice")
	}

	edited, err := s.SaveDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "direct", Name: "改名", ProductID: "p", ReportIntervalSec: 60})
	if err != nil || edited.Created || !edited.TimingChanged || edited.Device.SecretHash != "h" || edited.Device.AccessKey != "k-direct" {
		t.Fatalf("edit = %+v, %v", edited, err)
	}
	if _, err := s.SaveDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "direct", Name: "改模板", ProductID: "other"}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("changing the template = %v, want a conflict", err)
	}

	child, err := s.SaveDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "child", Name: "子设备", ProductID: "p", DeviceRole: "CHILD", GatewayID: "gw"})
	if err != nil || child.Device.ConnectorProfileID != "listener" {
		t.Fatalf("a child inherits its gateway's connection: %+v, %v", child, err)
	}
	for name, v := range map[string]model.ManagedDevice{
		"not a gateway":   {TenantID: "t", ID: "c2", Name: "子设备", ProductID: "p", DeviceRole: "CHILD", GatewayID: "direct"},
		"missing gateway": {TenantID: "t", ID: "c3", Name: "子设备", ProductID: "p", DeviceRole: "CHILD", GatewayID: "nobody"},
		"own connection":  {TenantID: "t", ID: "c4", Name: "子设备", ProductID: "p", DeviceRole: "CHILD", GatewayID: "gw", ConnectorProfileID: "elsewhere"},
		"unknown profile": {TenantID: "t", ID: "d2", Name: "设备", ProductID: "p", ConnectorProfileID: "elsewhere"},
		"unknown role":    {TenantID: "t", ID: "d3", Name: "设备", ProductID: "p", DeviceRole: "ROUTER"},
		"missing product": {TenantID: "t", ID: "d4", Name: "设备", ProductID: "none"},
	} {
		if _, err := s.SaveDevice(ctx, v); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("%s: %v, want an invalid-request error", name, err)
		}
	}
}

func TestSaveProductResolvesTheProtocolAfterValidating(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	s := New(repo, nil, "", nil)
	resolved := 0
	resolve := func(context.Context, string, string) (model.ProtocolPackage, error) {
		resolved++
		return model.ProtocolPackage{Transport: "MQTT", PayloadFormat: "json"}, nil
	}
	if _, err := s.SaveProduct(ctx, model.Product{TenantID: "t", Name: "烟感"}, resolve); !errors.Is(err, model.ErrInvalid) || resolved != 0 {
		t.Fatalf("an invalid template must be rejected before resolving its protocol: %v (resolved %d)", err, resolved)
	}
	saved, err := s.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Name: "烟感", ProtocolPackageID: "std@1"}, resolve)
	if err != nil || !saved.Created || saved.Product.Transport != "MQTT" || saved.Product.Status != "ENABLED" {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	if _, err := s.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Name: "烟感", ProtocolPackageID: "other@1"}, resolve); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("changing the protocol directly = %v, want a conflict", err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Name: "设备", Status: "ENABLED", AccessKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Name: "烟感", ProtocolPackageID: "std@1", Transport: "HTTP"}, resolve); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("reconfiguring a template in use = %v, want a conflict", err)
	}
}
