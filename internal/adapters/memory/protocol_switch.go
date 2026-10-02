package memory

import (
	"context"
	"time"

	"iot-platform/internal/model"
)

func (r *Repository) SwitchProductProtocol(ctx context.Context, v model.ProtocolSwitch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	pk := key(v.Product.TenantID, v.Product.ID)
	product, exists := r.products[pk]
	create := v.Preparation != nil && v.Preparation.CreateProduct
	if create && exists {
		return model.ErrOnboardingChanged
	}
	if !create && !exists {
		return ErrNotFound
	}
	if create {
		// Validate against the proposed new identity without persisting anything
		// until all snapshot, listener and record checks have succeeded.
		product = v.Product
	}
	if v.RequireUnused {
		for _, device := range r.devices {
			if device.TenantID == v.Product.TenantID && device.ProductID == v.Product.ID {
				return model.ErrOnboardingChanged
			}
		}
	}
	current, ok := r.protocolBindings[pk]
	if ok != (v.Expected != nil) || ok && (current.ProtocolID != v.Expected.ProtocolID || current.Version != v.Expected.Version) {
		return model.ErrBindingChanged
	}
	if change := v.Preparation; change != nil {
		profiles := []model.DeviceAccessProfile{}
		for _, p := range r.accessProfiles {
			if p.TenantID == v.Product.TenantID && p.ProductID == v.Product.ID && p.DeviceID == "" {
				profiles = append(profiles, p)
			}
		}
		if !model.SameTemplateSnapshot(product, change.ExpectedProduct, profiles, change.ExpectedProfiles) {
			return model.ErrOnboardingChanged
		}
		rec, found := r.onboardingRecords[key(change.Record.TenantID, change.Record.ID)]
		if found != (change.ExpectedRevision > 0) || found && (rec.Revision != change.ExpectedRevision || rec.OwnerID != change.Record.OwnerID || rec.Kind != change.Record.Kind) {
			return model.ErrOnboardingChanged
		}
		for _, p := range change.Profiles {
			if p.TenantID != v.Product.TenantID || p.ProductID != v.Product.ID || p.DeviceID != "" {
				return model.ErrOnboardingChanged
			}
			if other, found := r.accessProfiles[key(p.TenantID, p.ID)]; found && (other.ProductID != p.ProductID || other.DeviceID != "") {
				return model.ErrOnboardingChanged
			}
			for _, other := range r.accessProfiles {
				if other.ProductID == p.ProductID && other.TenantID == p.TenantID {
					continue
				}
				if p.Enabled && other.Enabled && p.Mode == "listener" && other.Mode == "listener" && p.ConnectionMode != "dial" && other.ConnectionMode != "dial" && p.Network == other.Network && p.Port == other.Port {
					return model.ErrBindingChanged
				}
			}
		}
		keep := map[string]bool{}
		for _, p := range change.Profiles {
			keep[p.ID] = true
		}
		for _, p := range profiles {
			if !keep[p.ID] {
				for _, d := range r.devices {
					if d.TenantID == p.TenantID && d.ConnectorProfileID == p.ID {
						return model.ErrOnboardingChanged
					}
				}
			}
		}
		for _, p := range profiles {
			if !keep[p.ID] {
				delete(r.accessProfiles, key(p.TenantID, p.ID))
			}
		}
		for _, p := range change.Profiles {
			r.accessProfiles[key(p.TenantID, p.ID)] = clone(p)
		}
		next := change.Record
		next.Revision = change.ExpectedRevision + 1
		next.UpdatedAt = time.Now().UnixMilli()
		next.CreatedAt = rec.CreatedAt
		if next.CreatedAt == 0 {
			next.CreatedAt = next.UpdatedAt
		}
		if r.onboardingRecords == nil {
			r.onboardingRecords = map[string]model.OnboardingRecord{}
		}
		r.onboardingRecords[key(next.TenantID, next.ID)] = clone(next)
	}
	r.protocols[key(v.Package.TenantID, v.Package.ID)] = clone(v.Package)
	r.products[pk] = clone(v.Product)
	r.protocolBindings[pk] = clone(v.Binding)
	return nil
}
