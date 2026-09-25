package memory

import (
	"context"

	"iot-platform/internal/model"
)

func (r *Repository) SwitchProductProtocol(_ context.Context, v model.ProtocolSwitch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	pk := key(v.Product.TenantID, v.Product.ID)
	if _, ok := r.products[pk]; !ok {
		return ErrNotFound
	}
	current, ok := r.protocolBindings[pk]
	if ok != (v.Expected != nil) || ok && (current.ProtocolID != v.Expected.ProtocolID || current.Version != v.Expected.Version) {
		return model.ErrBindingChanged
	}
	r.protocols[key(v.Package.TenantID, v.Package.ID)] = clone(v.Package)
	r.products[pk] = clone(v.Product)
	r.protocolBindings[pk] = clone(v.Binding)
	return nil
}
