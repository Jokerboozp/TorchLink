package memory

import (
	"context"
	"sort"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) SaveAIRun(_ context.Context, v model.AIRunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.aiRuns {
		if existing.RunID == v.RunID {
			return nil
		}
	}
	r.aiRuns = append(r.aiRuns, v)
	return nil
}

func (r *Repository) filteredAIRuns(f ports.AIRunFilter) []model.AIRunRecord {
	out := []model.AIRunRecord{}
	for _, v := range r.aiRuns {
		if v.TenantID == f.TenantID && (f.WorkflowID == "" || v.WorkflowID == f.WorkflowID) && (f.Status == "" || v.Status == f.Status) &&
			(f.Start <= 0 || v.StartedAt >= f.Start) && (f.End <= 0 || v.StartedAt <= f.End) {
			out = append(out, v)
		}
	}
	return out
}

func (r *Repository) ListAIRuns(_ context.Context, f ports.AIRunFilter) ([]model.AIRunRecord, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := r.filteredAIRuns(f)
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt > out[j].StartedAt
		}
		return out[i].RunID > out[j].RunID
	})
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return page(out, max(f.Offset, 0), limit), len(out), nil
}

func (r *Repository) AIRunUsage(_ context.Context, f ports.AIRunFilter) ([]model.AIRunUsage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byKey := map[[2]string]*model.AIRunUsage{}
	for _, v := range r.filteredAIRuns(f) {
		k := [2]string{time.UnixMilli(v.StartedAt).In(model.ReportZone).Format(time.DateOnly), v.WorkflowID}
		u := byKey[k]
		if u == nil {
			u = &model.AIRunUsage{Day: k[0], WorkflowID: k[1]}
			byKey[k] = u
		}
		u.Runs++
		if v.Status != model.AIRunSucceeded {
			u.Failed++
		}
		u.Usage.Add(v.Usage)
		u.ToolCalls += v.ToolCalls
		u.DurationMs += v.DurationMs
	}
	out := make([]model.AIRunUsage, 0, len(byKey))
	for _, u := range byKey {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		return out[i].WorkflowID < out[j].WorkflowID
	})
	return out, nil
}

var _ ports.AIRunStore = (*Repository)(nil)
