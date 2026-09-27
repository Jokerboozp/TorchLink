package aiadapter

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

// HarnessPool spreads workflow runs over several Harness instances. A chat
// conversation keeps its history inside one instance, so every run is routed
// by rendezvous hashing of its conversation (or run) ID: the same conversation
// always reaches the same instance while it is up. Provider settings and
// dynamic workflow manifests are applied to every instance.
type HarnessPool struct {
	members []*HarnessClient
	urls    []string
}

// NewHarnessPool accepts one or more comma-separated Harness base URLs.
func NewHarnessPool(baseURLs, token, mcpURL, model string, timeout time.Duration) (*HarnessPool, error) {
	p := &HarnessPool{}
	for _, raw := range strings.Split(baseURLs, ",") {
		if raw = strings.TrimRight(strings.TrimSpace(raw), "/"); raw == "" {
			continue
		}
		member, err := NewHarness(raw, token, mcpURL, model, timeout)
		if err != nil {
			return nil, err
		}
		p.members = append(p.members, member)
		p.urls = append(p.urls, raw)
	}
	if len(p.members) == 0 {
		return nil, errors.New("at least one Harness URL is required")
	}
	return p, nil
}

// Size is the number of Harness instances.
func (p *HarnessPool) Size() int { return len(p.members) }

// order returns members by rendezvous score for key; the first is the owner.
func (p *HarnessPool) order(key string) []int {
	type scored struct {
		i     int
		score uint64
	}
	s := make([]scored, len(p.members))
	for i, u := range p.urls {
		h := fnv.New64a()
		_, _ = h.Write([]byte(u))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(key))
		s[i] = scored{i, mix64(h.Sum64())}
	}
	sort.Slice(s, func(a, b int) bool { return s[a].score > s[b].score })
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = v.i
	}
	return out
}

// mix64 is the SplitMix64 finalizer; FNV alone spreads keys poorly when the
// URLs differ only in a few characters.
func mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// Owner reports which instance URL serves a conversation (for diagnostics).
func (p *HarnessPool) Owner(conversationID string) string { return p.urls[p.order(conversationID)[0]] }

// unreachable reports errors raised before the request reached an instance,
// the only case where another instance may take the run.
func unreachable(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func (p *HarnessPool) StreamChat(ctx context.Context, in ports.AIWorkflowRequest, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	key := in.ConversationID
	if key == "" {
		key = in.RunID
	}
	var lastErr error
	for _, i := range p.order(key) {
		result, err := p.members[i].StreamChat(ctx, in, emit)
		if err == nil || !unreachable(err) {
			return result, err
		}
		// The owner is down: its conversation history is unavailable either
		// way, so the next instance starts a fresh session.
		lastErr = err
	}
	return ports.AIWorkflowResult{}, lastErr
}

func (p *HarnessPool) ConfigureProvider(ctx context.Context, config ports.AIPluginConfig) error {
	var errs []error
	for i, m := range p.members {
		if err := m.ConfigureProvider(ctx, config); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.urls[i], err))
		}
	}
	return errors.Join(errs...)
}

func (p *HarnessPool) CurrentConfig() ports.AIPluginConfig { return p.members[0].CurrentConfig() }

func (p *HarnessPool) ListWorkflows(ctx context.Context) ([]ports.AIWorkflowPlugin, error) {
	var lastErr error
	for _, m := range p.members {
		items, err := m.ListWorkflows(ctx)
		if err == nil {
			return items, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (p *HarnessPool) ListWorkflowManifests(ctx context.Context) ([]ports.AIWorkflowManifest, error) {
	var lastErr error
	for _, m := range p.members {
		items, err := m.ListWorkflowManifests(ctx)
		if err == nil {
			return items, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// SaveWorkflow stores a dynamic manifest on every instance so any of them can
// run it; a partial failure is reported so the operator can retry.
func (p *HarnessPool) SaveWorkflow(ctx context.Context, manifest ports.AIWorkflowManifest) (ports.AIWorkflowPlugin, error) {
	var first ports.AIWorkflowPlugin
	var errs []error
	for i, m := range p.members {
		item, err := m.SaveWorkflow(ctx, manifest)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.urls[i], err))
			continue
		}
		if first.ID == "" {
			first = item
		}
	}
	return first, errors.Join(errs...)
}

func (p *HarnessPool) DeleteWorkflow(ctx context.Context, workflowID string) error {
	var errs []error
	for i, m := range p.members {
		if err := m.DeleteWorkflow(ctx, workflowID); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.urls[i], err))
		}
	}
	return errors.Join(errs...)
}

// Health fails only when no instance is healthy; conversations owned by a
// down instance fail over to the next instance.
func (p *HarnessPool) Health(ctx context.Context) error {
	var errs []error
	for i, m := range p.members {
		if err := m.Health(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.urls[i], err))
		}
	}
	if len(errs) == len(p.members) {
		return errors.Join(errs...)
	}
	return nil
}
