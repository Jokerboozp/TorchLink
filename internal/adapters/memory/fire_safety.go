package memory

import (
	"context"
	"encoding/json"

	"iot-platform/internal/model"
)

func (r *Repository) LoadFireSafetyState(ctx context.Context, tenant string) (model.FireSafetyState, error) {
	var state model.FireSafetyState
	if err := ctx.Err(); err != nil {
		return state, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return state, err
	}
	if body := r.fireSafetyStates[tenant]; len(body) > 0 {
		if err := json.Unmarshal(body, &state); err != nil {
			return state, err
		}
	}
	return state, nil
}

func (r *Repository) SaveFireSafetyState(ctx context.Context, tenant string, state model.FireSafetyState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var old model.FireSafetyState
	if body := r.fireSafetyStates[tenant]; len(body) > 0 {
		if err := json.Unmarshal(body, &old); err != nil {
			return false, err
		}
	}
	if old.Revision != state.Revision {
		return false, nil
	}
	state.Revision++
	body, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r.fireSafetyStates == nil {
		r.fireSafetyStates = map[string][]byte{}
	}
	r.fireSafetyStates[tenant] = body
	return true, nil
}
