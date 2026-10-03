package notify

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryStore keeps notifications in process for development without
// PostgreSQL and for tests.
type MemoryStore struct {
	mu       sync.Mutex
	channels map[string]Channel
	secrets  map[string]string
	policies map[string]Policy
	tasks    []Task
	leases   map[int64]int64
	seq      int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{channels: map[string]Channel{}, secrets: map[string]string{}, policies: map[string]Policy{}, leases: map[int64]int64{}}
}

func key(tenant, id string) string { return tenant + "\x00" + id }

func (m *MemoryStore) ListChannels(_ context.Context, tenant string) ([]Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Channel{}
	for _, c := range m.channels {
		if c.TenantID == tenant {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemoryStore) GetChannel(_ context.Context, tenant, id string) (Channel, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.channels[key(tenant, id)]
	if !ok {
		return c, "", ErrNotFound
	}
	return c, m.secrets[key(tenant, id)], nil
}

func (m *MemoryStore) SaveChannel(_ context.Context, c Channel, sealed *string) (Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(c.TenantID, c.ID)
	old, exists := m.channels[k]
	if exists != (c.Version != 0) || exists && old.Version != c.Version {
		return c, ErrConflict
	}
	if sealed != nil {
		m.secrets[k] = *sealed
	}
	c.SecretSet = m.secrets[k] != ""
	c.Version++
	c.UpdatedAt = time.Now().UnixMilli()
	m.channels[k] = c
	return c, nil
}

func (m *MemoryStore) DeleteChannel(_ context.Context, tenant, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.channels[key(tenant, id)]; !ok {
		return ErrNotFound
	}
	delete(m.channels, key(tenant, id))
	delete(m.secrets, key(tenant, id))
	return nil
}

func (m *MemoryStore) ListPolicies(_ context.Context, tenant string) ([]Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Policy{}
	for _, p := range m.policies {
		if p.TenantID == tenant {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemoryStore) GetPolicy(_ context.Context, tenant, id string) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.policies[key(tenant, id)]
	if !ok {
		return p, ErrNotFound
	}
	return p, nil
}

func (m *MemoryStore) SavePolicy(_ context.Context, p Policy) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(p.TenantID, p.ID)
	old, exists := m.policies[k]
	if exists != (p.Version != 0) || exists && old.Version != p.Version {
		return p, ErrConflict
	}
	p.Version++
	p.UpdatedAt = time.Now().UnixMilli()
	m.policies[k] = p
	return p, nil
}

func (m *MemoryStore) DeletePolicy(_ context.Context, tenant, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[key(tenant, id)]; !ok {
		return ErrNotFound
	}
	delete(m.policies, key(tenant, id))
	return nil
}

func taskIdentity(t Task) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", t.TenantID, t.AlarmID, t.PolicyID, t.Stage, t.Kind, t.ChannelID)
}

func (m *MemoryStore) EnqueueTasks(_ context.Context, tasks []Task) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing := map[string]bool{}
	for _, t := range m.tasks {
		existing[taskIdentity(t)] = true
	}
	inserted := 0
	for _, t := range tasks {
		if existing[taskIdentity(t)] {
			continue
		}
		m.seq++
		t.ID = m.seq
		m.tasks = append(m.tasks, t)
		existing[taskIdentity(t)] = true
		inserted++
	}
	return inserted, nil
}

func (m *MemoryStore) ClaimDueTasks(_ context.Context, now, leaseMillis int64, limit int) ([]Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Task{}
	for i := range m.tasks {
		t := &m.tasks[i]
		due := t.Status == StatusPending && t.NextAt <= now || t.Status == StatusSending && m.leases[t.ID] <= now
		if !due || len(out) >= limit {
			continue
		}
		t.Status = StatusSending
		m.leases[t.ID] = now + leaseMillis
		out = append(out, *t)
	}
	return out, nil
}

func (m *MemoryStore) FinishTask(_ context.Context, t Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == t.ID {
			m.tasks[i] = t
			delete(m.leases, t.ID)
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryStore) ListAlarmTasks(_ context.Context, tenant, alarmID string) ([]Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Task{}
	for _, t := range m.tasks {
		if t.TenantID == tenant && t.AlarmID == alarmID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
