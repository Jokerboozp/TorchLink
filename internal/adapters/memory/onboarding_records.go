package memory

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
)

func (r *Repository) GetOnboardingRecord(ctx context.Context, tenant, id string) (model.OnboardingRecord, error) {
	if err := ctx.Err(); err != nil {
		return model.OnboardingRecord{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.onboardingRecords[key(tenant, id)]
	if !ok || tenant == "" {
		return model.OnboardingRecord{}, model.ErrNotFound
	}
	return clone(v), nil
}

func (r *Repository) SaveOnboardingRecord(ctx context.Context, v model.OnboardingRecord, expected int64) (model.OnboardingRecord, error) {
	if err := ctx.Err(); err != nil {
		return v, err
	}
	if v.TenantID == "" || v.ID == "" || v.OwnerID == "" || v.Kind == "" || expected < 0 || !json.Valid(v.Body) {
		return v, errors.New("invalid onboarding record")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	old, exists := r.onboardingRecords[k]
	if exists != (expected > 0) || exists && (old.Revision != expected || old.OwnerID != v.OwnerID || old.Kind != v.Kind) {
		return v, model.ErrOnboardingChanged
	}
	v.Revision, v.UpdatedAt = expected+1, time.Now().UnixMilli()
	if exists {
		v.CreatedAt = old.CreatedAt
	} else {
		v.CreatedAt = v.UpdatedAt
	}
	if r.onboardingRecords == nil {
		r.onboardingRecords = map[string]model.OnboardingRecord{}
	}
	r.onboardingRecords[k] = clone(v)
	return clone(v), nil
}

func (r *Repository) ListOnboardingRecords(ctx context.Context, tenant, owner, kind string, limit, offset int) ([]model.OnboardingRecord, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if tenant == "" {
		return nil, 0, errors.New("onboarding tenant is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.OnboardingRecord{}
	for _, v := range r.onboardingRecords {
		if v.TenantID == tenant && (owner == "" || owner == v.OwnerID) && (kind == "" || kind == v.Kind) {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if strings.HasPrefix(kind, "device-batch-row:") {
			return out[i].ID < out[j].ID
		}
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return page(out, offset, limit), len(out), nil
}

func (r *Repository) ListPendingOnboardingRecords(ctx context.Context, kind string, limit int) ([]model.OnboardingRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.OnboardingRecord{}
	for _, v := range r.onboardingRecords {
		if v.Kind == kind && (v.Status == "INITIALIZING" || v.Status == "QUEUED" || v.Status == "RUNNING") {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt < out[j].UpdatedAt
	})
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page(out, 0, limit), nil
}
