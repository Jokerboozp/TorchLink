package httpapi

import (
	"context"
	"sync"

	"iot-platform/internal/model"
)

// accessCache keeps each tenant's access state for request authorization.
// Every lookup first reads the tenant's revision (one primary-key query) and
// reuses the cached state only while the revision is unchanged, so a user,
// role or key change still takes effect on the very next request.
type accessCache struct {
	mu      sync.Mutex
	entries map[string]model.AccessState
}

// authorizationAccess returns the tenant's current access state for
// read-only use; callers must not modify it. Handlers that change access
// load a private copy with LoadAccessState instead.
func (s *Server) authorizationAccess(ctx context.Context, tenant string) (model.AccessState, error) {
	store, err := s.accessStore()
	if err != nil {
		return model.AccessState{}, err
	}
	revision, err := store.AccessRevision(ctx, tenant)
	if err != nil {
		return model.AccessState{}, err
	}
	s.access.mu.Lock()
	cached, ok := s.access.entries[tenant]
	s.access.mu.Unlock()
	if ok && revision != 0 && cached.Revision == revision {
		return cached, nil
	}
	state, err := store.LoadAccessState(ctx, tenant)
	if err != nil {
		return state, err
	}
	s.access.mu.Lock()
	if s.access.entries == nil {
		s.access.entries = map[string]model.AccessState{}
	}
	if previous, exists := s.access.entries[tenant]; !exists || previous.Revision <= state.Revision {
		s.access.entries[tenant] = state
	}
	s.access.mu.Unlock()
	return state, nil
}
