package memory

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
)

func (r *Repository) LoadAccessState(_ context.Context, tenant string) (model.AccessState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var state model.AccessState
	if b := r.accessStates[tenant]; len(b) > 0 {
		if err := json.Unmarshal(b, &state); err != nil {
			return state, err
		}
	}
	return state, nil
}
func (r *Repository) SaveAccessState(_ context.Context, tenant string, state model.AccessState) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var old model.AccessState
	if b := r.accessStates[tenant]; len(b) > 0 {
		if err := json.Unmarshal(b, &old); err != nil {
			return false, err
		}
	}
	if old.Revision != state.Revision {
		return false, nil
	}
	state.Revision++
	b, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	if r.accessStates == nil {
		r.accessStates = map[string][]byte{}
	}
	r.accessStates[tenant] = b
	return true, nil
}
