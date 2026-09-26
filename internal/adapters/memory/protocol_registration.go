package memory

import (
	"context"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) RegisterProtocolDevice(ctx context.Context, expected model.DeviceAccessProfile, id, name string) (model.ManagedDevice, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.ManagedDevice{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.accessProfiles[key(expected.TenantID, expected.ID)]
	d, err := model.ProtocolRegistrationDevice(expected, current, r.products[key(current.TenantID, current.ProductID)], id, name, time.Now().UnixMilli())
	if err != nil {
		return d, false, err
	}
	if current.EdgeNodeID != "" {
		return d, false, model.ErrProtocolRegistration
	}
	if existing, ok := r.devices[key(current.TenantID, id)]; ok {
		return cloneManaged(existing), false, model.RegisteredProtocolDevice(existing, current)
	}
	for _, existing := range r.devices {
		if existing.AccessKey == d.AccessKey {
			return model.ManagedDevice{}, false, model.ErrProtocolRegistration
		}
	}
	r.devices[key(d.TenantID, d.ID)] = cloneManaged(d)
	return cloneManaged(d), true, nil
}
