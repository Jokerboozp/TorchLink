package httpapi

import (
	"context"
	"slices"
	"sync"

	"iot-platform/internal/model"
	"iot-platform/internal/sites"
)

// accessCache keeps each tenant's access state for request authorization.
// Every lookup first reads the tenant's access and site revisions (two
// primary-key queries) and reuses the cached state only while both are
// unchanged, so a user, role, key or device placement change still takes
// effect on the very next request.
type accessCache struct {
	mu      sync.Mutex
	entries map[string]cachedAccess
}

type cachedAccess struct {
	state        model.AccessState
	siteRevision int64
}

// authorizationAccess returns the tenant's current access state for
// read-only use; callers must not modify it. Unit grants are already
// expanded into device IDs. Handlers that change access load a private,
// unexpanded copy with LoadAccessState instead.
func (s *Server) authorizationAccess(ctx context.Context, tenant string) (model.AccessState, error) {
	store, err := s.accessStore()
	if err != nil {
		return model.AccessState{}, err
	}
	revision, err := store.AccessRevision(ctx, tenant)
	if err != nil {
		return model.AccessState{}, err
	}
	siteRevision, err := s.sites.Revision(ctx, tenant)
	if err != nil {
		return model.AccessState{}, err
	}
	s.access.mu.Lock()
	cached, ok := s.access.entries[tenant]
	s.access.mu.Unlock()
	if ok && revision != 0 && cached.state.Revision == revision && cached.siteRevision == siteRevision {
		return cached.state, nil
	}
	state, err := store.LoadAccessState(ctx, tenant)
	if err != nil {
		return state, err
	}
	if siteRevision, err = s.expandUnitGrants(ctx, tenant, &state); err != nil {
		return state, err
	}
	s.access.mu.Lock()
	if s.access.entries == nil {
		s.access.entries = map[string]cachedAccess{}
	}
	if previous, exists := s.access.entries[tenant]; !exists || previous.state.Revision < state.Revision || (previous.state.Revision == state.Revision && previous.siteRevision <= siteRevision) {
		s.access.entries[tenant] = cachedAccess{state: state, siteRevision: siteRevision}
	}
	s.access.mu.Unlock()
	return state, nil
}

// expandUnitGrants adds the devices placed in each user's and role's
// granted units to their device IDs and returns the site revision used.
func (s *Server) expandUnitGrants(ctx context.Context, tenant string, state *model.AccessState) (int64, error) {
	needed := false
	for _, u := range state.Users {
		needed = needed || len(u.UnitIDs) > 0
	}
	for _, r := range state.Roles {
		needed = needed || len(r.UnitIDs) > 0
	}
	siteState, err := s.sites.Snapshot(ctx, tenant)
	if err != nil || !needed {
		return siteState.Revision, err
	}
	byUnit := map[string][]string{}
	for device, unit := range sites.DeviceUnits(siteState) {
		byUnit[unit] = append(byUnit[unit], device)
	}
	expand := func(scope string, ids, units []string) []string {
		if scope != "selected" || len(units) == 0 {
			return ids
		}
		out := slices.Clone(ids)
		for _, unit := range units {
			out = append(out, byUnit[unit]...)
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	for i := range state.Users {
		state.Users[i].DeviceIDs = expand(state.Users[i].DeviceScope, state.Users[i].DeviceIDs, state.Users[i].UnitIDs)
	}
	for i := range state.Roles {
		state.Roles[i].DeviceIDs = expand(state.Roles[i].DeviceScope, state.Roles[i].DeviceIDs, state.Roles[i].UnitIDs)
	}
	return siteState.Revision, nil
}
