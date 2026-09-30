package analytics

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.AnalysisConfigBatchStore = (*Store)(nil)

func (s *Store) PutAnalysisConfigs(ctx context.Context, revisions []model.AnalysisConfigRevision, expected []int64) ([]model.AnalysisConfigRevision, error) {
	if len(revisions) == 0 || len(revisions) > 100 || len(revisions) != len(expected) {
		return nil, model.ErrAnalysisInvalid
	}
	tenant := revisions[0].TenantID
	ids, resources := map[string]bool{}, map[string]bool{}
	for i, revision := range revisions {
		owner := ""
		if revision.Scope == "PERSONAL" {
			owner = revision.Creator
		}
		resource, _ := AnalysisHash([]string{revision.Kind, revision.ResourceID, revision.Scope, owner})
		if tenant == "" || revision.TenantID != tenant || expected[i] < 0 || ids[revision.ID] || resources[resource] {
			return nil, model.ErrAnalysisInvalid
		}
		ids[revision.ID], resources[resource] = true, true
	}
	var out []model.AnalysisConfigRevision
	err := s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		out = make([]model.AnalysisConfigRevision, len(revisions))
		for i, revision := range revisions {
			var err error
			out[i], err = s.putAnalysisConfig(tx, revision, expected[i])
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
