package lab

import (
	"context"
	"encoding/json"
	"slices"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func (s *Service) ExperimentRuns(ctx context.Context, a analytics.Actor, id string, f model.AnalysisFilter) ([]model.AnalysisRun, int, error) {
	config, err := s.GetConfig(ctx, a, "experiments", id)
	if err != nil {
		return nil, 0, err
	}
	current, err := s.authorize(ctx, a, "", config.DeviceIDs)
	if err != nil {
		return nil, 0, err
	}
	items := []model.AnalysisRun{}
	for offset := 0; ; {
		runs, total, err := s.Analysis.Store.ListAnalysisRuns(ctx, a.TenantID, model.AnalysisFilter{Kind: analytics.KindRuleLab, Limit: 100, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		for _, v := range runs {
			var p model.RuleLabRunParameters
			if json.Unmarshal(v.Parameters, &p) == nil && p.Phase == "EXPERIMENT" && p.ExperimentRevisionID == id && current.Allows(analytics.KindRuleLab, "", v.DeviceIDs) {
				v.LeaseOwner = ""
				v.LeaseToken = 0
				items = append(items, v)
			}
		}
		offset += len(runs)
		if offset >= total {
			break
		}
		if offset >= 10000 {
			return nil, 0, invalid("实验任务读取范围超限")
		}
	}
	total := len(items)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AnalysisRun{}, total, nil
	}
	return items[offset:min(total, offset+limit)], total, nil
}

// The HTTP publication executor separately enforces current rule management
// over every affected device and performs the atomic production CAS. This
// method validates only the immutable experiment reference and its tested body.
func (s *Service) ValidatePublication(ctx context.Context, a analytics.Actor, experimentID string, rule model.AlarmRule, baselineVersion int) error {
	fixed, b, _, err := s.fixedExperiment(ctx, a, experimentID)
	if err != nil {
		return err
	}
	if b.CandidateRuleID != rule.ID || rule.TenantID != a.TenantID || b.Candidate.ProductID != rule.ProductID || rule.Enabled != b.CandidateEnabled {
		return invalid("发布候选与固定实验不一致")
	}
	found := false
	for _, v := range fixed.Baselines {
		if v.RuleID == rule.ID {
			found = true
			if v.Version != baselineVersion {
				return model.ErrRuleConflict
			}
		}
	}
	if !found {
		return invalid("发布基线不属于实验")
	}
	proposal := rule
	proposal.Version = b.Candidate.Version
	proposal.CreatedAt = b.Candidate.CreatedAt
	proposal.UpdatedAt = b.Candidate.UpdatedAt
	proposal.Enabled = false
	if model.RuleBodyHash(proposal) != model.RuleBodyHash(b.Candidate) {
		return invalid("发布正文与实验中的候选不同")
	}
	for offset := 0; ; {
		runs, total, err := s.ExperimentRuns(ctx, a, experimentID, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return err
		}
		for _, r := range runs {
			if (r.Status == model.AnalysisSucceeded || r.Status == model.AnalysisPartial) && r.SnapshotID != "" {
				return nil
			}
		}
		offset += len(runs)
		if offset >= total {
			return invalid("发布需引用已有完成结果的实验")
		}
	}
}

type frozenExperiment struct {
	Version             string                         `json:"version"`
	Experiment          model.AnalysisConfigRevision   `json:"experiment"`
	DatasetRunID        string                         `json:"datasetRunId"`
	DatasetFactsHash    string                         `json:"datasetFactsHash"`
	DatasetInputHashes  []string                       `json:"datasetInputHashes"`
	Baselines           []model.AlarmRuleRevision      `json:"baselines"`
	Labels              []model.AnalysisConfigRevision `json:"labels"`
	HoldoutPreviousUses int                            `json:"holdoutPreviousUses"`
}

func (s *Service) fixedExperiment(ctx context.Context, a analytics.Actor, id string) (frozenExperiment, model.RuleLabExperiment, model.RuleLabDataset, error) {
	v, err := s.GetConfig(ctx, a, "experiments", id)
	if err != nil {
		return frozenExperiment{}, model.RuleLabExperiment{}, model.RuleLabDataset{}, err
	}
	var b model.RuleLabExperiment
	if err = decode(v.Body, &b); err != nil {
		return frozenExperiment{}, b, model.RuleLabDataset{}, err
	}
	d, err := s.Dataset(ctx, a, b.DatasetID)
	if err != nil {
		return frozenExperiment{}, b, d, err
	}
	if d.Status != model.AnalysisSucceeded && d.Status != model.AnalysisPartial {
		return frozenExperiment{}, b, d, model.ErrAnalysisConflict
	}
	if !slices.Equal(v.DeviceIDs, d.DeviceIDs) || s.History == nil {
		return frozenExperiment{}, b, d, analytics.ErrForbidden
	}
	fixed := frozenExperiment{Version: "rulelab-experiment-v1", Experiment: v, DatasetRunID: d.RunID, DatasetFactsHash: d.FactsHash, Baselines: []model.AlarmRuleRevision{}, Labels: []model.AnalysisConfigRevision{}}
	r, err := s.Analysis.Get(ctx, a, analytics.KindRuleLab, d.RunID)
	if err != nil {
		return fixed, b, d, err
	}
	fixed.DatasetInputHashes = r.InputHashes
	for _, id := range b.BaselineRevisionIDs {
		revision, err := s.History.GetRuleRevision(ctx, a.TenantID, id)
		if err != nil {
			return fixed, b, d, err
		}
		if revision.TenantID != a.TenantID || revision.Hash != model.RuleBodyHash(revision.Rule) {
			return fixed, b, d, model.ErrAnalysisConflict
		}
		fixed.Baselines = append(fixed.Baselines, revision)
	}
	for _, id := range b.LabelRevisionIDs {
		label, err := s.GetConfig(ctx, a, "labels", id)
		if err != nil {
			return fixed, b, d, err
		}
		var body model.RuleLabLabel
		if err = decode(label.Body, &body); err != nil {
			return fixed, b, d, err
		}
		if body.Status != "CONFIRMED" || !slices.Contains(v.DeviceIDs, body.DeviceID) {
			return fixed, b, d, model.ErrAnalysisConflict
		}
		fixed.Labels = append(fixed.Labels, label)
	}
	return fixed, b, d, nil
}
func experimentHash(f frozenExperiment) (string, error) {
	f.HoldoutPreviousUses = 0
	return analytics.AnalysisHash(f)
}
func (s *Service) CreateExperimentRun(ctx context.Context, a analytics.Actor, id string, q model.RuleLabRunRequest) (model.AnalysisRun, error) {
	fixed, _, d, err := s.fixedExperiment(ctx, a, id)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/rule-lab/experiments/:id/runs", d.DeviceIDs); err != nil {
		return model.AnalysisRun{}, err
	}
	configuration, err := experimentHash(fixed)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	parameters, _ := json.Marshal(model.RuleLabRunParameters{Phase: "EXPERIMENT", DatasetID: d.ID, ExperimentRevisionID: id})
	return s.Analysis.Create(ctx, a, analytics.KindRuleLab, AlgorithmVersion, analytics.CreateRequest{DeviceIDs: d.DeviceIDs, Start: d.Start, End: d.End, ConfigurationVersion: configuration, Parameters: parameters, IdempotencyKey: q.IdempotencyKey, CreationOperation: "POST /api/v1/rule-lab/experiments/:id/runs"})
}
func (s *Service) ListDatasets(ctx context.Context, a analytics.Actor, f model.AnalysisFilter) ([]model.RuleLabDataset, int, error) {
	configs, total, err := s.ListConfigs(ctx, a, "datasets", f)
	if err != nil {
		return nil, 0, err
	}
	items := make([]model.RuleLabDataset, 0, len(configs))
	for _, v := range configs {
		d, err := s.Dataset(ctx, a, v.ID)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, d)
	}
	return items, total, nil
}
func (s *Service) loadFrozenExperiment(ctx context.Context, r model.AnalysisRun) (frozenExperiment, error) {
	page, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "input-manifest", Limit: 10})
	if err != nil {
		return frozenExperiment{}, err
	}
	if len(page) != 1 {
		return frozenExperiment{}, model.ErrNotFound
	}
	var m frozenExperiment
	if err = json.Unmarshal(page[0].Body, &m); err != nil {
		return m, err
	}
	hash, err := analytics.AnalysisHash(m)
	if err != nil || len(r.InputHashes) != 1 || hash != r.InputHashes[0] {
		return m, model.ErrAnalysisConflict
	}
	return m, nil
}
func (s *Service) freezeExperiment(ctx context.Context, e *analytics.Execution, p model.RuleLabRunParameters) (frozenExperiment, model.RuleLabExperiment, datasetChunk, error) {
	a := analytics.Actor{TenantID: e.Run.TenantID, Username: e.Run.Creator, Managed: e.Run.CreatorManaged, SessionVersion: e.Run.CreatorSessionVersion, AccessVersion: e.Run.PermissionsVersion}
	var fixed frozenExperiment
	var b model.RuleLabExperiment
	var err error
	if e.Run.InputsFrozen {
		fixed, err = s.loadFrozenExperiment(ctx, e.Run)
		if err != nil {
			return fixed, b, datasetChunk{}, err
		}
		if err = decode(fixed.Experiment.Body, &b); err != nil {
			return fixed, b, datasetChunk{}, err
		}
		hash, hashErr := experimentHash(fixed)
		if hashErr != nil || hash != e.Run.ConfigurationVersion || fixed.Experiment.ID != p.ExperimentRevisionID || b.DatasetID != p.DatasetID || !slices.Equal(fixed.Experiment.DeviceIDs, e.Run.DeviceIDs) {
			return fixed, b, datasetChunk{}, model.ErrAnalysisConflict
		}
		if _, err = s.authorize(ctx, a, "", fixed.Experiment.DeviceIDs); err != nil {
			return fixed, b, datasetChunk{}, err
		}
	} else {
		var d model.RuleLabDataset
		fixed, b, d, err = s.fixedExperiment(ctx, a, p.ExperimentRevisionID)
		if err != nil {
			return fixed, b, datasetChunk{}, err
		}
		if p.DatasetID != d.ID || !slices.Equal(e.Run.DeviceIDs, d.DeviceIDs) || e.Run.Start != d.Start || e.Run.End != d.End {
			return fixed, b, datasetChunk{}, model.ErrAnalysisConflict
		}
		hash, hashErr := experimentHash(fixed)
		if hashErr != nil || hash != e.Run.ConfigurationVersion {
			return fixed, b, datasetChunk{}, model.ErrAnalysisConflict
		}
	}
	r, err := s.Analysis.Store.GetAnalysisRun(ctx, e.Run.TenantID, fixed.DatasetRunID)
	if err != nil {
		return fixed, b, datasetChunk{}, err
	}
	if !slices.Equal(r.InputHashes, fixed.DatasetInputHashes) || r.SnapshotID == "" {
		return fixed, b, datasetChunk{}, model.ErrAnalysisConflict
	}
	m, err := s.loadDatasetChunk(ctx, r)
	if err != nil {
		return fixed, b, m, err
	}
	if !e.Run.InputsFrozen {
		if b.EvaluationPolicy.Split == "HOLDOUT" {
			for offset := 0; ; {
				runs, total, err := s.Analysis.List(ctx, a, analytics.KindRuleLab, model.AnalysisFilter{Limit: 100, Offset: offset})
				if err != nil {
					return fixed, b, m, err
				}
				for _, previous := range runs {
					if previous.ID == e.Run.ID || previous.Start != e.Run.Start || previous.End != e.Run.End || !slices.Equal(previous.DeviceIDs, e.Run.DeviceIDs) {
						continue
					}
					var params model.RuleLabRunParameters
					if json.Unmarshal(previous.Parameters, &params) == nil && params.Phase == "EXPERIMENT" {
						fixed.HoldoutPreviousUses++
					}
				}
				offset += len(runs)
				if offset >= total {
					break
				}
				if offset >= 10000 {
					return fixed, b, m, invalid("留出区间重用记录查询超限")
				}
			}
		}
		hash, err := analytics.AnalysisHash(fixed)
		if err != nil {
			return fixed, b, m, err
		}
		body, _ := json.Marshal(fixed)
		err = e.Commit(ctx, model.AnalysisBatch{ID: "experiment-freeze", FreezeInputs: true, InputHashes: []string{hash}, Sources: m.Sources, DataCutoff: m.Cutoff, Processed: 0, Status: model.AnalysisRunning, Stage: "experiment-inputs-frozen", Outputs: []model.AnalysisOutput{{ID: e.Run.ID + ":input-manifest", Kind: "input-manifest", Body: body}}})
		if err != nil {
			return fixed, b, m, err
		}
	}
	return fixed, b, m, nil
}
