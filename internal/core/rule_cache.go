package core

import (
	"context"
	"sync"
	"time"

	"iot-platform/internal/model"
)

// ruleCacheTTL bounds how long another replica may keep evaluating a tenant's
// previous rules; changes made through this process take effect immediately.
const ruleCacheTTL = 2 * time.Second

// ruleCache keeps each tenant's rule list briefly so standard messages do not
// read every rule from the database one message at a time.
type ruleCache struct {
	mu      sync.Mutex
	entries map[string]ruleCacheEntry
}

type ruleCacheEntry struct {
	rules    []model.AlarmRule
	loadedAt time.Time
}

// tenantRules returns the tenant's rules. The returned slice is shared and
// must be treated as read-only.
func (e *Engine) tenantRules(ctx context.Context, tenantID string) ([]model.AlarmRule, error) {
	e.rules.mu.Lock()
	entry, ok := e.rules.entries[tenantID]
	e.rules.mu.Unlock()
	if ok && time.Since(entry.loadedAt) < ruleCacheTTL {
		return entry.rules, nil
	}
	rules, err := e.Repo.ListRules(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	e.rules.mu.Lock()
	if e.rules.entries == nil {
		e.rules.entries = map[string]ruleCacheEntry{}
	}
	e.rules.entries[tenantID] = ruleCacheEntry{rules: rules, loadedAt: time.Now()}
	e.rules.mu.Unlock()
	return rules, nil
}

// RulesChanged must be called after a tenant's rules are saved, enabled,
// disabled or deleted so the next message sees the change.
func (e *Engine) RulesChanged(tenantID string) {
	e.rules.mu.Lock()
	delete(e.rules.entries, tenantID)
	e.rules.mu.Unlock()
}
