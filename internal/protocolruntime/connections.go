package protocolruntime

import (
	"context"
	"iot-platform/internal/model"
	"time"
)

// Configure before Start. Counts include every listener for a device: closing
// one of several sessions must not project a false disconnect.
func (r *Listeners) SetConnectionReporter(f func(context.Context, string, string, string, bool, int64) error) {
	r.connectionReporter = f
}
func (r *Listeners) reportConnection(p model.DeviceAccessProfile, device string, connected bool) {
	r.connectionMu.Lock()
	defer r.connectionMu.Unlock()
	if r.connectionCounts == nil {
		r.connectionCounts = map[string]int{}
	}
	key := p.TenantID + "\x00" + device
	n := r.connectionCounts[key]
	if connected {
		r.connectionCounts[key] = n + 1
		if n > 0 {
			return
		}
	} else {
		if n <= 0 {
			return
		}
		if n > 1 {
			r.connectionCounts[key] = n - 1
			return
		}
		delete(r.connectionCounts, key)
	}
	if r.connectionReporter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := r.connectionReporter(ctx, p.TenantID, p.ProductID, device, connected, time.Now().UnixMilli()); e != nil && r.log != nil {
			r.log.Warn("listener connection projection failed", "device", device, "error", e)
		}
	}
}
