package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) AcquireExecutionLease(ctx context.Context, tenant, resource, owner, endpoint string, ttl time.Duration) (model.ExecutionLease, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.ExecutionLease{}, false, err
	}
	if tenant == "" || resource == "" || owner == "" || ttl < time.Second || ttl > time.Minute {
		return model.ExecutionLease{}, false, errors.New("invalid execution lease")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.leases == nil {
		r.leases = map[string]model.ExecutionLease{}
	}
	k := key(tenant, resource)
	v, exists := r.leases[k]
	now := time.Now().UnixMilli()
	if exists && v.ExpiresAt > now && v.Owner != owner {
		return v, false, nil
	}
	if !exists {
		v = model.ExecutionLease{TenantID: tenant, Resource: resource, Token: 1}
	} else if v.ExpiresAt <= now {
		v.Token++
	}
	v.Owner, v.Endpoint, v.ExpiresAt = owner, endpoint, now+ttl.Milliseconds()
	r.leases[k] = v
	return v, true, nil
}
func (r *Repository) GetExecutionLease(_ context.Context, tenant, resource string) (model.ExecutionLease, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.leases[key(tenant, resource)]
	if !ok || v.ExpiresAt <= time.Now().UnixMilli() {
		return v, ErrNotFound
	}
	return v, nil
}
func (r *Repository) ReleaseExecutionLease(_ context.Context, lease model.ExecutionLease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(lease.TenantID, lease.Resource)
	v, ok := r.leases[k]
	if ok && v.Owner == lease.Owner && v.Token == lease.Token {
		v.ExpiresAt = 0
		r.leases[k] = v
	}
	return nil
}
