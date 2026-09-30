package analytics

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"sync"
	"time"

	"iot-platform/internal/model"
)

// MemoryStore shares the same transitions and validation as PostgreSQL. It is
// intended for tests; production workers require the PostgreSQL implementation.
type memoryBackend struct {
	mu   sync.Mutex
	docs map[string]StorageDocument
}
type memoryTx struct {
	tenant   string
	docs     map[string]StorageDocument
	readOnly bool
}

func NewMemoryStore() *Store { return NewMemoryStoreWithClock(time.Now) }
func NewMemoryStoreWithClock(now func() time.Time) *Store {
	s := NewStore(&memoryBackend{docs: map[string]StorageDocument{}})
	if now != nil {
		s.now = now
	}
	return s
}

func memoryKey(tenant, kind, id string) string { return tenant + "\x00" + kind + "\x00" + id }
func cloneStorage(d StorageDocument) StorageDocument {
	d.Body = slices.Clone(d.Body)
	d.DeviceIDs = slices.Clone(d.DeviceIDs)
	return d
}
func (b *memoryBackend) Transaction(ctx context.Context, tenant string, fn func(StorageTx) error) error {
	if tenant == "" {
		return model.ErrAnalysisInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	copyDocs := make(map[string]StorageDocument, len(b.docs))
	for k, d := range b.docs {
		copyDocs[k] = cloneStorage(d)
	}
	if err := fn(&memoryTx{tenant: tenant, docs: copyDocs}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.docs = copyDocs
	return nil
}
func (b *memoryBackend) Read(ctx context.Context, tenant string, fn func(StorageTx) error) error {
	if tenant == "" {
		return model.ErrAnalysisInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return fn(&memoryTx{tenant: tenant, docs: b.docs, readOnly: true})
}
func (b *memoryBackend) WorkTenants(ctx context.Context, kinds []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	set := map[string]bool{}
	for _, d := range b.docs {
		if d.Kind == "run" && slices.Contains(kinds, d.ApplicationKind) && slices.Contains([]string{model.AnalysisQueued, model.AnalysisPreparing, model.AnalysisRunning}, d.Status) {
			set[d.TenantID] = true
		}
	}
	out := make([]string, 0, len(set))
	for tenant := range set {
		out = append(out, tenant)
	}
	sort.Strings(out)
	return out, nil
}
func (tx *memoryTx) Get(kind, id string) (StorageDocument, error) {
	d, ok := tx.docs[memoryKey(tx.tenant, kind, id)]
	if !ok {
		return StorageDocument{}, model.ErrNotFound
	}
	return cloneStorage(d), nil
}
func (tx *memoryTx) Put(d StorageDocument, expected int64) error {
	if tx.readOnly {
		return model.ErrAnalysisConflict
	}
	if d.TenantID != tx.tenant || d.Version != expected+1 || !json.Valid(d.Body) {
		return model.ErrAnalysisInvalid
	}
	key := memoryKey(tx.tenant, d.Kind, d.ID)
	old, exists := tx.docs[key]
	if expected == 0 && exists || expected > 0 && (!exists || old.Version != expected) {
		return model.ErrAnalysisConflict
	}
	tx.docs[key] = cloneStorage(d)
	return nil
}
func (tx *memoryTx) List(kind string, f model.AnalysisFilter) ([]StorageDocument, int, error) {
	out := []StorageDocument{}
	for _, d := range tx.docs {
		if d.TenantID != tx.tenant || d.Kind != kind || f.Kind != "" && d.ApplicationKind != f.Kind || f.RunID != "" && d.RunID != f.RunID || f.ResourceID != "" && d.ResourceID != f.ResourceID || f.DeviceID != "" && d.DeviceID != f.DeviceID || len(f.Statuses) > 0 && !slices.Contains(f.Statuses, d.Status) {
			continue
		}
		if f.DeviceScopeSet {
			allowed := true
			for _, id := range d.DeviceIDs {
				if !slices.Contains(f.DeviceIDs, id) {
					allowed = false
					break
				}
			}
			if d.DeviceID != "" && !slices.Contains(f.DeviceIDs, d.DeviceID) {
				allowed = false
			}
			if len(d.DeviceIDs) == 0 || !allowed {
				continue
			}
		}
		out = append(out, cloneStorage(d))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	n := len(out)
	limit, offset := NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= n {
		return []StorageDocument{}, n, nil
	}
	return out[offset:min(offset+limit, n)], n, nil
}
