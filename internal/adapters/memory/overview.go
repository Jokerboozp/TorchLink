package memory

import (
	"context"
	"slices"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) DeviceOverviewCounts(_ context.Context, tenant string, restrict bool, ids []string) (model.DeviceOverview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := model.NewDeviceOverview()
	allowed := func(id string) bool { return !restrict || slices.Contains(ids, id) }
	registered := map[string]bool{}
	for _, d := range r.devices {
		if d.TenantID != tenant || !allowed(d.ID) {
			continue
		}
		registered[d.ID] = true
		out.Total++
		out.ByStatus[model.OverviewKey(d.Status)]++
		out.ByRole[model.OverviewKey(d.DeviceRole)]++
		if d.AutoRegistered {
			out.AutoRegistered++
		}
	}
	for _, s := range r.states {
		if s.TenantID != tenant || !allowed(s.DeviceID) {
			continue
		}
		if !registered[s.DeviceID] {
			out.DiscoveredUnregistered++
			continue
		}
		out.Reported++
		out.ConnectionStatus[model.OverviewKey(s.ConnectionStatus)]++
		out.DataStatus[model.OverviewKey(s.DataStatus)]++
		out.BusinessStatus[model.OverviewKey(s.BusinessStatus)]++
		out.LatestSeenAt = max(out.LatestSeenAt, s.LastSeenAt)
	}
	return out, nil
}

func (r *Repository) AlarmOverviewCounts(_ context.Context, f ports.AlarmFilter, since int64) (model.AlarmOverview, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := model.NewAlarmOverview()
	for _, a := range r.alarms {
		if matchesAlarmFilter(a, f) {
			out.AddAlarm(a, since)
		}
	}
	return out, nil
}
