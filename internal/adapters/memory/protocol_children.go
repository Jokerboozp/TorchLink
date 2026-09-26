package memory

import (
	"context"
	"iot-platform/internal/model"
	"sort"
	"time"
)

func (r *Repository) ListManagedDeviceChildren(ctx context.Context, tenant, parent string, limit, offset int) ([]model.ManagedDevice, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := []model.ManagedDevice{}
	for _, d := range r.devices {
		if d.TenantID == tenant && d.GatewayID == parent {
			items = append(items, cloneManaged(d))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	total := len(items)
	offset = max(0, min(offset, total))
	limit = max(1, min(limit, 100))
	return items[offset:min(total, offset+limit)], total, nil
}

func (r *Repository) RegisterProtocolChild(ctx context.Context, expected model.DeviceAccessProfile, parentID string, identity model.ChildIdentity) (model.ManagedDevice, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.ManagedDevice{}, false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.accessProfiles[key(expected.TenantID, expected.ID)]
	parent := r.devices[key(expected.TenantID, parentID)]
	if r.products[key(expected.TenantID, parent.ProductID)].Status != "ENABLED" {
		return model.ManagedDevice{}, false, model.ErrProtocolRegistration
	}
	var productID string
	for _, v := range current.ChildProducts {
		if v.Type == identity.Type {
			productID = v.ProductID
		}
	}
	d, err := model.ProtocolChildDevice(expected, current, parent, r.products[key(expected.TenantID, productID)], identity, time.Now().UnixMilli())
	if err != nil {
		return d, false, err
	}
	binding, ok := r.protocolBindings[key(d.TenantID, d.ProductID)]
	release := r.protocolReleases[key(d.TenantID, binding.ProtocolID, binding.Version)]
	if !ok || release.Status != "PUBLISHED" {
		return d, false, model.ErrProtocolRegistration
	}
	for _, old := range r.devices {
		if old.AccessKey == d.AccessKey && (old.TenantID != d.TenantID || old.ID != d.ID) {
			return d, false, model.ErrProtocolRegistration
		}
	}
	created := true
	if old, ok := r.devices[key(d.TenantID, d.ID)]; ok {
		if err = model.ExistingProtocolChild(old, d); err != nil {
			return d, false, err
		}
		old.UpdatedAt = d.UpdatedAt
		if identity.Name != "" {
			old.Name = identity.Name
		}
		d = old
		created = false
	}
	parent.DeviceRole = "GATEWAY"
	r.devices[key(parent.TenantID, parent.ID)] = cloneManaged(parent)
	r.devices[key(d.TenantID, d.ID)] = cloneManaged(d)
	return cloneManaged(d), created, nil
}
