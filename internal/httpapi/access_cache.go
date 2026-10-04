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
	sites        *siteIndex
}

// siteIndex resolves unit grants: the unit each placed device belongs to and
// the devices of each unit. One index per tenant and site revision is shared
// by every request, so unit grants cost no per-user copies.
type siteIndex struct {
	unitOf  map[string]string
	devices map[string][]string
}

func newSiteIndex(state model.SiteState) *siteIndex {
	index := &siteIndex{unitOf: sites.DeviceUnits(state), devices: map[string][]string{}}
	for device, unit := range index.unitOf {
		index.devices[unit] = append(index.devices[unit], device)
	}
	for _, list := range index.devices {
		slices.Sort(list)
	}
	return index
}

// authorizationAccess returns the tenant's current access state for
// read-only use; callers must not modify it. Unit grants stay as unit IDs;
// scopeFor resolves them with the site index cached with the state.
// Handlers that change access load a private copy with LoadAccessState.
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
	siteState, err := s.sites.Snapshot(ctx, tenant)
	if err != nil {
		return state, err
	}
	entry := cachedAccess{state: state, siteRevision: siteState.Revision, sites: newSiteIndex(siteState)}
	s.access.mu.Lock()
	if s.access.entries == nil {
		s.access.entries = map[string]cachedAccess{}
	}
	if previous, exists := s.access.entries[tenant]; !exists || previous.state.Revision < state.Revision || (previous.state.Revision == state.Revision && previous.siteRevision <= entry.siteRevision) {
		s.access.entries[tenant] = entry
	}
	s.access.mu.Unlock()
	return state, nil
}

// cachedSiteIndex returns the site index cached with the tenant's access
// state, loaded by the authorizationAccess call that preceded it.
func (s *Server) cachedSiteIndex(tenant string) *siteIndex {
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	return s.access.entries[tenant].sites
}
