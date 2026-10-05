package memory

import (
	"cmp"
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

func (r *Repository) AIAnalysisOutcomes(_ context.Context, f ports.AlarmFilter, promptVersion string) ([]model.AIAnalysisOutcome, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	counts := map[model.AIAnalysisOutcome]int{}
	for _, a := range r.alarms {
		d := a.Disposition
		if !matchesAlarmFilter(a, f) || d == nil || promptVersion != "" && d.AIPromptVersion != promptVersion {
			continue
		}
		counts[model.AIAnalysisOutcome{RiskLevel: d.AIRiskLevel, Result: d.Result, PromptVersion: d.AIPromptVersion}]++
	}
	out := make([]model.AIAnalysisOutcome, 0, len(counts))
	for key, n := range counts {
		key.Count = n
		out = append(out, key)
	}
	slices.SortFunc(out, func(a, b model.AIAnalysisOutcome) int {
		return cmp.Or(cmp.Compare(a.RiskLevel, b.RiskLevel), cmp.Compare(a.Result, b.Result), cmp.Compare(a.PromptVersion, b.PromptVersion))
	})
	return out, nil
}
