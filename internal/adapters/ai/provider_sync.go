package aiadapter

import (
	"context"
	"errors"
	"strings"
	"sync"

	"iot-platform/internal/ports"
)

// ProviderSync keeps this API process and every Harness instance on the AI
// settings persisted in the database. API replicas each hold the provider in
// memory and a restarted Harness falls back to its environment defaults, so
// saving once is not enough: Reconcile runs periodically and repairs both.
// Saves in this process hold the same lock, so a reconciliation never pushes
// the previous settings between a save's Harness update and its persistence.
type ProviderSync struct {
	mu        sync.Mutex
	runtime   *RuntimeProvider
	harness   *HarnessPool
	store     ports.AIProviderConfigStore
	manifests ports.AIWorkflowManifestStore
	// complete fills deployment defaults the stored settings may omit.
	complete func(ports.AIPluginConfig) ports.AIPluginConfig
}

// NewProviderSync accepts nil harness, store or manifests for deployments
// without them.
func NewProviderSync(runtime *RuntimeProvider, harness *HarnessPool, store ports.AIProviderConfigStore, manifests ports.AIWorkflowManifestStore, complete func(ports.AIPluginConfig) ports.AIPluginConfig) *ProviderSync {
	if complete == nil {
		complete = func(c ports.AIPluginConfig) ports.AIPluginConfig { return c }
	}
	return &ProviderSync{runtime: runtime, harness: harness, store: store, manifests: manifests, complete: complete}
}

// Lock serialises a settings change with reconciliation in this process.
func (s *ProviderSync) Lock()   { s.mu.Lock() }
func (s *ProviderSync) Unlock() { s.mu.Unlock() }

// Reconcile applies the stored provider to this process and to every Harness
// instance that has not accepted it, then reconciles dynamic Agents.
func (s *ProviderSync) Reconcile(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.runtime.CurrentConfig()
	if s.store != nil {
		saved, found, err := s.store.LoadAIProviderConfig(ctx)
		if err != nil {
			return err
		}
		if found {
			target = s.complete(saved)
		}
	}
	unconfigured := normalizeProvider(target.Provider) == "deepseek" && strings.TrimSpace(target.APIKey) == ""
	var errs []error
	if target != s.runtime.CurrentConfig() && !unconfigured {
		// Another replica saved new settings.
		errs = append(errs, s.runtime.Configure(ctx, target))
	}
	if s.harness == nil {
		return errors.Join(errs...)
	}
	if !unconfigured {
		errs = append(errs, s.harness.SyncProvider(ctx, target))
	}
	if s.manifests != nil {
		errs = append(errs, s.syncWorkflows(ctx))
	}
	return errors.Join(errs...)
}

func (s *ProviderSync) syncWorkflows(ctx context.Context) error {
	desired, err := s.manifests.ListAIWorkflowManifests(ctx)
	if err != nil {
		return err
	}
	adopt, err := s.harness.SyncWorkflows(ctx, desired)
	errs := []error{err}
	for _, manifest := range adopt {
		// Agents created before the store existed become its first entries;
		// the next pass copies them to instances that lack them.
		errs = append(errs, s.manifests.SaveAIWorkflowManifest(ctx, ports.StoredAIWorkflowManifest{ID: manifest.ID, Manifest: manifest}))
	}
	return errors.Join(errs...)
}
