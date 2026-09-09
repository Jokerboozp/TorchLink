package memory

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) CreateEdgeReadJob(_ context.Context, j model.EdgeReadJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.edgeReadJobs == nil {
		r.edgeReadJobs = map[string]model.EdgeReadJob{}
	}
	count := 0
	for k, job := range r.edgeReadJobs {
		if job.ExpiresAt < time.Now().Add(-time.Hour).UnixMilli() {
			delete(r.edgeReadJobs, k)
		}
		if job.TenantID == j.TenantID && job.NodeID == j.NodeID && job.ExpiresAt > time.Now().UnixMilli() && (job.Status == "PENDING" || job.Status == "RUNNING") {
			count++
		}
	}
	if count >= 8 {
		return errors.New("edge diagnostic queue full")
	}
	k := key(j.TenantID, j.ID)
	if _, ok := r.edgeReadJobs[k]; ok {
		return errors.New("edge diagnostic already exists")
	}
	j.Status, j.Token, j.Raw, j.Error = "PENDING", "", nil, ""
	r.edgeReadJobs[k] = clone(j)
	return nil
}
func (r *Repository) GetEdgeReadJob(_ context.Context, tenant, id string) (model.EdgeReadJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.edgeReadJobs[key(tenant, id)]
	if !ok {
		return j, ErrNotFound
	}
	return clone(j), nil
}
func (r *Repository) ClaimEdgeReadJob(_ context.Context, tenant, node, token string) (model.EdgeReadJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, j := range r.edgeReadJobs {
		if j.TenantID == tenant && j.NodeID == node && j.Status == "PENDING" && j.ExpiresAt > time.Now().UnixMilli() {
			j.Status, j.Token = "RUNNING", token
			r.edgeReadJobs[k] = clone(j)
			return clone(j), nil
		}
	}
	return model.EdgeReadJob{}, nil
}
func (r *Repository) FinishEdgeReadJob(_ context.Context, j model.EdgeReadJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(j.TenantID, j.ID)
	old, ok := r.edgeReadJobs[k]
	if !ok || old.NodeID != j.NodeID || old.Status != "RUNNING" || old.Token == "" || old.Token != j.Token || old.ExpiresAt <= time.Now().UnixMilli() {
		return errors.New("expired or unowned edge diagnostic")
	}
	old.Raw, old.Error, old.Status = j.Raw, j.Error, "DONE"
	r.edgeReadJobs[k] = clone(old)
	return nil
}
