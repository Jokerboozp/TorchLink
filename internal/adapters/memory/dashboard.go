package memory

import (
	"context"
	"iot-platform/internal/model"
	"strconv"
)

func (r *Repository) DashboardCounts(_ context.Context, tenant string, start, end int64) ([]model.DashboardCount, error) {
	return r.dashboardCounts(tenant, start, end, nil)
}

func (r *Repository) DashboardCountsForDevices(_ context.Context, tenant string, start, end int64, ids []string) ([]model.DashboardCount, error) {
	return r.dashboardCounts(tenant, start, end, idSet(ids))
}

func (r *Repository) dashboardCounts(tenant string, start, end int64, allowed map[string]bool) ([]model.DashboardCount, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	counts := map[string]model.DashboardCount{}
	add := func(kind, id, name string) {
		k := kind + "\x00" + id
		v := counts[k]
		v.Kind = kind
		v.Key = id
		v.Name = name
		v.Count++
		counts[k] = v
	}
	for _, d := range r.devices {
		if d.TenantID != tenant || allowed != nil && !allowed[d.ID] {
			continue
		}
		state := r.states[key(tenant, d.ID)]
		status := state.BusinessStatus
		if status == "ALARM" {
			status = "ONLINE"
		}
		if status == "" {
			status = "NEVER_SEEN"
		}
		add("state", status, "")
		p := r.products[key(tenant, d.ProductID)]
		name := p.Name
		if name == "" {
			name = d.ProductID
		}
		add("product", d.ProductID, name)
	}
	for _, a := range r.alarms {
		if a.TenantID != tenant || allowed != nil && !allowed[a.DeviceID] {
			continue
		}
		if a.Status == "ACTIVE" {
			add("level", a.AlarmLevel, "")
		}
		if a.FirstTriggeredAt >= start && a.FirstTriggeredAt <= end {
			add("day", strconv.FormatInt((a.FirstTriggeredAt-start)/86400000, 10), "")
		}
	}
	out := make([]model.DashboardCount, 0, len(counts))
	for _, v := range counts {
		out = append(out, v)
	}
	return out, nil
}
