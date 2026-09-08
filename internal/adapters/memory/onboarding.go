package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
)

func (r *Repository) SaveOnboarding(_ context.Context, b model.OnboardingBundle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := b.Device
	k := key(d.TenantID, d.ID)
	if _, ok := r.devices[k]; ok {
		return errors.New("device already exists")
	}
	pk := key(d.TenantID, d.ProductID)
	if b.Product != nil {
		if _, ok := r.products[pk]; ok {
			return errors.New("product already exists")
		}
	} else if p, ok := r.products[pk]; !ok || p.Status != "ENABLED" {
		return errors.New("product is disabled")
	}
	for _, other := range r.devices {
		if other.AccessKey == d.AccessKey {
			return errors.New("access key already exists")
		}
	}
	if v := b.Release; v != nil {
		if old, ok := r.protocolReleases[key(v.TenantID, v.ProtocolID, v.Version)]; ok && (old.Status != "PUBLISHED" || old.ParserType != v.ParserType) {
			return errors.New("protocol release changed; test again")
		}
	}
	if v := b.Profile; v != nil {
		if _, ok := r.accessProfiles[key(v.TenantID, v.ID)]; ok && !b.ReuseProfile {
			return errors.New("profile already exists")
		}
		if b.ReuseProfile {
			old, ok := r.accessProfiles[key(v.TenantID, v.ID)]
			if !ok || !old.Enabled || old.ProductID != v.ProductID || old.Host != v.Host || old.Port != v.Port || old.Network != v.Network {
				return errors.New("listener changed; test again")
			}
		}
		binding, ok := r.protocolBindings[key(v.TenantID, v.ProductID)]
		if !ok && b.Binding != nil {
			binding = *b.Binding
		}
		if binding.ProtocolID != v.ProtocolID || binding.Version != v.ProtocolVersion {
			return errors.New("product binding changed; test again")
		}
		for _, p := range r.accessProfiles {
			if !b.ReuseProfile && p.Enabled && p.Mode == "listener" && v.Mode == "listener" && p.Network == v.Network && p.Port == v.Port {
				return errors.New("listener port is already reserved")
			}
		}
	}
	if v := b.Release; v != nil {
		rk := key(v.TenantID, v.ProtocolID, v.Version)
		if _, ok := r.protocolReleases[rk]; !ok {
			r.protocolReleases[rk] = clone(*v)
		}
		dk := key(v.TenantID, v.ProtocolID)
		if _, ok := r.protocolDefinitions[dk]; !ok {
			r.protocolDefinitions[dk] = model.ProtocolDefinition{TenantID: v.TenantID, ID: v.ProtocolID, Name: v.ProtocolID, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt}
		}
	}
	if v := b.PointTable; v != nil {
		pk := key(v.TenantID, v.ProtocolID, v.Version)
		if _, ok := r.pointTables[pk]; !ok {
			r.pointTables[pk] = clone(*v)
		}
	}
	if b.Product != nil {
		r.products[pk] = clone(*b.Product)
		v := b.Release
		pkg := model.ProtocolPackage{TenantID: v.TenantID, ID: b.Product.ProtocolPackageID, Name: v.ProtocolID, Version: v.Version, Protocol: v.ProtocolID, Transport: v.Transport, PayloadFormat: v.PayloadFormat, ParserType: v.ParserType, Status: v.Status, Config: v.Config, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt}
		if _, ok := r.protocols[key(pkg.TenantID, pkg.ID)]; !ok {
			r.protocols[key(pkg.TenantID, pkg.ID)] = clone(pkg)
		}
	}
	if v := b.Binding; v != nil {
		bk := key(v.TenantID, v.ProductID)
		if _, ok := r.protocolBindings[bk]; !ok {
			r.protocolBindings[bk] = *v
		}
	}
	if v := b.Profile; v != nil && !b.ReuseProfile {
		r.accessProfiles[key(v.TenantID, v.ID)] = clone(*v)
	}
	saved := clone(d)
	saved.SecretHash = d.SecretHash
	r.devices[k] = saved
	return nil
}
