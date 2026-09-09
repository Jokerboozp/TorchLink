package memory

import (
	"context"
	"iot-platform/internal/model"
	"sort"
)

func shadowKey(tenant, device, name string) string {
	if name == "" {
		return key(tenant, device)
	}
	return key(tenant, device) + "\x00" + name
}
func (r *Repository) GetDeviceShadow(ctx context.Context, tenant, device string, names ...string) (model.DeviceShadow, error) {
	name, err := model.ShadowName(names...)
	if err != nil {
		return model.DeviceShadow{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.DeviceShadow{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := clone(r.shadows[shadowKey(tenant, device, name)])
	s.TenantID, s.DeviceID, s.Name = tenant, device, name
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
	k := shadowKey(u.TenantID, u.DeviceID, u.Name)
	s := clone(r.shadows[k])
	if u.Name != "" && s.TenantID == "" {
		count := 0
		for _, item := range r.shadows {
			if item.TenantID == u.TenantID && item.DeviceID == u.DeviceID && item.Name != "" {
				count++
			}
		}
		if count >= 16 {
			return model.DeviceShadow{}, model.ErrShadowCount
		}
	}
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
func (r *Repository) ListShadowChanges(ctx context.Context, tenant, device string, limit, offset int, names ...string) ([]model.ShadowChange, error) {
	name, err := model.ShadowName(names...)
	if err != nil {
		return nil, err
	}
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
	items := r.shadowChanges[shadowKey(tenant, device, name)]
	for i := len(items) - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, clone(items[i]))
	}
	return out, nil
}

func (r *Repository) ListDeviceShadowNames(ctx context.Context, tenant, device string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := []string{}
	for _, shadow := range r.shadows {
		if shadow.TenantID == tenant && shadow.DeviceID == device && shadow.Name != "" {
			names = append(names, shadow.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}
