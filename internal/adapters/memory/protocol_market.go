package memory

import (
	"context"
	"iot-platform/internal/model"
	"sort"
)

func (r *Repository) ListProtocolMarket(_ context.Context, tenant string) ([]model.ProtocolMarketEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := []model.ProtocolMarketEntry{}
	for _, v := range r.protocolMarket {
		if v.TenantID == tenant {
			items = append(items, clone(v))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ProtocolID == items[j].ProtocolID {
			return items[i].Version < items[j].Version
		}
		return items[i].ProtocolID < items[j].ProtocolID
	})
	return items, nil
}
func (r *Repository) GetProtocolMarket(_ context.Context, tenant, id, version string) (model.ProtocolMarketEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.protocolMarket[key(tenant, id, version)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) SubmitProtocolMarket(_ context.Context, v model.ProtocolMarketEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ProtocolID, v.Version)
	if _, ok := r.protocolMarket[k]; ok {
		return model.ErrMarketConflict
	}
	count := 0
	for _, entry := range r.protocolMarket {
		if entry.TenantID == v.TenantID {
			count++
		}
	}
	if count >= 1000 {
		return model.ErrMarketLimit
	}
	if r.protocolMarket == nil {
		r.protocolMarket = map[string]model.ProtocolMarketEntry{}
	}
	r.protocolMarket[k] = clone(v)
	return nil
}
func (r *Repository) ReviewProtocolMarket(_ context.Context, tenant, id, version, actor, decision, reason string, expected int64) (model.ProtocolMarketEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, id, version)
	v, ok := r.protocolMarket[k]
	if !ok {
		return v, ErrNotFound
	}
	v, err := v.Review(actor, decision, reason, expected)
	if err != nil {
		return v, err
	}
	r.protocolMarket[k] = clone(v)
	return clone(v), nil
}
