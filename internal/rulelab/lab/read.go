package lab

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func (s *Service) GetExperimentCompletedRun(ctx context.Context, a analytics.Actor, id string) (model.AnalysisRun, error) {
	fixed, _, _, err := s.fixedExperiment(ctx, a, id)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	hash, err := experimentHash(fixed)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	for offset := 0; ; {
		runs, total, err := s.ExperimentRuns(ctx, a, id, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return model.AnalysisRun{}, err
		}
		for _, v := range runs {
			if v.ConfigurationVersion == hash && (v.Status == model.AnalysisSucceeded || v.Status == model.AnalysisPartial) && v.SnapshotID != "" {
				return v, nil
			}
		}
		offset += len(runs)
		if offset >= total {
			return model.AnalysisRun{}, model.ErrAnalysisConflict
		}
	}
}
func (s *Service) SaveAICandidate(ctx context.Context, a analytics.Actor, id string, rule model.AlarmRule) (model.AnalysisConfigRevision, error) {
	v, expected, err := s.PrepareAICandidate(ctx, a, id, rule)
	if err != nil {
		return v, err
	}
	return s.Analysis.Store.PutAnalysisConfig(ctx, v, expected)
}

// PrepareAICandidate validates a disabled immutable draft without writing. The
// AI worker must persist it in the same lease-fenced transaction as SUCCESS.
func (s *Service) PrepareAICandidate(ctx context.Context, a analytics.Actor, id string, rule model.AlarmRule) (model.AnalysisConfigRevision, int64, error) {
	v, err := s.GetConfig(ctx, a, "experiments", id)
	if err != nil {
		return v, 0, err
	}
	var b model.RuleLabExperiment
	if err = decode(v.Body, &b); err != nil {
		return v, 0, err
	}
	rule.ID = b.CandidateRuleID
	rule.TenantID = a.TenantID
	rule.ProductID = b.Candidate.ProductID
	rule.Enabled = false
	b.Candidate = rule
	b.CandidateEnabled = false
	body, _ := json.Marshal(b)
	prepared, err := s.prepareExperiment(ctx, a, model.RuleLabConfigRequest{ResourceID: v.ResourceID, ExpectedVersion: v.Version, Scope: v.Scope, DeviceIDs: v.DeviceIDs, Body: body})
	return prepared, v.Version, err
}
func (s *Service) Review(ctx context.Context, a analytics.Actor, findingID string, q model.QualityReviewRequest) (model.AnalysisReview, error) {
	r, err := s.Analysis.Get(ctx, a, analytics.KindRuleLab, q.RunID)
	if err != nil {
		return model.AnalysisReview{}, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/rule-lab/findings/:id/reviews", r.DeviceIDs); err != nil {
		return model.AnalysisReview{}, err
	}
	if !slices.Contains([]string{"CONFIRMED_PROBLEM", "NORMAL_EXPLANATION", "RESOLVED", "OBSERVE"}, q.Result) || strings.TrimSpace(q.Explanation) == "" {
		return model.AnalysisReview{}, invalid("人工结论与依据无效")
	}
	found := false
	for offset := 0; !found; {
		page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, a.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "findings", Limit: 100, Offset: offset})
		if err != nil {
			return model.AnalysisReview{}, err
		}
		for _, v := range page {
			if v.ID == findingID {
				found = true
				break
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
	}
	if !found {
		return model.AnalysisReview{}, model.ErrNotFound
	}
	return s.Analysis.Store.AppendAnalysisReview(ctx, model.AnalysisReview{ID: uuid.NewString(), TenantID: a.TenantID, RunID: r.ID, ResourceID: findingID, ResourceVersion: 1, Reviewer: a.Username, Result: q.Result, Explanation: q.Explanation, CorrectsID: q.CorrectsID, IdempotencyKey: q.IdempotencyKey}, q.ExpectedRunVersion)
}
func (s *Service) Reviews(ctx context.Context, a analytics.Actor, runID, findingID string, f model.AnalysisFilter) ([]model.AnalysisReview, int, error) {
	if _, err := s.Analysis.Get(ctx, a, analytics.KindRuleLab, runID); err != nil {
		return nil, 0, err
	}
	f.RunID = runID
	f.ResourceID = findingID
	return s.Analysis.Store.ListAnalysisReviews(ctx, a.TenantID, f)
}
func (s *Service) Report(ctx context.Context, a analytics.Actor, experimentID, runID string) (json.RawMessage, error) {
	v, err := s.GetConfig(ctx, a, "experiments", experimentID)
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, a, "GET /api/v1/rule-lab/experiments/:id/report", v.DeviceIDs); err != nil {
		return nil, err
	}
	var run model.AnalysisRun
	if runID == "" {
		run, err = s.GetExperimentCompletedRun(ctx, a, experimentID)
	} else {
		run, err = s.Analysis.Get(ctx, a, analytics.KindRuleLab, runID)
	}
	if err != nil {
		return nil, err
	}
	var p model.RuleLabRunParameters
	if err = decode(run.Parameters, &p); err != nil || p.ExperimentRevisionID != experimentID {
		return nil, model.ErrNotFound
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindRuleLab, run.ID)
	if err != nil {
		return nil, err
	}
	fixed, err := s.loadFrozenExperiment(ctx, run)
	if err != nil {
		return nil, err
	}
	outputs := []model.AnalysisOutput{}
	evidence := []model.AnalysisEvidence{}
	reviews := []model.AnalysisReview{}
	aiReports := []model.AnalysisAIRevision{}
	if s.AI != nil {
		for offset := 0; ; {
			page, total, err := s.AI.List(ctx, a, analytics.KindRuleLab, run.ID, model.AnalysisFilter{Limit: 100, Offset: offset})
			if err != nil {
				return nil, err
			}
			aiReports = append(aiReports, page...)
			offset += len(page)
			if offset >= total {
				break
			}
			if len(page) == 0 {
				return nil, invalid("AI报告分页未推进")
			}
		}
	}
	for _, kind := range []string{"outcomes", "diffs", "metrics", "findings", "labels"} {
		for offset := 0; ; {
			page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, a.TenantID, model.AnalysisFilter{RunID: run.ID, Kind: kind, Limit: 100, Offset: offset})
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, page...)
			offset += len(page)
			if offset >= total {
				break
			}
		}
	}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisEvidence(ctx, a.TenantID, model.AnalysisFilter{RunID: run.ID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, page...)
		offset += len(page)
		if offset >= total {
			break
		}
	}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisReviews(ctx, a.TenantID, model.AnalysisFilter{RunID: run.ID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, page...)
		offset += len(page)
		if offset >= total {
			break
		}
	}
	run.LeaseOwner = ""
	run.LeaseToken = 0
	return json.Marshal(map[string]any{"format": "torchlink-rule-lab-v1", "run": run, "snapshot": snapshot, "experiment": v, "datasetRunId": fixed.DatasetRunID, "baselineRevisions": fixed.Baselines, "labelRevisions": fixed.Labels, "outputs": outputs, "evidence": evidence, "reviews": reviews, "aiReports": aiReports, "limitations": []string{"只执行逻辑判定与意向动作，不产生生产告警、通知、设备状态或联动写入", "历史模拟的时钟、顺序及初态假设不等同于生产精确重现"}})
}
