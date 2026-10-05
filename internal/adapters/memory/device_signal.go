package memory

import (
	"context"
	"math"
	"slices"
	"sort"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) DevicePropertyStats(ctx context.Context, tenant string, start, end int64, ranges []model.PropertyRange) ([]model.DevicePropertyStat, error) {
	stats, _ := r.telemetryStats(tenant, start, end, ranges)
	return stats, nil
}

func (r *Repository) DeviceReportStats(ctx context.Context, tenant string, start, end int64) ([]model.DeviceReportStat, error) {
	_, reports := r.telemetryStats(tenant, start, end, nil)
	return reports, nil
}

func (r *Repository) telemetryStats(tenant string, start, end int64, ranges []model.PropertyRange) ([]model.DevicePropertyStat, []model.DeviceReportStat) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	type key struct{ device, property string }
	values := map[key][]float64{}
	products := map[string]string{}
	reports := map[string]*model.DeviceReportStat{}
	for _, m := range r.standard {
		if m.TenantID != tenant || m.Timestamp < start || m.Timestamp > end {
			continue
		}
		products[m.DeviceID] = m.ProductID
		rep := reports[m.DeviceID]
		if rep == nil {
			rep = &model.DeviceReportStat{DeviceID: m.DeviceID, ProductID: m.ProductID, FirstAt: m.Timestamp, LastAt: m.Timestamp}
			reports[m.DeviceID] = rep
		}
		rep.Count++
		rep.FirstAt, rep.LastAt = min(rep.FirstAt, m.Timestamp), max(rep.LastAt, m.Timestamp)
		for name, value := range m.Properties {
			if number, ok := value.(float64); ok {
				values[key{m.DeviceID, name}] = append(values[key{m.DeviceID, name}], number)
			}
		}
	}
	stats := []model.DevicePropertyStat{}
	for k, list := range values {
		s := model.DevicePropertyStat{DeviceID: k.device, ProductID: products[k.device], Property: k.property, Count: int64(len(list)), Min: slices.Min(list), Max: slices.Max(list)}
		sum := 0.0
		for _, v := range list {
			sum += v
		}
		s.Mean = sum / float64(len(list))
		variance := 0.0
		for _, v := range list {
			variance += (v - s.Mean) * (v - s.Mean)
		}
		s.StdDev = math.Sqrt(variance / float64(len(list)))
		for _, rg := range ranges {
			if rg.ProductID != s.ProductID || rg.Property != s.Property {
				continue
			}
			for _, v := range list {
				if rg.Min != nil && v < *rg.Min || rg.Max != nil && v > *rg.Max {
					s.OutOfRange++
				}
			}
		}
		stats = append(stats, s)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].DeviceID < stats[j].DeviceID || stats[i].DeviceID == stats[j].DeviceID && stats[i].Property < stats[j].Property
	})
	out := make([]model.DeviceReportStat, 0, len(reports))
	for _, rep := range reports {
		out = append(out, *rep)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return stats, out
}

func (r *Repository) ReplaceDeviceSignals(_ context.Context, tenant string, signals []model.DeviceSignal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deviceSignals == nil {
		r.deviceSignals = map[string][]model.DeviceSignal{}
	}
	r.deviceSignals[tenant] = slices.Clone(signals)
	return nil
}

func (r *Repository) ListDeviceSignals(_ context.Context, tenant string, deviceIDs []string, limit int) ([]model.DeviceSignal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	out := []model.DeviceSignal{}
	for _, s := range r.deviceSignals[tenant] {
		if deviceIDs == nil || slices.Contains(deviceIDs, s.DeviceID) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Strength > out[j].Strength })
	return out[:min(limit, len(out))], nil
}

func (r *Repository) SignalTenants(_ context.Context) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]bool{}
	for _, s := range r.states {
		seen[s.TenantID] = true
	}
	out := make([]string, 0, len(seen))
	for tenant := range seen {
		out = append(out, tenant)
	}
	sort.Strings(out)
	return out, nil
}

var (
	_ ports.DeviceSignalStore    = (*Repository)(nil)
	_ ports.DeviceTelemetryStats = (*Repository)(nil)
)
