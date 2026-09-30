package memory

import (
	"context"
	"slices"
)

func (r *Repository) GovernanceTenants(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []string{}
	for _, d := range r.governanceDocuments {
		if !slices.Contains(out, d.TenantID) {
			out = append(out, d.TenantID)
		}
	}
	slices.Sort(out)
	return out, nil
}
