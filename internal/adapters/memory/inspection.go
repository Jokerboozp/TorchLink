package memory

import (
	"context"
	"iot-platform/internal/model"
)

func (r *Repository) LatestHealthInspectionSummary(ctx context.Context, tenant, status string) (model.HealthInspectionJob, error) {
	v, err := r.LatestHealthInspectionJob(ctx, tenant, status)
	v.Report.ReportID, v.Report.TotalItems, v.Report.Items = v.ID, len(v.Report.Items), nil
	return v, err
}
func (r *Repository) HealthInspectionPage(_ context.Context, tenant, id string, limit, offset int) (model.HealthInspectionJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.inspectionJobs[tenant] {
		if v.ID != id {
			continue
		}
		v.Report.ReportID, v.Report.TotalItems = v.ID, len(v.Report.Items)
		offset = max(0, min(offset, len(v.Report.Items)))
		limit = max(1, min(limit, 2000))
		v.Report.Items = append([]model.DeviceHealthItem{}, v.Report.Items[offset:min(offset+limit, len(v.Report.Items))]...)
		return v, nil
	}
	return model.HealthInspectionJob{}, ErrNotFound
}
