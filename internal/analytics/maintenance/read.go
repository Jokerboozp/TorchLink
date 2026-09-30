package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"slices"
	"strings"
)

func (s *Service) Review(ctx context.Context, a analytics.Actor, kind, id string, q model.MaintenanceReviewRequest) (model.AnalysisReview, error) {
	run, err := s.Analysis.Get(ctx, a, kind, id)
	if err != nil {
		return model.AnalysisReview{}, err
	}
	if _, err = s.authorize(ctx, a, "POST "+analytics.RunCollection(kind)+"/:id/reviews", run.DeviceIDs); err != nil {
		return model.AnalysisReview{}, err
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, kind, id)
	if err != nil {
		return model.AnalysisReview{}, err
	}
	if snapshot.FactsHash != q.FactsHash || snapshot.Version != q.SnapshotVersion {
		return model.AnalysisReview{}, model.ErrAnalysisConflict
	}
	if strings.TrimSpace(q.Explanation) == "" || !slices.Contains([]string{"CONFIRMED_PROBLEM", "NORMAL_EXPLANATION", "RESOLVED", "OBSERVE"}, q.Result) || q.IdempotencyKey == "" {
		return model.AnalysisReview{}, invalid("核实须绑定固定快照、事实摘要和明确结论")
	}
	return s.Analysis.Store.AppendAnalysisReview(ctx, model.AnalysisReview{ID: uuid.NewString(), TenantID: a.TenantID, RunID: id, ResourceID: snapshot.ID, ResourceVersion: snapshot.Version, Result: q.Result, Explanation: q.Explanation, Reviewer: a.Username, IdempotencyKey: q.IdempotencyKey}, q.ExpectedVersion)
}
func (s *Service) Reviews(ctx context.Context, a analytics.Actor, kind, id string, f model.AnalysisFilter) ([]model.AnalysisReview, int, error) {
	if _, err := s.Analysis.Get(ctx, a, kind, id); err != nil {
		return nil, 0, err
	}
	f.RunID = id
	return s.Analysis.Store.ListAnalysisReviews(ctx, a.TenantID, f)
}
func (s *Service) Export(ctx context.Context, a analytics.Actor, kind, id string) (json.RawMessage, error) {
	run, err := s.Analysis.Get(ctx, a, kind, id)
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, a, "GET "+analytics.RunCollection(kind)+"/:id/export", run.DeviceIDs); err != nil {
		return nil, err
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, kind, id)
	if err != nil {
		return nil, err
	}
	outputs := []model.AnalysisOutput{}
	evidence := []model.AnalysisEvidence{}
	reviews := []model.AnalysisReview{}
	allowed := []string{"observations", "change-metrics", "findings"}
	if kind == analytics.KindInvestment {
		allowed = []string{"investment-priorities", "budget-lines", "findings"}
	}
	for _, collection := range allowed {
		for offset := 0; ; {
			page, total, err := s.Analysis.Outputs(ctx, a, kind, id, model.AnalysisFilter{Kind: collection, Limit: 100, Offset: offset})
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, page...)
			offset += len(page)
			if offset >= total {
				break
			}
			if offset > 200000 || len(page) == 0 {
				return nil, invalid("报告明细超过读取保护范围")
			}
		}
	}
	for offset := 0; ; {
		page, total, err := s.Analysis.Evidence(ctx, a, kind, id, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, page...)
		offset += len(page)
		if offset >= total {
			break
		}
		if offset > 200000 || len(page) == 0 {
			return nil, invalid("报告证据超过读取保护范围")
		}
	}
	for offset := 0; ; {
		page, total, err := s.Reviews(ctx, a, kind, id, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, page...)
		offset += len(page)
		if offset >= total {
			break
		}
		if offset > 10000 || len(page) == 0 {
			return nil, invalid("人工核实记录超过保护范围")
		}
	}
	ai := []model.AnalysisAIRevision{}
	if s.AI != nil {
		for offset := 0; ; {
			page, total, err := s.AI.List(ctx, a, kind, id, model.AnalysisFilter{Limit: 100, Offset: offset})
			if err != nil {
				return nil, err
			}
			for _, job := range page {
				job.LeaseOwner = ""
				job.LeaseToken = 0
				job.LeaseExpiresAt = 0
				job.CreatorSessionVersion = 0
				job.PermissionVersion = ""
				ai = append(ai, job)
			}
			offset += len(page)
			if offset >= total {
				break
			}
			if offset > 10000 || len(page) == 0 {
				return nil, invalid("解读版本超过保护范围")
			}
		}
	}
	// Session and worker fencing details are not part of the fixed report.
	run.LeaseOwner = ""
	run.LeaseToken = 0
	run.LeaseExpiresAt = 0
	run.CreatorSessionVersion = 0
	run.PermissionsVersion = ""
	run.Checkpoint = nil
	return json.Marshal(map[string]any{"run": run, "snapshot": snapshot, "outputs": outputs, "evidence": evidence, "reviews": reviews, "aiRevisions": ai, "policy": "固定事实；人工核实不改写指标；完成工作不等于功能验收；决定不等于采购完成"})
}
func (s *Service) reauthorizeInputs(ctx context.Context, r model.AnalysisRun, m frozenManifest) error {
	a := runActor(r)
	if p := m.Business.Parameters.Observation; p != nil {
		for _, id := range p.QualityRunIDs {
			if _, err := s.Analysis.Get(ctx, a, analytics.KindDataQuality, id); err != nil {
				return err
			}
		}
	}
	for _, v := range m.Business.Configs {
		if _, err := s.authorize(ctx, a, "", v.DeviceIDs); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) RelatedRuns(ctx context.Context, a analytics.Actor, kind, resource string, f model.AnalysisFilter) ([]model.AnalysisRun, int, error) {
	objectKind := InterventionKind
	if kind == analytics.KindInvestment {
		objectKind = ScenarioKind
	}
	if _, err := s.Latest(ctx, a, objectKind, resource); err != nil {
		return nil, 0, err
	}
	matches := []model.AnalysisRun{}
	for offset := 0; ; {
		page, total, err := s.Analysis.List(ctx, a, kind, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		for _, r := range page {
			var p model.MaintenanceRunParameters
			json.Unmarshal(r.Parameters, &p)
			id := p.ScenarioRevisionID
			if p.Observation != nil {
				id = p.Observation.InterventionRevisionID
			}
			v, err := s.Revision(ctx, a, objectKind, id)
			if err != nil {
				continue
			}
			if v.ResourceID == resource {
				matches = append(matches, r)
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset > 10000 || len(page) == 0 {
			return nil, 0, fmt.Errorf("%w: 请缩小任务范围", model.ErrAnalysisInvalid)
		}
	}
	total := len(matches)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AnalysisRun{}, total, nil
	}
	return matches[offset:min(total, offset+limit)], total, nil
}
