package repositorytest

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"sync/atomic"
	"testing"
)

func DeviceShadow(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	tenant, device := "shadow-tenant", "device"
	s, err := r.GetDeviceShadow(ctx, tenant, device)
	if err != nil || s.Version != 0 || len(s.Delta) != 0 {
		t.Fatal("initial shadow", s, err)
	}
	s, err = r.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, Actor: "operator", Desired: map[string]any{"target": float64(42)}, Timestamp: 100})
	if err != nil || s.DesiredVersion != 1 || s.Delta["target"] != float64(42) {
		t.Fatal("desired", s, err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := r.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, Actor: "operator", ExpectedVersion: 1, Desired: map[string]any{"target": float64(100 + i)}, Timestamp: 200})
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, model.ErrShadowConflict) {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if wins.Load() != 1 {
		t.Fatalf("concurrent writers: %d", wins.Load())
	}
	s, err = r.GetDeviceShadow(ctx, tenant, device)
	if err != nil {
		t.Fatal(err)
	}
	target := s.Desired["target"]
	newer := model.ShadowUpdate{TenantID: tenant, DeviceID: device, MessageID: "new", Timestamp: 500, Reported: map[string]any{"target": target}}
	s, err = r.UpdateDeviceShadow(ctx, newer)
	if err != nil || len(s.Delta) != 0 {
		t.Fatal("reconciliation", s, err)
	}
	version := s.Version
	s, err = r.UpdateDeviceShadow(ctx, newer)
	if err != nil || s.Version != version {
		t.Fatal("duplicate report changed revision", s, err)
	}
	s, err = r.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, MessageID: "late", Timestamp: 400, Reported: map[string]any{"target": float64(-1), "battery": float64(85)}})
	if err != nil || s.Reported["target"] != target || s.Reported["battery"] != float64(85) || len(s.Delta) != 0 {
		t.Fatal("late partial report", s, err)
	}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := r.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, MessageID: fmt.Sprint(i), Timestamp: int64(600 + i), Reported: map[string]any{fmt.Sprintf("field%d", i): float64(i)}})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	s, err = r.GetDeviceShadow(ctx, tenant, device)
	if err != nil || len(s.Reported) != 18 {
		t.Fatal("lost concurrent properties", len(s.Reported), err)
	}
	history, err := r.ListShadowChanges(ctx, tenant, device, 20, 0)
	if err != nil || len(history) != 2 || history[0].Version != 2 {
		t.Fatal("desired audit", history, err)
	}
	foreign, err := r.GetDeviceShadow(ctx, "other", device)
	if err != nil || foreign.Version != 0 || len(foreign.Reported) != 0 {
		t.Fatal("tenant leak", foreign, err)
	}
	s, err = r.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, ExpectedVersion: 2, Desired: map[string]any{"target": nil}, Timestamp: 700})
	if err != nil || len(s.Desired) != 0 {
		t.Fatal("remove desired", s, err)
	}
}
