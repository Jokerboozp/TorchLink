package memory

import (
	"context"
	"iot-platform/internal/model"
)

func (r *Repository) GetDeviceShadow(ctx context.Context, tenant, device string) (model.DeviceShadow, error) {
	if err := ctx.Err(); err != nil {
		return model.DeviceShadow{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := clone(r.shadows[key(tenant, device)])
	s.TenantID, s.DeviceID = tenant, device
	if s.Desired == nil {
		s.Desired = map[string]any{}
	}
	if s.Reported == nil {
		s.Reported = map[string]any{}
	}
	s.ComputeDelta()
	return s, nil
}
func (r *Repository) UpdateDeviceShadow(ctx context.Context, u model.ShadowUpdate) (model.DeviceShadow, error) {
	if err := ctx.Err(); err != nil {
		return model.DeviceShadow{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(u.TenantID, u.DeviceID)
	s := clone(r.shadows[k])
	changed, err := model.ApplyShadow(&s, u)
	if err != nil {
		return model.DeviceShadow{}, err
	}
	if changed {
		if r.shadows == nil {
			r.shadows = map[string]model.DeviceShadow{}
		}
		r.shadows[k] = clone(s)
		if u.Desired != nil {
			if r.shadowChanges == nil {
				r.shadowChanges = map[string][]model.ShadowChange{}
			}
			r.shadowChanges[k] = append(r.shadowChanges[k], model.ShadowChange{Version: s.DesiredVersion, Timestamp: u.Timestamp, Actor: u.Actor, Desired: clone(s.Desired)})
			if len(r.shadowChanges[k]) > 1000 {
				r.shadowChanges[k] = r.shadowChanges[k][len(r.shadowChanges[k])-1000:]
			}
		}
	}
	return clone(s), nil
}
func (r *Repository) ListShadowChanges(ctx context.Context, tenant, device string, limit, offset int) ([]model.ShadowChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.ShadowChange{}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	items := r.shadowChanges[key(tenant, device)]
	for i := len(items) - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, clone(items[i]))
	}
	return out, nil
}
