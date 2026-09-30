package lab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type datasetChunk struct {
	Version             string                         `json:"version"`
	Selection           model.RuleLabDatasetRequest    `json:"selection"`
	Inputs              []model.RuleLabInput           `json:"inputs"`
	ManualActions       []model.BusinessEventFact      `json:"manualActions"`
	Sources             []model.AnalysisSourceCoverage `json:"sources"`
	Limitations         []string                       `json:"limitations"`
	Cutoff              int64                          `json:"cutoff"`
	InitialStateQuality string                         `json:"initialStateQuality"`
	ReproductionQuality string                         `json:"reproductionQuality"`
}

func (s *Service) Process(ctx context.Context, e *analytics.Execution) (resultErr error) {
	defer func() {
		if errors.Is(resultErr, context.DeadlineExceeded) {
			if err := s.timeout(ctx, e); err == nil {
				resultErr = nil
			}
		}
	}()
	var p model.RuleLabRunParameters
	if err := decode(e.Run.Parameters, &p); err != nil {
		return err
	}
	switch p.Phase {
	case "DATASET":
		return s.processDataset(ctx, e, p)
	case "EXPERIMENT":
		return s.processExperiment(ctx, e, p)
	}
	return invalid("实验运行阶段无效")
}
func addCoverage(sources []model.AnalysisSourceCoverage, c model.FactSourceCoverage) []model.AnalysisSourceCoverage {
	v := model.AnalysisSourceCoverage{Source: c.Source, Start: c.CoverageStart, End: c.CoverageEnd, Complete: c.Complete, ReadAt: c.ReadAt, Version: c.SourceVersion, Reason: strings.Join(append([]string{c.Status, c.BackfillStatus, c.HistoricalReconstructionQuality}, c.Limitations...), ";")}
	for i, old := range sources {
		if old.Source == v.Source {
			sources[i].Complete = old.Complete && v.Complete
			sources[i].Start = max(old.Start, v.Start)
			sources[i].End = min(old.End, v.End)
			sources[i].ReadAt = max(old.ReadAt, v.ReadAt)
			if !strings.Contains(old.Reason, v.Reason) {
				sources[i].Reason += ";" + v.Reason
			}
			return sources
		}
	}
	return append(sources, v)
}
func inputHash(v model.RuleLabInput) (string, error) { v.Hash = ""; return analytics.AnalysisHash(v) }
func (s *Service) processDataset(ctx context.Context, e *analytics.Execution, p model.RuleLabRunParameters) error {
	if p.DatasetSelection == nil || p.DatasetID == "" {
		return invalid("固定数据集选择缺失")
	}
	q := *p.DatasetSelection
	if !slices.Equal(q.DeviceIDs, e.Run.DeviceIDs) || q.Start != e.Run.Start || q.End != e.Run.End {
		return invalid("固定数据集与任务范围不符")
	}
	configuration, err := analytics.AnalysisHash(q)
	if err != nil || configuration != e.Run.ConfigurationVersion {
		return model.ErrAnalysisConflict
	}
	if e.Run.InputsFrozen {
		m, err := s.loadDatasetChunk(ctx, e.Run)
		if err != nil {
			return err
		}
		return s.finishDataset(ctx, e, m, false)
	}
	m := datasetChunk{Version: "rulelab-dataset-v1", Selection: q, Inputs: []model.RuleLabInput{}, ManualActions: []model.BusinessEventFact{}, Sources: []model.AnalysisSourceCoverage{}, Limitations: []string{}, Cutoff: time.Now().UnixMilli(), InitialStateQuality: "UNKNOWN", ReproductionQuality: "HISTORICAL_SIMULATION"}
	if q.InitialStatePolicy == "EMPTY_UNKNOWN" {
		m.Limitations = unique(m.Limitations, "EMPTY_INITIAL_BRANCH_STATE_IS_AN_ASSUMPTION")
	}
	if q.ClockPolicy != "RECORDED_TRACE" {
		m.Limitations = unique(m.Limitations, "EXPLICIT_VIRTUAL_PROCESSING_CLOCK_NOT_PRODUCTION_REPRODUCTION")
	}
	size, traceCount := 0, 0
	seen := map[string]bool{}
	clipped := false
	var readErr error
	if s.Inputs == nil {
		readErr = analytics.ErrUnsupported
	} else {
		readErr = s.Inputs.RuleLabInputsRead(ctx, e.Run.TenantID, func(reader ports.RuleLabInputReader) error {
			query := model.RuleLabInputQuery{DeviceIDs: e.Run.DeviceIDs, Start: q.WarmupStart, End: q.End, TimeBasis: q.TimeBasis, Limit: min(1000, s.Analysis.Limits.BatchSize)}
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				page, err := reader.ListStandardInputs(query)
				if err != nil {
					return err
				}
				for _, c := range append([]model.FactSourceCoverage{page.Source}, page.AdditionalSources...) {
					m.Sources = addCoverage(m.Sources, c)
					m.Cutoff = max(m.Cutoff, c.ReadAt)
				}
				pageTraces, err := s.pageTraces(ctx, e.Run, q, page.Items, &traceCount, &m.Limitations)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return err
					}
					m.Limitations = unique(m.Limitations, "PRODUCTION_TRACE_SOURCE_UNAVAILABLE")
					pageTraces = map[string][]model.RuleEvaluationTrace{}
				}
				for _, v := range page.Items {
					if v.Message.TenantID != e.Run.TenantID || !slices.Contains(e.Run.DeviceIDs, v.Message.DeviceID) || v.ID != v.Message.MessageID {
						return analytics.ErrForbidden
					}
					if seen[v.ID] {
						continue
					}
					seen[v.ID] = true
					if v.MetadataQuality == "SOURCE_RECORD_MISSING" {
						m.Limitations = unique(m.Limitations, "SOURCE_RECORD_MISSING")
						continue
					}
					if len(m.Inputs) >= s.recordLimit() {
						clipped = true
						break
					}
					v.Traces = pageTraces[v.ID]
					slices.SortFunc(v.Traces, func(a, b model.RuleEvaluationTrace) int {
						if a.StartedAt < b.StartedAt {
							return -1
						}
						if a.StartedAt > b.StartedAt {
							return 1
						}
						return strings.Compare(a.ID, b.ID)
					})
					v.Hash, err = inputHash(v)
					if err != nil {
						return err
					}
					body, err := json.Marshal(v)
					if err != nil {
						return err
					}
					if size+len(body) > MaxDatasetBytes {
						clipped = true
						m.Limitations = unique(m.Limitations, "DATASET_BYTE_CAP_REACHED")
						break
					}
					size += len(body)
					m.Inputs = append(m.Inputs, v)
				}
				if clipped {
					break
				}
				if !page.HasMore {
					return nil
				}
				if page.Cursor == "" || page.Cursor == query.Cursor {
					return invalid("数据集输入分页游标没有推进")
				}
				query.Cursor = page.Cursor
			}
			return nil
		})
	}
	if readErr != nil {
		if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
			return readErr
		}
		m.Inputs = nil
		m.Sources = []model.AnalysisSourceCoverage{{Source: "rulelab_standard_inputs", Start: q.WarmupStart, End: q.End, Complete: false, ReadAt: m.Cutoff, Reason: "SOURCE_READ_FAILED"}}
		m.Limitations = unique(m.Limitations, "SOURCE_READ_FAILED")
	}
	if clipped {
		m.Limitations = unique(m.Limitations, "DATASET_RECORD_OR_BYTE_CAP_REACHED")
		for i := range m.Sources {
			m.Sources[i].Complete = false
			m.Sources[i].Reason += ";DATASET_RECORD_OR_BYTE_CAP_REACHED"
		}
	}
	if s.Facts != nil {
		err := s.Facts.AnalyticsFactsRead(ctx, e.Run.TenantID, func(reader ports.AnalyticsFactReader) error {
			query := model.FactQuery{DeviceIDs: e.Run.DeviceIDs, Start: q.WarmupStart, End: q.End, Limit: 1000}
			for {
				page, err := reader.ListAlarmLifecycleEvents(query)
				if err != nil {
					return err
				}
				m.Sources = addCoverage(m.Sources, page.Source)
				for _, v := range page.Items {
					if len(m.ManualActions) >= s.recordLimit() {
						m.Limitations = unique(m.Limitations, "MANUAL_ACTION_READ_CAP_REACHED")
						for i := range m.Sources {
							if m.Sources[i].Source == page.Source.Source {
								m.Sources[i].Complete = false
								m.Sources[i].Reason += ";MANUAL_ACTION_READ_CAP_REACHED"
							}
						}
						return nil
					}
					m.ManualActions = append(m.ManualActions, v)
				}
				if !page.HasMore {
					return nil
				}
				if page.Cursor == "" || query.Cursor == page.Cursor {
					return invalid("人工动作分页游标没有推进")
				}
				query.Cursor = page.Cursor
			}
		})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			m.ManualActions = nil
			m.Sources = append(m.Sources, model.AnalysisSourceCoverage{Source: "alarm_lifecycle", Start: q.WarmupStart, End: q.End, ReadAt: m.Cutoff, Complete: false, Reason: "MANUAL_ACTION_SOURCE_UNAVAILABLE"})
			m.Limitations = unique(m.Limitations, "MANUAL_ACTION_SOURCE_UNAVAILABLE")
		}
	}
	if s.Facts == nil {
		m.Limitations = unique(m.Limitations, "MANUAL_ACTION_SOURCE_UNAVAILABLE")
		m.Sources = append(m.Sources, model.AnalysisSourceCoverage{Source: "alarm_lifecycle", Start: q.WarmupStart, End: q.End, ReadAt: m.Cutoff, Complete: false, Reason: "MANUAL_ACTION_SOURCE_UNAVAILABLE"})
	}
	if s.History != nil {
		m.Sources = append(m.Sources, model.AnalysisSourceCoverage{Source: "rule_evaluation_trace", Start: q.WarmupStart, End: q.End, ReadAt: time.Now().UnixMilli(), Version: "independent-history-read-cutoff", Complete: !slices.Contains(m.Limitations, "TRACE_ATTEMPT_READ_CAP_REACHED") && !slices.Contains(m.Limitations, "PRODUCTION_TRACE_SOURCE_UNAVAILABLE") && !slices.Contains(m.Limitations, "TRACE_HAS_UNAUTHORIZED_OR_UNAVAILABLE_REFERENCES"), Reason: "INDEPENDENT_SOURCE_READ_CUTOFF"})
	}
	completeTrace := len(m.Inputs) > 0
	knownInitial := q.InitialStatePolicy == "TRACE_INITIAL"
	for _, v := range m.Inputs {
		messageHash, _ := analytics.AnalysisHash(v.Message)
		exact := false
		for _, trace := range v.Traces {
			if trace.Status == "COMPLETE" && trace.ReproductionQuality == "EXACT" && trace.SemanticsVersion == q.SemanticsVersion && trace.MessageHash == messageHash && len(trace.Steps) == len(trace.Rules) && trace.RuleSetHash == model.RuleSetHash(trace.Rules) {
				exact = true
				for _, step := range trace.Steps {
					knownInitial = knownInitial && step.Before.InitialStateQuality == "KNOWN"
				}
			}
		}
		completeTrace = completeTrace && exact
	}
	if q.ClockPolicy == "RECORDED_TRACE" {
		if !completeTrace {
			m.Limitations = unique(m.Limitations, "COMPLETE_PRODUCTION_TRACE_OR_STAGE_CLOCKS_MISSING")
		} else if knownInitial {
			m.ReproductionQuality = "EXACT_TRACE_AVAILABLE"
			m.InitialStateQuality = "KNOWN_TRACE_INITIAL"
		}
	}
	knownInitial = knownInitial && completeTrace
	if !knownInitial {
		m.Limitations = unique(m.Limitations, "WINDOW_INITIAL_STATE_NOT_PROVED")
	}
	if len(m.Sources) == 0 {
		m.Sources = []model.AnalysisSourceCoverage{{Source: "rulelab_standard_inputs", Start: q.WarmupStart, End: q.End, Complete: false, ReadAt: m.Cutoff, Reason: "NO_SOURCE_COVERAGE"}}
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(body) > MaxDatasetBytes+(8<<20) {
		m.Limitations = unique(m.Limitations, "DATASET_FREEZE_BYTE_CAP_REACHED")
		for i := range m.Sources {
			m.Sources[i].Complete = false
			m.Sources[i].Reason += ";DATASET_FREEZE_BYTE_CAP_REACHED"
		}
		// Truncation is disclosed and frozen; no report claims complete input.
		for len(body) > MaxDatasetBytes+(8<<20) && len(m.ManualActions) > 0 {
			m.ManualActions = m.ManualActions[:len(m.ManualActions)/2]
			body, _ = json.Marshal(m)
		}
		for len(body) > MaxDatasetBytes+(8<<20) && len(m.Inputs) > 0 {
			m.Inputs = m.Inputs[:len(m.Inputs)-1]
			body, _ = json.Marshal(m)
		}
	}
	hash, err := analytics.AnalysisHash(m)
	if err != nil {
		return err
	}
	err = e.Commit(ctx, model.AnalysisBatch{ID: "dataset-freeze", FreezeInputs: true, InputHashes: []string{hash}, Sources: m.Sources, DataCutoff: m.Cutoff, Processed: int64(len(m.Inputs)), Status: model.AnalysisRunning, Stage: "dataset-inputs-frozen", Outputs: []model.AnalysisOutput{{ID: e.Run.ID + ":dataset-chunk:0", Kind: "dataset-chunk", Body: body}}})
	if err != nil {
		return err
	}
	return s.finishDataset(ctx, e, m, false)
}
func (s *Service) loadDatasetChunk(ctx context.Context, r model.AnalysisRun) (datasetChunk, error) {
	page, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "dataset-chunk", Limit: 10})
	if err != nil {
		return datasetChunk{}, err
	}
	if len(page) != 1 {
		return datasetChunk{}, model.ErrNotFound
	}
	var m datasetChunk
	if err = json.Unmarshal(page[0].Body, &m); err != nil {
		return m, err
	}
	hash, err := analytics.AnalysisHash(m)
	if err != nil || len(r.InputHashes) != 1 || hash != r.InputHashes[0] {
		return m, model.ErrAnalysisConflict
	}
	return m, nil
}
func (s *Service) finishDataset(ctx context.Context, e *analytics.Execution, m datasetChunk, timedOut bool) error {
	current, err := s.Analysis.Store.GetAnalysisRun(ctx, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	status := model.AnalysisSucceeded
	missing := []string{}
	affected := []model.AnalysisSourceCoverage{}
	for _, v := range m.Sources {
		if !v.Complete {
			status = model.AnalysisPartial
			missing = append(missing, v.Source)
			affected = append(affected, v)
		}
	}
	for _, reason := range m.Limitations {
		if strings.Contains(reason, "CAP_REACHED") || strings.Contains(reason, "SOURCE_") || strings.Contains(reason, "MISSING") {
			status = model.AnalysisPartial
		}
	}
	if timedOut {
		status = model.AnalysisPartial
		m.Limitations = unique(m.Limitations, "RUN_TIME_BUDGET_EXHAUSTED")
	}
	warmupCount, mainCount := 0, 0
	for _, v := range m.Inputs {
		at := v.Message.Timestamp
		if m.Selection.TimeBasis == "RECEIVED" {
			at = v.ReceivedAt
		}
		if at < m.Selection.Start {
			warmupCount++
		} else {
			mainCount++
		}
	}
	stats, _ := json.Marshal(map[string]any{"inputCount": len(m.Inputs), "warmupInputs": warmupCount, "mainInputs": mainCount, "inputBytesLimit": MaxDatasetBytes, "recordLimit": s.recordLimit(), "reproductionQuality": m.ReproductionQuality, "initialStateQuality": m.InitialStateQuality, "sourceCoverage": m.Sources, "limitations": m.Limitations})
	snapshot := model.AnalysisSnapshot{ID: current.ID + ":snapshot", DataCutoff: m.Cutoff, InputHashes: current.InputHashes, Sources: m.Sources, Statistics: stats, InitialStateQuality: m.InitialStateQuality, Limitations: m.Limitations, MissingSources: missing, AffectedIntervals: affected}
	if timedOut {
		snapshot.UncomputableMetrics = []string{"预算内未完成的数据集成员"}
	}
	return e.Commit(ctx, model.AnalysisBatch{ID: fmt.Sprintf("dataset-final:%t", timedOut), Processed: current.Processed, Status: status, Stage: "dataset-complete", Snapshot: &snapshot})
}
func (s *Service) DatasetInputs(ctx context.Context, a analytics.Actor, id string, f model.AnalysisFilter) ([]model.RuleLabInput, int, error) {
	d, err := s.Dataset(ctx, a, id)
	if err != nil {
		return nil, 0, err
	}
	r, err := s.Analysis.Get(ctx, a, analytics.KindRuleLab, d.RunID)
	if err != nil {
		return nil, 0, err
	}
	if !r.InputsFrozen {
		return []model.RuleLabInput{}, 0, nil
	}
	m, err := s.loadDatasetChunk(ctx, r)
	if err != nil {
		return nil, 0, err
	}
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	total := len(m.Inputs)
	if offset >= total {
		return []model.RuleLabInput{}, total, nil
	}
	return m.Inputs[offset:min(total, offset+limit)], total, nil
}

func (s *Service) pageTraces(ctx context.Context, r model.AnalysisRun, q model.RuleLabDatasetRequest, items []model.RuleLabInput, count *int, limitations *[]string) (map[string][]model.RuleEvaluationTrace, error) {
	result := map[string][]model.RuleEvaluationTrace{}
	if s.History == nil || len(items) == 0 {
		return result, nil
	}
	ids := make([]string, len(items))
	devices := map[string]string{}
	for i, v := range items {
		ids[i] = v.ID
		devices[v.ID] = v.Message.DeviceID
	}
	for offset := 0; ; {
		traces, total, err := s.History.ListRuleEvaluationTraces(ctx, r.TenantID, model.RuleTraceFilter{DeviceIDs: r.DeviceIDs, MessageIDs: ids, Limit: 1000, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, trace := range traces {
			if trace.TenantID != r.TenantID || devices[trace.MessageID] != trace.DeviceID {
				return nil, analytics.ErrForbidden
			}
			if *count >= s.recordLimit() {
				*limitations = unique(*limitations, "TRACE_ATTEMPT_READ_CAP_REACHED")
				return result, nil
			}
			*count++
			if !s.authorizedTrace(ctx, r, trace) {
				*limitations = unique(*limitations, "TRACE_HAS_UNAUTHORIZED_OR_UNAVAILABLE_REFERENCES")
				continue
			}
			trace = projectTrace(trace)
			result[trace.MessageID] = append(result[trace.MessageID], trace)
		}
		offset += len(traces)
		if offset >= total {
			return result, nil
		}
		if len(traces) == 0 {
			return nil, invalid("trace分页没有推进")
		}
	}
}

func (s *Service) authorizedTrace(ctx context.Context, r model.AnalysisRun, t model.RuleEvaluationTrace) bool {
	if s.ValidateCandidate == nil {
		return false
	}
	a := analytics.Actor{TenantID: r.TenantID, Username: r.Creator, Managed: r.CreatorManaged, SessionVersion: r.CreatorSessionVersion, AccessVersion: r.PermissionsVersion}
	for _, v := range t.Rules {
		candidate := v.Rule
		candidate.Enabled = false
		if v.TenantID != r.TenantID || candidate.TenantID != r.TenantID || v.Hash != model.RuleBodyHash(v.Rule) || s.ValidateCandidate(ctx, a, candidate) != nil {
			return false
		}
	}
	for _, step := range t.Steps {
		if ref := step.Before.RecoveryRevision; ref != nil {
			candidate := ref.Rule
			candidate.Enabled = false
			if ref.TenantID != r.TenantID || candidate.TenantID != r.TenantID || ref.Hash != model.RuleBodyHash(ref.Rule) || s.ValidateCandidate(ctx, a, candidate) != nil {
				return false
			}
		}
	}
	return true
}
func projectTrace(t model.RuleEvaluationTrace) model.RuleEvaluationTrace {
	for i := range t.Rules {
		t.Rules[i].Actor = ""
		t.Rules[i].Reason = ""
		t.Rules[i].ExperimentID = ""
	}
	for i := range t.Steps {
		v := &t.Steps[i]
		v.Before.Alarm = cloneAlarm(v.Before.Alarm)
		v.Alarm = cloneAlarm(v.Alarm)
		if v.Before.RecoveryRevision != nil {
			v.Before.RecoveryRevision.Actor = ""
			v.Before.RecoveryRevision.Reason = ""
			v.Before.RecoveryRevision.ExperimentID = ""
		}
	}
	for i := range t.RoutingSteps {
		v := &t.RoutingSteps[i]
		v.Before.Alarm = cloneAlarm(v.Before.Alarm)
		v.After.Alarm = cloneAlarm(v.After.Alarm)
	}
	for _, snapshot := range []*model.RuleRoutingSnapshot{&t.Routing.Initial, &t.Routing.Final} {
		for key, v := range snapshot.Direct {
			v.Alarm = cloneAlarm(v.Alarm)
			snapshot.Direct[key] = v
		}
		for key, v := range snapshot.Components {
			v.Lifecycle.Alarm = cloneAlarm(v.Lifecycle.Alarm)
			snapshot.Components[key] = v
		}
	}
	return t
}
