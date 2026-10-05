package core

import (
	"context"
	"iot-platform/internal/model"
)

func (e *Engine) ReportConnection(ctx context.Context, tenant, product, device string, connected bool, at int64) error {
	unlock := e.lockDeviceState(tenant, device)
	defer unlock()
	before, after, written, err := e.mutateDeviceState(ctx, tenant, device, func(state *model.DeviceState, found bool) (bool, error) {
		if !found {
			interval, tolerance := e.deviceTiming(ctx, tenant, product, device)
			*state = model.DeviceState{TenantID: tenant, ProductID: product, DeviceID: device, DataStatus: "UNKNOWN", BusinessStatus: "UNKNOWN", ReportIntervalSec: interval, OfflineToleranceSec: tolerance}
		}
		state.ConnectionStatus = "DISCONNECTED"
		if !connected {
			state.LastDisconnectAt = at
		}
		if connected {
			state.ConnectionStatus = "CONNECTED"
			state.LastConnectAt = at
		}
		state.StatusSource = "LISTENER_SESSION"
		return true, nil
	})
	if err == nil && written {
		e.publishStateChange(ctx, before, after)
	}
	return err
}

// Bound the lock set without retaining an entry for every device ever seen.
func (e *Engine) lockDeviceState(tenant, device string) func() {
	var h uint32 = 2166136261
	for _, b := range []byte(tenant + "\x00" + device) {
		h = (h ^ uint32(b)) * 16777619
	}
	m := &e.stateLocks[h%uint32(len(e.stateLocks))]
	m.Lock()
	return m.Unlock
}
