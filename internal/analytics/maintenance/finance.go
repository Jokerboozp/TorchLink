package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"slices"
	"strings"
)

func (s *Service) SaveCost(ctx context.Context, a analytics.Actor, q model.MaintenanceRevisionRequest) (model.AnalysisConfigRevision, error) {
	if v, found, err := s.replay(ctx, a, CostKind, q.ResourceID, q.IdempotencyKey, q); found || err != nil {
		return v, err
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/maintenance-costs", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err := s.finance(ctx, a, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.MaintenanceCost
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if len(b.Requests) > 0 {
		return model.AnalysisConfigRevision{}, invalid("操作收据由服务端保存")
	}
	if err := ValidateCost(b); err != nil {
		return model.AnalysisConfigRevision{}, invalid(err.Error())
	}
	if !slices.Contains([]string{AssetKind, InterventionKind}, b.SourceKind) {
		return model.AnalysisConfigRevision{}, invalid("费用须绑定实物或维修记录")
	}
	source, err := s.Latest(ctx, a, b.SourceKind, b.SourceID)
	if err != nil {
		return source, err
	}
	if !sameDevices(source.DeviceIDs, q.DeviceIDs) {
		return source, analytics.ErrForbidden
	}
	b.Evidence, err = s.canonicalEvidence(ctx, a, b.Evidence, q.DeviceIDs)
	if err != nil {
		return source, err
	}
	old, err := s.Latest(ctx, a, CostKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.MaintenanceCost
		if decode(old.Body, &prior) != nil {
			return old, model.ErrAnalysisConflict
		}
		if prior.SourceID != b.SourceID || prior.SourceKind != b.SourceKind || !sameDevices(old.DeviceIDs, q.DeviceIDs) {
			return old, invalid("费用版本不可隐式改绑来源")
		}
		b.Requests = prior.Requests
	}
	return s.commit(ctx, a, CostKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}

// bindCandidates accepts only recorded factors. Caller-provided numeric metrics
// are rejected; immutable observation snapshots provide the comparable values.
func (s *Service) bindCandidates(ctx context.Context, a analytics.Actor, b *model.InvestmentScenario, devices []string, acceptDerived bool) ([]model.AnalysisConfigRevision, []model.AnalysisSnapshot, error) {
	refs := []model.AnalysisConfigRevision{}
	snapshots := []model.AnalysisSnapshot{}
	seen := map[string]bool{}
	b.RequiredPermissions = nil
	if b.UseFinance {
		b.RequiredPermissions = append(b.RequiredPermissions, FinancePermission)
	}
	for i := range b.Candidates {
		c := &b.Candidates[i]
		if c.ID == "" || seen[c.ID] || !slices.Contains([]string{"OBSERVE", "INSPECT", "REPAIR", "CALIBRATE", "REPLACE"}, c.Action) || len(c.Constraints) > 100 || len(c.DefectBasisIDs) > 100 {
			return nil, nil, invalid("候选动作、身份或约束无效")
		}
		seen[c.ID] = true
		if !acceptDerived && (c.AssetID != "" || c.Importance != 0 || c.Comparable || c.MetricBasis != "" || c.ConfirmedFaultRate != nil || c.KnownOfflineMs != nil || c.UnresolvedVerifiedDefect) {
			return nil, nil, invalid("实物和指标因素由受权固定记录提供")
		}
		v, err := s.Revision(ctx, a, AssetKind, c.AssetRevisionID)
		if err != nil {
			return nil, nil, err
		}
		if !subset(v.DeviceIDs, devices) {
			return nil, nil, analytics.ErrForbidden
		}
		refs = append(refs, v)
		var asset model.AssetInstance
		if decode(v.Body, &asset) != nil {
			return nil, nil, model.ErrAnalysisConflict
		}
		c.AssetID = v.ResourceID
		c.Importance = asset.Importance
		c.Comparable = false
		c.MetricBasis = ""
		c.ConfirmedFaultRate = nil
		c.KnownOfflineMs = nil
		c.UnresolvedVerifiedDefect = false
		if c.ObservationRunID != "" {
			run, err := s.Analysis.Get(ctx, a, analytics.KindMaintenance, c.ObservationRunID)
			if err != nil {
				return nil, nil, err
			}
			if !subset(run.DeviceIDs, devices) || !subset(v.DeviceIDs, run.DeviceIDs) || !b.UseFinance && slices.Contains(run.RequiredPermissions, FinancePermission) {
				return nil, nil, analytics.ErrForbidden
			}
			snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindMaintenance, run.ID)
			if err != nil {
				return nil, nil, err
			}
			var observation model.MaintenanceObservation
			if json.Unmarshal(snapshot.Statistics, &observation) != nil {
				return nil, nil, invalid("观察快照不含固定指标")
			}
			after, err := s.Revision(ctx, a, AssetKind, observation.Parameters.AfterAssetRevisionID)
			if err != nil || after.ResourceID != v.ResourceID {
				return nil, nil, invalid("观察须绑定该候选实物")
			}
			var extra struct {
				Supplement struct {
					ComparisonBasis   string `json:"comparisonBasis"`
					ContextComparable bool   `json:"contextComparable"`
				} `json:"supplement"`
			}
			json.Unmarshal(snapshot.Statistics, &extra)
			if (observation.Status == "READY" || observation.Status == "REVIEWED") && extra.Supplement.ContextComparable && extra.Supplement.ComparisonBasis != "" && after.ID == v.ID && asset.BoundaryStatus == "CONFIRMED" {
				c.Comparable = true
				c.MetricBasis = extra.Supplement.ComparisonBasis
				c.ConfirmedFaultRate = observation.After.FaultsPer1000Hours
				offline := observation.After.OfflineMs
				c.KnownOfflineMs = &offline
			}
			snapshots = append(snapshots, snapshot)
			b.RequiredPermissions = append(b.RequiredPermissions, run.RequiredPermissions...)
		}
		// A failed, latest functional verification remains an unresolved defect.
		// Free text and a superseded failed check cannot set this ranking factor.
		for _, id := range c.DefectBasisIDs {
			defect, err := s.Revision(ctx, a, InterventionKind, id)
			if err != nil {
				return nil, nil, err
			}
			latest, err := s.Latest(ctx, a, InterventionKind, defect.ResourceID)
			if err != nil {
				return nil, nil, err
			}
			if latest.ID != defect.ID {
				return nil, nil, invalid("缺陷依据须为维修当前版本")
			}
			var work model.MaintenanceIntervention
			if decode(defect.Body, &work) != nil {
				return nil, nil, invalid("缺陷依据无效")
			}
			assetRef, err := s.Revision(ctx, a, AssetKind, work.AssetRevisionID)
			if err != nil || assetRef.ResourceID != v.ResourceID {
				return nil, nil, analytics.ErrForbidden
			}
			if len(work.Verifications) == 0 || work.Verifications[len(work.Verifications)-1].Result != "FAILED" {
				return nil, nil, invalid("未解除缺陷须有当前失败功能验收")
			}
			c.UnresolvedVerifiedDefect = true
			refs = append(refs, defect)
		}
		if !b.UseFinance {
			if c.QuoteRevisionID != "" {
				return nil, nil, invalid("普通方案不能引用报价")
			}
			continue
		}
		if c.QuoteRevisionID != "" {
			quote, err := s.Revision(ctx, a, CostKind, c.QuoteRevisionID)
			if err != nil {
				return nil, nil, err
			}
			if !subset(quote.DeviceIDs, devices) {
				return nil, nil, analytics.ErrForbidden
			}
			var cost model.MaintenanceCost
			if decode(quote.Body, &cost) != nil {
				return nil, nil, invalid("报价无效")
			}
			if cost.Type != "ESTIMATE" || cost.SourceKind != AssetKind || cost.SourceID != v.ResourceID || cost.Currency != b.Currency || cost.PlanningStart != b.PlanningStart || cost.PlanningEnd != b.PlanningEnd {
				return nil, nil, invalid("报价须同实物、同币种、同规划期；历史支出不作未来报价")
			}
			refs = append(refs, quote)
		}
	}
	// Rates from different fault/admission/context bases are not compared in a
	// single tier. Stable identity keeps a reproducible awaiting-information
	// group instead of silently mixing incomparable normalized numbers.
	bases := map[int]map[string]bool{}
	for _, c := range b.Candidates {
		if c.Comparable {
			if bases[c.RequiredTier] == nil {
				bases[c.RequiredTier] = map[string]bool{}
			}
			bases[c.RequiredTier][c.MetricBasis] = true
		}
	}
	for i := range b.Candidates {
		c := &b.Candidates[i]
		if len(bases[c.RequiredTier]) > 1 {
			c.Comparable = false
			if !slices.Contains(c.Constraints, "CROSS_CANDIDATE_METRIC_BASIS_DIFFERS") {
				c.Constraints = append(c.Constraints, "CROSS_CANDIDATE_METRIC_BASIS_DIFFERS")
			}
		}
	}
	slices.Sort(b.RequiredPermissions)
	b.RequiredPermissions = slices.Compact(b.RequiredPermissions)
	return refs, snapshots, nil
}
func subset(a, b []string) bool {
	for _, v := range a {
		if !slices.Contains(b, v) {
			return false
		}
	}
	return len(a) > 0
}
func sameDevices(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

func (s *Service) SaveScenario(ctx context.Context, a analytics.Actor, q model.MaintenanceRevisionRequest) (model.AnalysisConfigRevision, error) {
	if v, found, err := s.replay(ctx, a, ScenarioKind, q.ResourceID, q.IdempotencyKey, q); found || err != nil {
		return v, err
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/investment-scenarios", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err := s.validateDevices(ctx, a, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.InvestmentScenario
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if strings.TrimSpace(b.Name) == "" || b.Status != "" && b.Status != "DRAFT" || len(b.Adjustments) > 0 || len(b.Decisions) > 0 || len(b.Requests) > 0 || len(b.RequiredPermissions) > 0 {
		return model.AnalysisConfigRevision{}, invalid("方案初始状态须为草稿；人工调整和决定使用独立操作")
	}
	if !b.UseFinance && b.Budget != nil {
		return model.AnalysisConfigRevision{}, invalid("普通方案不含资金输入")
	}
	if b.UseFinance {
		if err := s.finance(ctx, a, q.DeviceIDs); err != nil {
			return model.AnalysisConfigRevision{}, err
		}
	}
	if _, _, err := s.bindCandidates(ctx, a, &b, q.DeviceIDs, false); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if _, err := RankInvestment(b, map[string]InvestmentQuote{}, b.UseFinance); err != nil {
		return model.AnalysisConfigRevision{}, invalid(err.Error())
	}
	b.Status = "DRAFT"
	old, err := s.Latest(ctx, a, ScenarioKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.InvestmentScenario
		if decode(old.Body, &prior) != nil {
			return old, model.ErrAnalysisConflict
		}
		for _, r := range prior.Requests {
			if r.Key == q.IdempotencyKey {
				return s.commit(ctx, a, ScenarioKind, q.ResourceID, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &prior)
			}
		}
		if prior.Status != "DRAFT" || prior.UseFinance != b.UseFinance || !sameDevices(old.DeviceIDs, q.DeviceIDs) {
			return old, invalid("已审核方案或资金口径不能原位降级；创建新方案")
		}
		b.Requests = prior.Requests
	}
	return s.commit(ctx, a, ScenarioKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) AdjustScenario(ctx context.Context, a analytics.Actor, id string, q model.InvestmentAdjustmentRequest) (model.AnalysisConfigRevision, error) {
	v, err := s.Latest(ctx, a, ScenarioKind, id)
	if err != nil {
		return v, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/investment-scenarios/:id/adjustments", v.DeviceIDs); err != nil {
		return v, err
	}
	var b model.InvestmentScenario
	if err = decode(v.Body, &b); err != nil {
		return v, err
	}
	for _, r := range b.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ScenarioKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
		}
	}
	if v.Version != q.ExpectedVersion || b.Status == "DECIDED" || b.Status == "ARCHIVED" || strings.TrimSpace(q.Reason) == "" {
		return v, model.ErrAnalysisConflict
	}
	b.Adjustments = append(b.Adjustments, model.InvestmentAdjustment{CandidateOrder: q.CandidateOrder, Reason: q.Reason, Actor: a.Username, RecordedAt: s.now()})
	if _, err = RankInvestment(b, map[string]InvestmentQuote{}, b.UseFinance); err != nil {
		return v, invalid(err.Error())
	}
	b.Status = "REVIEWED"
	return s.commit(ctx, a, ScenarioKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) DecideScenario(ctx context.Context, a analytics.Actor, id string, q model.InvestmentDecisionRequest) (model.AnalysisConfigRevision, error) {
	v, err := s.Latest(ctx, a, ScenarioKind, id)
	if err != nil {
		return v, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/investment-scenarios/:id/decisions", v.DeviceIDs); err != nil {
		return v, err
	}
	var b model.InvestmentScenario
	if err = decode(v.Body, &b); err != nil {
		return v, err
	}
	for _, r := range b.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ScenarioKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
		}
	}
	if v.Version != q.ExpectedVersion || b.Status == "ARCHIVED" || strings.TrimSpace(q.Reason) == "" || len(q.SelectedCandidateIDs) == 0 {
		return v, model.ErrAnalysisConflict
	}
	run, err := s.Analysis.Get(ctx, a, analytics.KindInvestment, q.EvaluationRunID)
	if err != nil {
		return v, err
	}
	var params model.MaintenanceRunParameters
	if decode(run.Parameters, &params) != nil || params.ScenarioRevisionID != v.ID || run.Status != model.AnalysisSucceeded {
		return v, invalid("决定须绑定该方案版本的完成评估")
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindInvestment, run.ID)
	if err != nil {
		return v, err
	}
	selected := map[string]bool{}
	for _, id := range q.SelectedCandidateIDs {
		if selected[id] {
			return v, invalid("候选选择重复")
		}
		selected[id] = true
		found := false
		for _, c := range b.Candidates {
			found = found || c.ID == id
		}
		if !found {
			return v, invalid("选择不属于固定候选集合")
		}
	}
	b.Decisions = append(b.Decisions, model.InvestmentDecision{EvaluationRunID: run.ID, FactsHash: snapshot.FactsHash, SelectedCandidateIDs: q.SelectedCandidateIDs, Reason: q.Reason, Actor: a.Username, RecordedAt: s.now()})
	b.Status = "DECIDED"
	return s.commit(ctx, a, ScenarioKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) ScenarioAction(ctx context.Context, a analytics.Actor, id string, q model.MaintenanceActionRequest) (model.AnalysisConfigRevision, error) {
	v, err := s.Latest(ctx, a, ScenarioKind, id)
	if err != nil {
		return v, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/investment-scenarios/:id/actions", v.DeviceIDs); err != nil {
		return v, err
	}
	var b model.InvestmentScenario
	if err = decode(v.Body, &b); err != nil {
		return v, err
	}
	for _, r := range b.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ScenarioKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
		}
	}
	if q.ExpectedVersion != v.Version || strings.TrimSpace(q.Reason) == "" {
		return v, model.ErrAnalysisConflict
	}
	if q.Action == "REVIEW" && b.Status == "DRAFT" {
		b.Status = "REVIEWED"
	} else if q.Action == "ARCHIVE" && b.Status == "DECIDED" {
		b.Status = "ARCHIVED"
	} else {
		return v, model.ErrAnalysisConflict
	}
	return s.commit(ctx, a, ScenarioKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
