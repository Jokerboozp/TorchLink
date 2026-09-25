package memory

import (
	"context"
	"sort"

	"iot-platform/internal/model"
)

// Ops center preferences (saved queries, query history, favorites) are keyed
// by tenant and username so they never leak between accounts.

func (r *Repository) ListOpsItems(_ context.Context, tenantID, username, kind string, limit int) ([]model.OpsUserItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.OpsUserItem{}
	for _, item := range r.opsItems {
		if item.TenantID == tenantID && item.Username == username && item.Kind == kind {
			item.Body = cloneBody(item.Body)
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *Repository) SaveOpsItem(_ context.Context, item model.OpsUserItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.opsItems == nil {
		r.opsItems = map[string]model.OpsUserItem{}
	}
	k := key(item.TenantID, item.Username, item.Kind, item.ID)
	if existing, ok := r.opsItems[k]; ok && existing.CreatedAt > 0 {
		item.CreatedAt = existing.CreatedAt
	}
	item.Body = cloneBody(item.Body)
	r.opsItems[k] = item
	return nil
}

func (r *Repository) DeleteOpsItem(_ context.Context, tenantID, username, kind, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenantID, username, kind, id)
	if _, ok := r.opsItems[k]; !ok {
		return false, nil
	}
	delete(r.opsItems, k)
	return true, nil
}

func (r *Repository) TrimOpsItems(ctx context.Context, tenantID, username, kind string, keep int) error {
	items, _ := r.ListOpsItems(ctx, tenantID, username, kind, 0)
	if len(items) <= keep {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range items[keep:] {
		delete(r.opsItems, key(tenantID, username, kind, item.ID))
	}
	return nil
}

func cloneBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = v
	}
	return out
}
