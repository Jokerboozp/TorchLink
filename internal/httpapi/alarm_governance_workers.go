package httpapi

import (
	"context"
	"iot-platform/internal/ports"
	"time"
)

func (s *Server) RunGovernanceMaintenance(ctx context.Context) {
	if s.governance == nil {
		return
	}
	lister, ok := s.governance.Store.(ports.GovernanceTenantLister)
	if !ok {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tenants, err := lister.GovernanceTenants(ctx)
			if err != nil {
				s.metrics.Inc("alarm_governance_maintenance_failed_total")
				continue
			}
			for _, tenant := range tenants {
				maintenance, cancel := context.WithTimeout(ctx, 30*time.Second)
				n, err := s.governance.CleanupAttachmentUploads(maintenance, tenant, s.engine.Archive)
				cancel()
				if err != nil {
					s.metrics.Inc("alarm_governance_maintenance_failed_total")
				} else {
					s.metrics.Add("alarm_governance_orphan_objects_cleaned_total", uint64(n))
				}
			}
		}
	}
}
