package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"iot-platform/internal/model"
)

// protocolCacheTTL bounds how long another replica may keep using a product's
// previous protocol binding or a release's previous status; changes made
// through this process take effect immediately via ProtocolsChanged.
const protocolCacheTTL = 2 * time.Second

// protocolCache keeps product bindings and protocol releases briefly so every
// raw message does not read them from the database at ingest and again at parse.
type protocolCache struct {
	mu      sync.Mutex
	tenants map[string]*tenantProtocols
}

type tenantProtocols struct {
	bindings map[string]cachedBinding
	releases map[string]cachedRelease
}

type cachedBinding struct {
	binding  model.ProductProtocolBinding
	err      error
	loadedAt time.Time
}

type cachedRelease struct {
	release  model.ProtocolRelease
	loadedAt time.Time
}

func (c *protocolCache) tenant(tenantID string) *tenantProtocols {
	if c.tenants == nil {
		c.tenants = map[string]*tenantProtocols{}
	}
	t := c.tenants[tenantID]
	if t == nil {
		t = &tenantProtocols{bindings: map[string]cachedBinding{}, releases: map[string]cachedRelease{}}
		c.tenants[tenantID] = t
	}
	return t
}

// productBinding returns the product's protocol binding. A missing binding is
// cached too, so products parsed without a Go protocol skip the lookup.
func (e *Engine) productBinding(ctx context.Context, tenantID, productID string) (model.ProductProtocolBinding, error) {
	e.protocols.mu.Lock()
	entry, ok := e.protocols.tenant(tenantID).bindings[productID]
	e.protocols.mu.Unlock()
	if ok && time.Since(entry.loadedAt) < protocolCacheTTL {
		return entry.binding, entry.err
	}
	binding, err := e.Repo.GetProductProtocolBinding(ctx, tenantID, productID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return binding, err
	}
	e.protocols.mu.Lock()
	e.protocols.tenant(tenantID).bindings[productID] = cachedBinding{binding: binding, err: err, loadedAt: time.Now()}
	e.protocols.mu.Unlock()
	return binding, err
}

// protocolRelease returns a release. Only found releases are cached; the
// returned value shares its Config and must be treated as read-only.
func (e *Engine) protocolRelease(ctx context.Context, tenantID, protocolID, version string) (model.ProtocolRelease, error) {
	key := protocolID + "@" + version
	e.protocols.mu.Lock()
	entry, ok := e.protocols.tenant(tenantID).releases[key]
	e.protocols.mu.Unlock()
	if ok && time.Since(entry.loadedAt) < protocolCacheTTL {
		return entry.release, nil
	}
	release, err := e.Repo.GetProtocolRelease(ctx, tenantID, protocolID, version)
	if err != nil {
		return release, err
	}
	e.protocols.mu.Lock()
	e.protocols.tenant(tenantID).releases[key] = cachedRelease{release: release, loadedAt: time.Now()}
	e.protocols.mu.Unlock()
	return release, nil
}

// ProtocolsChanged must be called after a tenant's product bindings or protocol
// releases are switched, published, revoked or deleted.
func (e *Engine) ProtocolsChanged(tenantID string) {
	e.protocols.mu.Lock()
	delete(e.protocols.tenants, tenantID)
	e.protocols.mu.Unlock()
}
