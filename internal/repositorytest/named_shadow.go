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

func NamedShadows(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const tenant, device = "named-shadow-tenant", "device"
	for _, name := range []string{"", "control"} {
		if _, err := r.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, Name: name, Desired: map[string]any{"target": float64(42)}, Timestamp: 100}); err != nil {
			t.Fatal(err)
		}
	}
	u := model.ShadowUpdate{TenantID: tenant, DeviceID: device, Name: "control", MessageID: "report", Reported: map[string]any{"target": float64(42)}, Timestamp: 200}
	s, err := r.UpdateDeviceShadow(ctx, u)
	if err != nil || s.Name != "control" || len(s.Delta) != 0 {
		t.Fatal("named reconciliation", s, err)
	}
	unnamed, err := r.GetDeviceShadow(ctx, tenant, device)
	if err != nil || unnamed.Name != "" || len(unnamed.Reported) != 0 || len(unnamed.Delta) != 1 {
		t.Fatal("default shadow changed", unnamed, err)
	}
	u.ExpectedVersion, u.Reported, u.Desired = 1, nil, map[string]any{"target": float64(60)}
	if _, err = r.UpdateDeviceShadow(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err = r.UpdateDeviceShadow(ctx, u); !errors.Is(err, model.ErrShadowConflict) {
		t.Fatal("stale named desired", err)
	}
	history, err := r.ListShadowChanges(ctx, tenant, device, 20, 0, "control")
	if err != nil || len(history) != 2 || history[0].Version != 2 {
		t.Fatal("named history", history, err)
	}
	history, err = r.ListShadowChanges(ctx, tenant, device, 20, 0)
	if err != nil || len(history) != 1 || history[0].Version != 1 {
		t.Fatal("default history changed", history, err)
	}
	for _, name := range []string{"../escape", "bad/name"} {
		if _, err := r.GetDeviceShadow(ctx, tenant, device, name); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := r.UpdateDeviceShadow(ctx, model.ShadowUpdate{TenantID: tenant, DeviceID: device, Name: fmt.Sprintf("part-%02d", i), MessageID: "report", Reported: map[string]any{"target": float64(i)}, Timestamp: 200})
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, model.ErrShadowCount) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	names, err := r.ListDeviceShadowNames(ctx, tenant, device)
	if err != nil || wins.Load() != 15 || len(names) != 16 {
		t.Fatal("concurrent named shadow capacity", wins.Load(), names, err)
	}
	foreign, err := r.ListDeviceShadowNames(ctx, "foreign", device)
	if err != nil || len(foreign) != 0 {
		t.Fatal("foreign names", foreign, err)
	}
	// Existing names remain writable at capacity; the count is a creation bound.
	u.ExpectedVersion, u.Timestamp = 2, 300
	u.Desired = map[string]any{"target": float64(61)}
	if _, err := r.UpdateDeviceShadow(ctx, u); err != nil {
		t.Fatal("capacity blocked existing shadow", err)
	}
}
