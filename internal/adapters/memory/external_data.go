package memory

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/externaldata"
)

type externalDataStore struct{ repo *Repository }

var _ externaldata.Store = (*externalDataStore)(nil)

func (r *Repository) ExternalDataStore() externaldata.Store { return &externalDataStore{repo: r} }

func validExternalKey(tenant, kind, id string) bool {
	return strings.TrimSpace(tenant) != "" && strings.TrimSpace(kind) != "" && strings.TrimSpace(id) != ""
}

func copyExternalEntry(e externaldata.Entry) externaldata.Entry {
	e.Body = append(json.RawMessage(nil), e.Body...)
	return e
}

func (s *externalDataStore) Get(ctx context.Context, tenant, kind, id string) (externaldata.Entry, error) {
	if err := ctx.Err(); err != nil {
		return externaldata.Entry{}, err
	}
	if !validExternalKey(tenant, kind, id) {
		return externaldata.Entry{}, externaldata.ErrInvalid
	}
	s.repo.mu.RLock()
	defer s.repo.mu.RUnlock()
	e, ok := s.repo.externalData[key(tenant, kind, id)]
	if !ok {
		return externaldata.Entry{}, externaldata.ErrNotFound
	}
	return copyExternalEntry(e), nil
}

func (s *externalDataStore) List(ctx context.Context, q externaldata.Query) ([]externaldata.Entry, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(q.TenantID) == "" {
		return nil, 0, externaldata.ErrInvalid
	}
	s.repo.mu.RLock()
	defer s.repo.mu.RUnlock()
	items := []externaldata.Entry{}
	for _, e := range s.repo.externalData {
		if e.TenantID != q.TenantID || (q.Kind != "" && e.Kind != q.Kind) || (q.SourceID != "" && e.SourceID != q.SourceID) || (q.EndpointID != "" && e.EndpointID != q.EndpointID) || (q.Status != "" && !slices.Contains(strings.Split(q.Status, ","), e.Status)) {
			continue
		}
		if q.JobID != "" {
			var v struct {
				JobID string `json:"jobId"`
			}
			if json.Unmarshal(e.Body, &v) != nil || v.JobID != q.JobID {
				continue
			}
		}
		items = append(items, copyExternalEntry(e))
	}
	sort.Slice(items, func(i, j int) bool {
		if q.UpdatedOrder && items[i].UpdatedAt != items[j].UpdatedAt {
			return items[i].UpdatedAt > items[j].UpdatedAt
		}
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].ID < items[j].ID
	})
	total := len(items)
	limit, offset := q.Limit, q.Offset
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	return items[offset:min(total, offset+limit)], total, nil
}

func (s *externalDataStore) Put(ctx context.Context, e externaldata.Entry, expectedRevision int64) (externaldata.Entry, error) {
	if err := ctx.Err(); err != nil {
		return externaldata.Entry{}, err
	}
	if !validExternalKey(e.TenantID, e.Kind, e.ID) || expectedRevision < 0 || !json.Valid(e.Body) {
		return externaldata.Entry{}, externaldata.ErrInvalid
	}
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	now := time.Now().UnixMilli()
	k := key(e.TenantID, e.Kind, e.ID)
	old, exists := s.repo.externalData[k]
	if (expectedRevision == 0 && exists) || (expectedRevision > 0 && (!exists || old.Revision != expectedRevision || (old.Status == "RUNNING" && old.LeaseUntil <= now))) {
		return externaldata.Entry{}, externaldata.ErrConflict
	}
	e.CreatedAt = old.CreatedAt
	if !exists {
		e.CreatedAt = now
	}
	e.UpdatedAt, e.Revision = now, expectedRevision+1
	if s.repo.externalData == nil {
		s.repo.externalData = map[string]externaldata.Entry{}
	}
	s.repo.externalData[k] = copyExternalEntry(e)
	return copyExternalEntry(e), nil
}

func (s *externalDataStore) Delete(ctx context.Context, tenant, kind, id string, expectedRevision int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validExternalKey(tenant, kind, id) || expectedRevision <= 0 {
		return externaldata.ErrInvalid
	}
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	k := key(tenant, kind, id)
	e, exists := s.repo.externalData[k]
	if !exists {
		return externaldata.ErrNotFound
	}
	if e.Revision != expectedRevision {
		return externaldata.ErrConflict
	}
	delete(s.repo.externalData, k)
	return nil
}

func (s *externalDataStore) Claim(ctx context.Context, kind, owner string, now, leaseMillis int64) (externaldata.Entry, error) {
	if err := ctx.Err(); err != nil {
		return externaldata.Entry{}, err
	}
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(owner) == "" || leaseMillis <= 0 || now > (1<<63-1)-leaseMillis {
		return externaldata.Entry{}, externaldata.ErrInvalid
	}
	s.repo.mu.Lock()
	defer s.repo.mu.Unlock()
	var selected externaldata.Entry
	for _, e := range s.repo.externalData {
		if e.Kind != kind || e.DueAt > now || !(e.Status == "PENDING" || e.Status == "RETRY" || (e.Status == "RUNNING" && e.LeaseUntil <= now)) {
			continue
		}
		if selected.ID == "" || e.DueAt < selected.DueAt || (e.DueAt == selected.DueAt && (e.CreatedAt < selected.CreatedAt || (e.CreatedAt == selected.CreatedAt && key(e.TenantID, e.ID) < key(selected.TenantID, selected.ID)))) {
			selected = e
		}
	}
	if selected.ID == "" {
		return selected, externaldata.ErrNotFound
	}
	selected.Status, selected.Owner, selected.LeaseUntil, selected.UpdatedAt = "RUNNING", owner, now+leaseMillis, now
	selected.Revision++
	s.repo.externalData[key(selected.TenantID, selected.Kind, selected.ID)] = copyExternalEntry(selected)
	return copyExternalEntry(selected), nil
}
