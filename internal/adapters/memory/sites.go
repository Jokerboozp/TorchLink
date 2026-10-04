package memory

import (
	"context"
	"encoding/json"

	"iot-platform/internal/model"
)

func (r *Repository) LoadSiteState(ctx context.Context, tenant string) (model.SiteState, error) {
	var state model.SiteState
	if err := ctx.Err(); err != nil {
		return state, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if body := r.siteStates[tenant]; len(body) > 0 {
		if err := json.Unmarshal(body, &state); err != nil {
			return state, err
		}
	}
	return state, nil
}

func (r *Repository) SaveSiteState(ctx context.Context, tenant string, base, state model.SiteState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var old model.SiteState
	if body := r.siteStates[tenant]; len(body) > 0 {
		if err := json.Unmarshal(body, &old); err != nil {
			return false, err
		}
	}
	if old.Revision != base.Revision {
		return false, nil
	}
	state.Revision = base.Revision + 1
	body, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	if r.siteStates == nil {
		r.siteStates = map[string][]byte{}
	}
	r.siteStates[tenant] = body
	return true, nil
}

func (r *Repository) SiteRevision(ctx context.Context, tenant string) (int64, error) {
	state, err := r.LoadSiteState(ctx, tenant)
	return state.Revision, err
}
