// Package lab runs bounded, read-only experiments on immutable datasets. It
// has no production alarm mutation, queue, realtime or device command ports.
package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/rulelab/eval"
)

const AlgorithmVersion = "rule-lab-dual-branch-v1"
const MaxRecords = 50000
const MaxDatasetBytes = 64 << 20

type Catalog interface {
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetProduct(context.Context, string, string) (model.Product, error)
}
type HistoryReader interface {
	GetRuleRevision(context.Context, string, string) (model.AlarmRuleRevision, error)
	ListRuleRevisions(context.Context, string, string, int, int) ([]model.AlarmRuleRevision, int, error)
	ListRuleEvaluationTraces(context.Context, string, model.RuleTraceFilter) ([]model.RuleEvaluationTrace, int, error)
}
type Service struct {
	Analysis          *analytics.Service
	Inputs            ports.RuleLabInputStore
	Facts             ports.AnalyticsFactStore
	Catalog           Catalog
	History           HistoryReader
	ValidateCandidate func(context.Context, analytics.Actor, model.AlarmRule) error
	RecordLimit       int
	AI                AIReports
}
type AIReports interface {
	List(context.Context, analytics.Actor, string, string, model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error)
}

func NewService(a *analytics.Service, inputs ports.RuleLabInputStore) *Service {
	return &Service{Analysis: a, Inputs: inputs, RecordLimit: MaxRecords}
}
func (s *Service) Register() error { return s.Analysis.Register(analytics.KindRuleLab, s.Process) }
func (s *Service) recordLimit() int {
	if s.RecordLimit <= 0 {
		return MaxRecords
	}
	return min(MaxRecords, s.RecordLimit)
}
func invalid(reason string) error { return fmt.Errorf("%w: %s", model.ErrAnalysisInvalid, reason) }
func decode(data json.RawMessage, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return invalid(err.Error())
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return invalid("只允许一个JSON对象")
	}
	return nil
}
func unique(values []string, value string) []string {
	if !slices.Contains(values, value) {
		return append(values, value)
	}
	return values
}
func (s *Service) authorize(ctx context.Context, a analytics.Actor, operation string, devices []string) (analytics.Actor, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil || !current.Allows(analytics.KindRuleLab, operation, devices) {
		return current, analytics.ErrForbidden
	}
	return current, nil
}

// Generic run creation is intentionally rejected. These two concrete creation
// paths bind a frozen dataset/experiment and validate its dedicated operation.
func (s *Service) ValidateCreate(context.Context, analytics.Actor, *analytics.CreateRequest) error {
	return invalid("请通过固定数据集或实验版本创建运行")
}
func resourceKind(resource string) string {
	switch resource {
	case "datasets":
		return model.RuleLabDatasetKind
	case "experiments":
		return model.RuleLabExperimentKind
	case "labels":
		return model.RuleLabLabelKind
	}
	return ""
}
func (s *Service) GetConfig(ctx context.Context, a analytics.Actor, resource, id string) (model.AnalysisConfigRevision, error) {
	v, err := s.Analysis.Store.GetAnalysisConfig(ctx, a.TenantID, id)
	if err != nil {
		return v, err
	}
	if v.Kind != resourceKind(resource) {
		return model.AnalysisConfigRevision{}, model.ErrNotFound
	}
	current, err := s.authorize(ctx, a, "", v.DeviceIDs)
	if err != nil || v.Scope == "PERSONAL" && v.Creator != current.Username {
		return model.AnalysisConfigRevision{}, analytics.ErrForbidden
	}
	return v, nil
}
func (s *Service) ListConfigs(ctx context.Context, a analytics.Actor, resource string, f model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return nil, 0, err
	}
	allowed := current.DeviceIDs
	if current.AllDevices {
		allowed = []string{"catalog"}
	}
	if !current.Allows(analytics.KindRuleLab, "", allowed) {
		return nil, 0, analytics.ErrForbidden
	}
	items := []model.AnalysisConfigRevision{}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: resourceKind(resource), Limit: 100, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		for _, v := range page {
			if (v.Scope != "PERSONAL" || v.Creator == a.Username) && current.Allows(analytics.KindRuleLab, "", v.DeviceIDs) {
				items = append(items, v)
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset >= 10000 {
			return nil, 0, invalid("版本读取范围超限")
		}
	}
	total := len(items)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AnalysisConfigRevision{}, total, nil
	}
	return items[offset:min(total, offset+limit)], total, nil
}
func (s *Service) putConfig(ctx context.Context, a analytics.Actor, resource string, q model.RuleLabConfigRequest, operation string) (model.AnalysisConfigRevision, error) {
	v, err := s.prepareConfig(ctx, a, resource, q, operation)
	if err != nil {
		return v, err
	}
	return s.Analysis.Store.PutAnalysisConfig(ctx, v, q.ExpectedVersion)
}
func (s *Service) prepareConfig(ctx context.Context, a analytics.Actor, resource string, q model.RuleLabConfigRequest, operation string) (model.AnalysisConfigRevision, error) {
	q.Scope = strings.ToUpper(strings.TrimSpace(q.Scope))
	if q.Scope == "" {
		q.Scope = "PERSONAL"
	}
	slices.Sort(q.DeviceIDs)
	q.DeviceIDs = slices.Compact(q.DeviceIDs)
	if q.ResourceID == "" || len(q.ResourceID) > 200 || q.ExpectedVersion < 0 || len(q.DeviceIDs) == 0 || len(q.DeviceIDs) > min(1000, s.Analysis.Limits.MaxDevices) || !slices.Contains([]string{"PERSONAL", "SHARED"}, q.Scope) {
		return model.AnalysisConfigRevision{}, invalid("资源、版本、范围或共享级别无效")
	}
	if _, err := s.authorize(ctx, a, operation, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if q.Scope == "SHARED" {
		if _, err := s.authorize(ctx, a, "POST /api/v1/rule-lab/"+resource+"/publish", q.DeviceIDs); err != nil {
			return model.AnalysisConfigRevision{}, err
		}
	}
	if q.ExpectedVersion > 0 {
		found := false
		for offset := 0; ; {
			page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: resourceKind(resource), ResourceID: q.ResourceID, Limit: 100, Offset: offset})
			if err != nil {
				return model.AnalysisConfigRevision{}, err
			}
			for _, v := range page {
				if v.Version != q.ExpectedVersion {
					continue
				}
				found = true
				if v.Scope == "PERSONAL" && v.Creator != a.Username {
					return model.AnalysisConfigRevision{}, analytics.ErrForbidden
				}
				if _, err = s.authorize(ctx, a, operation, v.DeviceIDs); err != nil {
					return model.AnalysisConfigRevision{}, err
				}
			}
			offset += len(page)
			if offset >= total {
				break
			}
			if offset >= 10000 {
				return model.AnalysisConfigRevision{}, invalid("版本读取范围超限")
			}
		}
		if !found {
			return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
		}
	}
	if s.Catalog == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	for _, id := range q.DeviceIDs {
		d, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, id)
		if err != nil || d.TenantID != a.TenantID {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
	}
	return model.AnalysisConfigRevision{ID: uuid.NewString(), TenantID: a.TenantID, Kind: resourceKind(resource), ResourceID: q.ResourceID, Scope: q.Scope, DeviceIDs: q.DeviceIDs, Creator: a.Username, Body: q.Body}, nil
}

type datasetConfig struct {
	Selection model.RuleLabDatasetRequest `json:"selection"`
	RunID     string                      `json:"runId"`
}

func (s *Service) CreateDataset(ctx context.Context, a analytics.Actor, q model.RuleLabDatasetRequest) (model.RuleLabDataset, error) {
	q.Scope = strings.ToUpper(strings.TrimSpace(q.Scope))
	if q.Scope == "" {
		q.Scope = "PERSONAL"
	}
	slices.Sort(q.DeviceIDs)
	q.DeviceIDs = slices.Compact(q.DeviceIDs)
	if q.Start <= 0 || q.End <= q.Start || q.WarmupStart < 0 || q.WarmupStart > q.Start || q.End-q.WarmupStart > s.Analysis.Limits.MaxRange.Milliseconds() || len(q.DeviceIDs) == 0 || len(q.DeviceIDs) > min(1000, s.Analysis.Limits.MaxDevices) || q.IdempotencyKey == "" || len(q.IdempotencyKey) > 160 || !slices.Contains([]string{"PERSONAL", "SHARED"}, q.Scope) || !slices.Contains([]string{"EVENT", "RECEIVED"}, q.TimeBasis) || !slices.Contains([]string{"EVENT_AS_PROCESSING", "RECEIVED_AS_PROCESSING", "RECORDED_TRACE"}, q.ClockPolicy) || !slices.Contains([]string{eval.ProcessingV1, eval.RevisionV2}, q.SemanticsVersion) || !slices.Contains([]string{"EMPTY_UNKNOWN", "TRACE_INITIAL"}, q.InitialStatePolicy) {
		return model.RuleLabDataset{}, invalid("固定数据集的范围、排序、时钟与初态政策须明确且符合保护上限")
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/rule-lab/datasets", q.DeviceIDs); err != nil {
		return model.RuleLabDataset{}, err
	}
	if q.Scope == "SHARED" {
		if _, err := s.authorize(ctx, a, "POST /api/v1/rule-lab/datasets/publish", q.DeviceIDs); err != nil {
			return model.RuleLabDataset{}, err
		}
	}
	if s.Inputs == nil {
		return model.RuleLabDataset{}, analytics.ErrUnsupported
	}
	if q.ClockPolicy == "RECORDED_TRACE" && s.History == nil {
		return model.RuleLabDataset{}, analytics.ErrUnsupported
	}
	keyHash, _ := analytics.AnalysisHash([]string{a.Username, q.IdempotencyKey})
	id := "rulelab-dataset:" + keyHash
	if old, err := s.GetConfig(ctx, a, "datasets", id); err == nil {
		var body datasetConfig
		if err = decode(old.Body, &body); err != nil {
			return model.RuleLabDataset{}, err
		}
		first, _ := analytics.AnalysisHash(body.Selection)
		second, _ := analytics.AnalysisHash(q)
		if first != second {
			return model.RuleLabDataset{}, model.ErrAnalysisConflict
		}
		return s.Dataset(ctx, a, id)
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.RuleLabDataset{}, err
	}
	configuration, _ := analytics.AnalysisHash(q)
	parameters, _ := json.Marshal(model.RuleLabRunParameters{Phase: "DATASET", DatasetID: id, DatasetSelection: &q})
	run, err := s.Analysis.Create(ctx, a, analytics.KindRuleLab, AlgorithmVersion, analytics.CreateRequest{DeviceIDs: q.DeviceIDs, Start: q.Start, End: q.End, ConfigurationVersion: configuration, Parameters: parameters, IdempotencyKey: "dataset:" + q.IdempotencyKey, CreationOperation: "POST /api/v1/rule-lab/datasets"})
	if err != nil {
		return model.RuleLabDataset{}, err
	}
	body, _ := json.Marshal(datasetConfig{q, run.ID})
	_, err = s.Analysis.Store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: id, TenantID: a.TenantID, Kind: model.RuleLabDatasetKind, ResourceID: id, Scope: q.Scope, DeviceIDs: q.DeviceIDs, Creator: a.Username, Body: body}, 0)
	if err != nil && !errors.Is(err, model.ErrAnalysisConflict) {
		return model.RuleLabDataset{}, err
	}
	return s.Dataset(ctx, a, id)
}
func (s *Service) Dataset(ctx context.Context, a analytics.Actor, id string) (model.RuleLabDataset, error) {
	v, err := s.GetConfig(ctx, a, "datasets", id)
	if err != nil {
		return model.RuleLabDataset{}, err
	}
	var body datasetConfig
	if err = decode(v.Body, &body); err != nil {
		return model.RuleLabDataset{}, err
	}
	r, err := s.Analysis.Get(ctx, a, analytics.KindRuleLab, body.RunID)
	if err != nil {
		return model.RuleLabDataset{}, err
	}
	d := model.RuleLabDataset{ID: id, RunID: r.ID, RuleLabDatasetRequest: body.Selection, Status: r.Status, Coverage: r.Sources, InitialStateQuality: "UNKNOWN", ReproductionQuality: "HISTORICAL_SIMULATION", Limitations: []string{}}
	if r.SnapshotID != "" {
		snap, err := s.Analysis.Snapshot(ctx, a, analytics.KindRuleLab, r.ID)
		if err != nil {
			return d, err
		}
		var summary struct {
			InputCount          int    `json:"inputCount"`
			ReproductionQuality string `json:"reproductionQuality"`
		}
		if err = json.Unmarshal(snap.Statistics, &summary); err != nil {
			return d, err
		}
		d.InputCount = summary.InputCount
		d.ReproductionQuality = summary.ReproductionQuality
		d.InitialStateQuality = snap.InitialStateQuality
		d.FactsHash = snap.FactsHash
		d.Limitations = snap.Limitations
	}
	return d, nil
}
func (s *Service) SaveLabel(ctx context.Context, a analytics.Actor, q model.RuleLabConfigRequest) (model.AnalysisConfigRevision, error) {
	var b model.RuleLabLabel
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if len(q.DeviceIDs) != 1 || q.DeviceIDs[0] != b.DeviceID || b.EventType == "" || b.Start <= 0 || b.End <= b.Start || b.End-b.Start > s.Analysis.Limits.MaxRange.Milliseconds() || !slices.Contains([]string{"CONFIRMED_EVENT", "TEST", "NO_ABNORMALITY_FOUND", "UNKNOWN"}, b.Conclusion) || strings.TrimSpace(b.Basis) == "" || b.ConfirmedBy != "" || b.ConfirmedAt != 0 || b.Status != "" && b.Status != "DRAFT" {
		return model.AnalysisConfigRevision{}, invalid("标签须明确设备、事件类型、范围与核实依据，确认人由服务器绑定")
	}
	b.Status = "DRAFT"
	q.Body, _ = json.Marshal(b)
	return s.putConfig(ctx, a, "labels", q, "POST /api/v1/rule-lab/labels")
}
func (s *Service) ConfirmLabel(ctx context.Context, a analytics.Actor, id string, expected int64) (model.AnalysisConfigRevision, error) {
	v, err := s.GetConfig(ctx, a, "labels", id)
	if err != nil {
		return v, err
	}
	var b model.RuleLabLabel
	if err = decode(v.Body, &b); err != nil {
		return v, err
	}
	if v.Version != expected || b.Status != "DRAFT" {
		return v, model.ErrAnalysisConflict
	}
	b.Status = "CONFIRMED"
	b.ConfirmedBy = a.Username
	b.ConfirmedAt = time.Now().UnixMilli()
	body, _ := json.Marshal(b)
	return s.putConfig(ctx, a, "labels", model.RuleLabConfigRequest{ResourceID: v.ResourceID, ExpectedVersion: v.Version, Scope: v.Scope, DeviceIDs: v.DeviceIDs, Body: body}, "POST /api/v1/rule-lab/labels/:id/confirm")
}
func (s *Service) SaveExperiment(ctx context.Context, a analytics.Actor, q model.RuleLabConfigRequest) (model.AnalysisConfigRevision, error) {
	v, err := s.prepareExperiment(ctx, a, q)
	if err != nil {
		return v, err
	}
	return s.Analysis.Store.PutAnalysisConfig(ctx, v, q.ExpectedVersion)
}
func (s *Service) prepareExperiment(ctx context.Context, a analytics.Actor, q model.RuleLabConfigRequest) (model.AnalysisConfigRevision, error) {
	q.Scope = strings.ToUpper(strings.TrimSpace(q.Scope))
	if q.Scope == "" {
		q.Scope = "PERSONAL"
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/rule-lab/experiments", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.RuleLabExperiment
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	dataset, err := s.Dataset(ctx, a, b.DatasetID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	selected := slices.Clone(q.DeviceIDs)
	slices.Sort(selected)
	selected = slices.Compact(selected)
	if !slices.Equal(selected, dataset.DeviceIDs) {
		return model.AnalysisConfigRevision{}, invalid("实验设备集合须与固定数据集相同")
	}
	if dataset.Status != model.AnalysisSucceeded && dataset.Status != model.AnalysisPartial {
		return model.AnalysisConfigRevision{}, invalid("固定数据集尚未完成")
	}
	if q.Scope == "SHARED" && dataset.Scope == "PERSONAL" {
		return model.AnalysisConfigRevision{}, invalid("共享实验不能引用私人数据集")
	}
	if s.History == nil || s.ValidateCandidate == nil || s.Catalog == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	if b.BaselinePolicy == "" {
		b.BaselinePolicy = "FIXED_REVISIONS"
	}
	if !slices.Contains([]string{"FIXED_REVISIONS", "RECORDED_ACTIVATIONS"}, b.BaselinePolicy) || b.BaselinePolicy == "RECORDED_ACTIVATIONS" && dataset.ClockPolicy != "RECORDED_TRACE" || len(b.BaselineRevisionIDs) == 0 || len(b.BaselineRevisionIDs) > 500 || len(b.LabelRevisionIDs) > 10000 || strings.TrimSpace(b.Hypothesis) == "" || b.CandidateRuleID == "" {
		return model.AnalysisConfigRevision{}, invalid("基线、候选规则与行为假设无效")
	}
	seen := map[string]bool{}
	target := model.AlarmRuleRevision{}
	for _, id := range b.BaselineRevisionIDs {
		v, err := s.History.GetRuleRevision(ctx, a.TenantID, id)
		if err != nil {
			return model.AnalysisConfigRevision{}, err
		}
		if v.TenantID != a.TenantID || v.Hash != model.RuleBodyHash(v.Rule) || seen[v.RuleID] {
			return model.AnalysisConfigRevision{}, invalid("基线不可变版本集合无效")
		}
		seen[v.RuleID] = true
		if v.RuleID == b.CandidateRuleID {
			target = v
		}
		if v.Rule.ProductID != "" {
			applicable := false
			for _, device := range selected {
				d, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, device)
				if err != nil || d.TenantID != a.TenantID {
					return model.AnalysisConfigRevision{}, analytics.ErrForbidden
				}
				applicable = applicable || d.ProductID == v.Rule.ProductID
			}
			if !applicable {
				return model.AnalysisConfigRevision{}, analytics.ErrForbidden
			}
		}
		permitted := v.Rule
		permitted.Enabled = false
		if s.ValidateCandidate(ctx, a, permitted) != nil {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}

	}
	if target.ID == "" || b.Candidate.ID != b.CandidateRuleID || b.Candidate.ProductID != target.Rule.ProductID || b.Candidate.TenantID != "" && b.Candidate.TenantID != a.TenantID {
		return model.AnalysisConfigRevision{}, invalid("候选只能替换选择的同目标规则")
	}
	b.Candidate.TenantID = a.TenantID
	b.Candidate.Enabled = false
	b.Candidate.Version = target.Version + 1
	b.Candidate.CreatedAt = target.Rule.CreatedAt
	b.Candidate.UpdatedAt = 0
	if err = s.ValidateCandidate(ctx, a, b.Candidate); err != nil {
		return model.AnalysisConfigRevision{}, invalid(err.Error())
	}
	p := b.EvaluationPolicy
	if p.Version == "" || p.ToleranceMs < 0 || p.ToleranceMs > s.Analysis.Limits.MaxRange.Milliseconds() || !slices.Contains([]string{"EVENT", "PROCESSING"}, p.MatchTimeBasis) || p.ExtraCyclePolicy != "COUNT_EACH_CYCLE" || !slices.Contains([]string{"TUNING", "HOLDOUT"}, p.Split) || p.RepresentativeConfirmed && strings.TrimSpace(p.RepresentativenessBasis) == "" {
		return model.AnalysisConfigRevision{}, invalid("事件匹配、额外周期及调参/留出政策须预先固定")
	}
	labels := map[string]bool{}
	for _, id := range b.LabelRevisionIDs {
		if labels[id] {
			return model.AnalysisConfigRevision{}, invalid("标签版本重复")
		}
		labels[id] = true
		v, err := s.GetConfig(ctx, a, "labels", id)
		if err != nil {
			return model.AnalysisConfigRevision{}, err
		}
		var label model.RuleLabLabel
		if err = decode(v.Body, &label); err != nil {
			return model.AnalysisConfigRevision{}, err
		}
		if label.Status != "CONFIRMED" || label.ConfirmedBy == "" || !slices.Contains(selected, label.DeviceID) || label.Start < dataset.Start || label.End > dataset.End || q.Scope == "SHARED" && v.Scope == "PERSONAL" {
			return model.AnalysisConfigRevision{}, invalid("标签未核实、范围不符或共享实验引用私人标签")
		}
	}
	q.Body, _ = json.Marshal(b)
	return s.prepareConfig(ctx, a, "experiments", q, "POST /api/v1/rule-lab/experiments")
}
