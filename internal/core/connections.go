package core

import (
	"context"
	"iot-platform/internal/model"
)

func (e *Engine) ReportConnection(ctx context.Context, tenant, product, device string, connected bool, at int64) error {
	unlock := e.lockDeviceState(tenant, device)
	defer unlock()
	state, err := e.Repo.GetDeviceState(ctx, tenant, device)
	if err != nil {
		state = model.DeviceState{TenantID: tenant, ProductID: product, DeviceID: device, DataStatus: "UNKNOWN", BusinessStatus: "UNKNOWN", ReportIntervalSec: 300, OfflineToleranceSec: 60}
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
	return e.updateDeviceState(ctx, state)
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
