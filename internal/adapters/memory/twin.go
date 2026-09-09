package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
)

func (r *Repository) GetTwinTopology(_ context.Context, tenant string) (model.TwinTopology, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.twins[tenant]
	if !ok {
		return model.TwinTopology{TenantID: tenant, Relations: []model.TwinRelation{}}, nil
	}
	return clone(v), nil
}
func (r *Repository) UpdateTwinTopology(_ context.Context, u model.TwinUpdate) (model.TwinTopology, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.twins == nil {
		r.twins = map[string]model.TwinTopology{}
	}
	old := r.twins[u.TenantID]
	next, err := model.ApplyTwin(old, u)
	if err != nil {
		return old, err
	}
	for _, relation := range u.Add {
		for _, id := range []string{relation.Source, relation.Target} {
			if _, ok := r.devices[key(u.TenantID, id)]; !ok {
				return old, errors.New("twin relation device is absent from current tenant")
			}
		}
	}
	r.twins[u.TenantID] = clone(next)
	return clone(next), nil
}
func (r *Repository) GetTwinNodes(_ context.Context, tenant string, ids []string) ([]model.TwinNode, error) {
	if len(ids) > 200 {
		return nil, errors.New("twin node query exceeds 200")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.TwinNode{}
	for _, id := range ids {
		d, ok := r.devices[key(tenant, id)]
		if !ok {
			continue
		}
		s := r.states[key(tenant, id)]
		connection, business := s.ConnectionStatus, s.BusinessStatus
		if connection == "" {
			connection = "UNKNOWN"
		}
		if business == "" {
			business = "UNKNOWN"
		}
		out = append(out, model.TwinNode{ID: id, Name: d.Name, ProductID: d.ProductID, Status: d.Status, ConnectionStatus: connection, BusinessStatus: business, LastSeenAt: s.LastSeenAt})
	}
	return out, nil
}
