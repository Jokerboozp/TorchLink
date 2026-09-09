package memory

import (
	"context"
	"iot-platform/internal/model"
)

func (r *Repository) SetEdgeCredential(_ context.Context, tenant, id, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, id)
	if _, ok := r.edgeNodes[k]; !ok {
		return ErrNotFound
	}
	if r.edgeSecrets == nil {
		r.edgeSecrets = map[string]string{}
	}
	r.edgeSecrets[k] = hash
	return nil
}
func (r *Repository) GetEdgeCredential(_ context.Context, tenant, id string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.edgeSecrets[key(tenant, id)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (r *Repository) SaveEdgeHeartbeat(_ context.Context, tenant, id string, v model.EdgeHeartbeat) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, id)
	if _, ok := r.edgeNodes[k]; !ok {
		return ErrNotFound
	}
	if r.edgeHeartbeats == nil {
		r.edgeHeartbeats = map[string]model.EdgeHeartbeat{}
	}
	r.edgeHeartbeats[k] = clone(v)
	return nil
}
func (r *Repository) GetEdgeHeartbeat(_ context.Context, tenant, id string) (model.EdgeHeartbeat, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.edgeHeartbeats[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
