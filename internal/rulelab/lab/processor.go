package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

type comparisonStage struct {
	Outputs     []model.AnalysisOutput   `json:"outputs"`
	Evidence    []model.AnalysisEvidence `json:"evidence"`
	Statistics  json.RawMessage          `json:"statistics"`
	Limitations []string                 `json:"limitations"`
	Partial     bool                     `json:"partial"`
	Processed   int64                    `json:"processed"`
}

func (s *Service) processExperiment(ctx context.Context, e *analytics.Execution, p model.RuleLabRunParameters) error {
	fixed, b, m, err := s.freezeExperiment(ctx, e, p)
	if err != nil {
		return err
	}
	current, err := s.Analysis.Store.GetAnalysisRun(ctx, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	cp := comparisonCheckpoint{Branches: map[string]*branchState{"BASELINE": newBranch("BASELINE"), "CANDIDATE": newBranch("CANDIDATE")}, Limits: []string{}}
	if len(current.Checkpoint) > 0 {
		if err = json.Unmarshal(current.Checkpoint, &cp); err != nil {
			return err
		}
	}
	if !cp.DocumentsFrozen {
		items := work(m)
		upper := min(len(items), s.recordLimit())
		if len(items) > upper {
			cp.Limits = unique(cp.Limits, "TRACE_WORK_RECORD_CAP_REACHED")
		}
		batchSize := min(100, s.Analysis.Limits.BatchSize)
		for cp.Index < upper {
			if err = ctx.Err(); err != nil {
				return err
			}
			end := min(upper, cp.Index+batchSize)
			for cp.Index < end {
				item := items[cp.Index]
				for _, name := range []string{"BASELINE", "CANDIDATE"} {
					if err = cp.Branches[name].apply(e.Run, m, fixed, b, item); err != nil {
						cp.Limits = unique(cp.Limits, "BRANCH_STATE_CAP_REACHED")
						upper = cp.Index
						break
					}
				}
				if cp.Index == upper {
					break
				}
				cp.Index++
			}
			checkpoint, _ := json.Marshal(cp)
			if err = e.Commit(ctx, model.AnalysisBatch{ID: fmt.Sprintf("comparison-step:%d", cp.Index), Checkpoint: checkpoint, Processed: int64(cp.Index), Status: model.AnalysisRunning, Stage: "comparing-independent-branches"}); err != nil {
				return err
			}
		}
		stage := s.documents(e.Run, m, fixed, b, cp, len(items))
		body, err := json.Marshal(stage)
		if err != nil {
			return err
		}
		cp = comparisonCheckpoint{Index: 0, DocumentsFrozen: true, Limits: cp.Limits}
		checkpoint, _ := json.Marshal(cp)
		if err = e.Commit(ctx, model.AnalysisBatch{ID: "comparison-documents-freeze", Checkpoint: checkpoint, Processed: stage.Processed, Status: model.AnalysisRunning, Stage: "comparison-documents-frozen", Outputs: []model.AnalysisOutput{{ID: e.Run.ID + ":comparison-stage", Kind: "comparison-stage", Body: body}}}); err != nil {
			return err
		}
	}
	return s.finishComparison(ctx, e, m, cp)
}
func (s *Service) documents(run model.AnalysisRun, m datasetChunk, f frozenExperiment, b model.RuleLabExperiment, cp comparisonCheckpoint, totalWork int) comparisonStage {
	stage := comparisonStage{Outputs: []model.AnalysisOutput{}, Evidence: []model.AnalysisEvidence{}, Limitations: slices.Clone(m.Limitations), Processed: int64(cp.Index)}
	for _, reason := range append(cp.Limits, verifyTraceClockSelection(m, b)...) {
		stage.Limitations = unique(stage.Limitations, reason)
	}
	complete := true
	for _, source := range m.Sources {
		complete = complete && source.Complete
	}
	calculated := cp.Index == totalWork
	initialKnown := m.InitialStateQuality == "KNOWN_TRACE_INITIAL"
	for _, branch := range cp.Branches {
		for _, reason := range branch.Metrics.Limitations {
			if strings.HasPrefix(reason, "TRACE_") {
				m.ReproductionQuality = "HISTORICAL_SIMULATION_TRACE_COMMIT_DIFFERENCE"
				initialKnown = false
				stage.Partial = true
			}
		}
	}
	if !complete || !calculated {
		stage.Partial = true
	}
	if len(m.ManualActions) > 0 {
		stage.Limitations = unique(stage.Limitations, "UNMAPPED_MANUAL_ACTIONS_NOT_APPLIED_TO_COUNTERFACTUAL_ALARMS")
	}
	window := model.FactRange{Start: run.Start, End: run.End}
	values := map[string][]model.RuleLabOutcome{}
	metrics := []map[string]any{}
	references := map[string]bool{}
	for _, name := range []string{"BASELINE", "CANDIDATE"} {
		branch := cp.Branches[name]
		branch.finish(window)
		values[name] = branch.cycles(window)
		for _, limitation := range branch.Metrics.Limitations {
			stage.Limitations = unique(stage.Limitations, limitation)
		}
		if branch.Metrics.UncomputedMessages > 0 || branch.Metrics.EvaluationErrors > 0 {
			stage.Partial = true
		}
		labels := evaluateLabels(run, f, b.EvaluationPolicy, values[name], complete && calculated && initialKnown && branch.Metrics.UncomputedMessages == 0 && branch.Metrics.EvaluationErrors == 0 && len(m.ManualActions) == 0)
		for _, reason := range labels.Limitations {
			stage.Limitations = unique(stage.Limitations, reason)
			if strings.Contains(reason, "CAP_REACHED") {
				stage.Partial = true
			}
		}
		metric := map[string]any{"branch": name, "counters": branch.Metrics, "labelEvaluation": labels, "sourceInputCount": len(m.Inputs), "processedWorkItems": cp.Index, "totalWorkItems": totalWork, "timeBasis": m.Selection.TimeBasis, "clockPolicy": m.Selection.ClockPolicy, "semanticsVersion": m.Selection.SemanticsVersion, "reproductionQuality": m.ReproductionQuality, "initialStateQuality": m.InitialStateQuality}
		metrics = append(metrics, metric)
		body, _ := json.Marshal(metric)
		stage.Outputs = append(stage.Outputs, model.AnalysisOutput{ID: run.ID + ":metrics:" + name, Kind: "metrics", Body: body})
		for _, v := range values[name] {
			for _, id := range v.EvidenceIDs {
				references[id] = true
			}
			body, _ := json.Marshal(v)
			stage.Outputs = append(stage.Outputs, model.AnalysisOutput{ID: v.ID, Kind: "outcomes", DeviceID: v.DeviceID, Body: body})
		}
	}
	left, right := outcomePoints(values["BASELINE"], b.EvaluationPolicy.MatchTimeBasis), outcomePoints(values["CANDIDATE"], b.EvaluationPolicy.MatchTimeBasis)
	pairs, err := maximumMatch(left, right, b.EvaluationPolicy.ToleranceMs)
	matchedLeft, matchedRight := map[int]bool{}, map[int]bool{}
	if err != nil {
		stage.Partial = true
		stage.Limitations = unique(stage.Limitations, err.Error())
	} else {
		for _, pair := range pairs {
			matchedLeft[pair.Left] = true
			matchedRight[pair.Right] = true
			diff := model.RuleLabEventMatch{ID: run.ID + ":diff:" + left[pair.Left].ID + ":" + right[pair.Right].ID, BaselineID: left[pair.Left].ID, CandidateID: right[pair.Right].ID, DifferenceMs: -pair.Difference, Kind: "COMMON"}
			body, _ := json.Marshal(diff)
			stage.Outputs = append(stage.Outputs, model.AnalysisOutput{ID: diff.ID, Kind: "diffs", DeviceID: left[pair.Left].Device, Body: body})
		}
		for i, v := range left {
			if !matchedLeft[i] {
				diff := model.RuleLabEventMatch{ID: run.ID + ":diff:" + v.ID, BaselineID: v.ID, Kind: "BASELINE_ONLY"}
				body, _ := json.Marshal(diff)
				stage.Outputs = append(stage.Outputs, model.AnalysisOutput{ID: diff.ID, Kind: "diffs", DeviceID: v.Device, Body: body})
			}
		}
		for i, v := range right {
			if !matchedRight[i] {
				diff := model.RuleLabEventMatch{ID: run.ID + ":diff:" + v.ID, CandidateID: v.ID, Kind: "CANDIDATE_ONLY"}
				body, _ := json.Marshal(diff)
				stage.Outputs = append(stage.Outputs, model.AnalysisOutput{ID: diff.ID, Kind: "diffs", DeviceID: v.Device, Body: body})
			}
		}
	}
	for _, v := range f.Labels {
		stage.Outputs = append(stage.Outputs, model.AnalysisOutput{ID: run.ID + ":label:" + v.ID, Kind: "labels", DeviceID: v.DeviceIDs[0], Body: v.Body})
	}
	for i, reason := range stage.Limitations {
		stage.Outputs = append(stage.Outputs, stepFinding(run, fmt.Sprintf("limitation:%d", i), "", "INSUFFICIENT_INPUT", reason, map[string]any{"sourceCoverage": m.Sources}))
	}
	if err == nil && (len(left) != len(right) || len(pairs) != len(left)) {
		stage.Outputs = append(stage.Outputs, stepFinding(run, "behavior-difference", "", "BEHAVIOR_DIFFERENCE", "共同及独有事件只表示固定模拟行为差异", map[string]any{"common": len(pairs), "baselineOnly": len(left) - len(pairs), "candidateOnly": len(right) - len(pairs)}))
	}
	originalOutputs := len(stage.Outputs)
	if originalOutputs > s.recordLimit() {
		stage.Outputs = stage.Outputs[:s.recordLimit()]
		stage.Partial = true
		stage.Limitations = unique(stage.Limitations, "PUBLIC_OUTPUT_RECORD_CAP_REACHED")
	}
	for _, v := range m.Inputs {
		id := run.ID + ":evidence:" + v.Hash
		if !references[id] {
			continue
		}
		if len(stage.Evidence) >= s.recordLimit() {
			stage.Partial = true
			stage.Limitations = unique(stage.Limitations, "EVIDENCE_RECORD_CAP_REACHED")
			break
		}
		body, _ := json.Marshal(map[string]any{"messageId": v.ID, "messageType": v.Message.MessageType, "eventAt": v.Message.Timestamp, "receivedAt": v.ReceivedAt, "availableAt": v.AvailableAt, "availableAtSource": v.AvailableAtSource, "properties": v.Message.Properties, "protocolVersion": v.ProtocolVersion, "configurationVersion": v.ConfigurationVersion, "pointTableVersion": v.PointTableVersion, "units": v.Units, "metadataQuality": v.MetadataQuality, "frozenInputHash": v.Hash})
		stage.Evidence = append(stage.Evidence, model.AnalysisEvidence{ID: id, SourceKind: "rule-lab-frozen-input", SourceID: v.ID, DeviceID: v.Message.DeviceID, OccurredAt: v.Message.Timestamp, ResourceVersion: v.Hash, Summary: body, PermissionCategory: "rule-lab-evidence", OriginalAvailability: "FROZEN_DATASET", RawMessageID: v.Message.RawMessageID})
	}
	if len(cp.Limits) > 0 {
		stage.Partial = true
	}
	var commonCount, baselineOnly, candidateOnly any
	matchingStatus := "NOT_COMPUTED"
	if err == nil {
		matchingStatus = "COMPUTED"
		commonCount = len(pairs)
		baselineOnly = len(left) - len(pairs)
		candidateOnly = len(right) - len(pairs)
	}
	stage.Statistics, _ = json.Marshal(map[string]any{"branches": metrics, "sourceInputCount": len(m.Inputs), "processedWorkItems": cp.Index, "totalWorkItems": totalWork, "originalOutputCount": originalOutputs, "representedOutputCount": len(stage.Outputs), "evidenceCount": len(stage.Evidence), "behaviorMatchingStatus": matchingStatus, "commonEvents": commonCount, "baselineOnly": baselineOnly, "candidateOnly": candidateOnly, "sourceCoverage": m.Sources, "initialStateQuality": m.InitialStateQuality, "reproductionQuality": m.ReproductionQuality, "holdoutPreviousUses": f.HoldoutPreviousUses, "limitations": stage.Limitations})
	return stage
}
func (s *Service) finishComparison(ctx context.Context, e *analytics.Execution, m datasetChunk, cp comparisonCheckpoint) error {
	page, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, e.Run.TenantID, model.AnalysisFilter{RunID: e.Run.ID, Kind: "comparison-stage", Limit: 1})
	if err != nil {
		return err
	}
	if len(page) != 1 {
		return model.ErrNotFound
	}
	var stage comparisonStage
	if err = json.Unmarshal(page[0].Body, &stage); err != nil {
		return err
	}
	total := len(stage.Outputs) + len(stage.Evidence)
	for cp.Index < total {
		end := min(total, cp.Index+s.Analysis.Limits.BatchSize)
		outputs := []model.AnalysisOutput{}
		evidence := []model.AnalysisEvidence{}
		for i := cp.Index; i < end; i++ {
			if i < len(stage.Outputs) {
				outputs = append(outputs, stage.Outputs[i])
			} else {
				evidence = append(evidence, stage.Evidence[i-len(stage.Outputs)])
			}
		}
		cp.Index = end
		checkpoint, _ := json.Marshal(cp)
		if err = e.Commit(ctx, model.AnalysisBatch{ID: fmt.Sprintf("comparison-output:%d", end), Checkpoint: checkpoint, Processed: stage.Processed, Status: model.AnalysisRunning, Stage: "saving-comparison", Outputs: outputs, Evidence: evidence}); err != nil {
			return err
		}
	}
	current, err := s.Analysis.Store.GetAnalysisRun(ctx, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	missing := []string{}
	affected := []model.AnalysisSourceCoverage{}
	for _, v := range m.Sources {
		if !v.Complete {
			missing = append(missing, v.Source)
			affected = append(affected, v)
		}
	}
	snapshot := model.AnalysisSnapshot{ID: current.ID + ":snapshot", InputHashes: current.InputHashes, DataCutoff: m.Cutoff, Sources: m.Sources, InitialStateQuality: m.InitialStateQuality, Statistics: stage.Statistics, Limitations: stage.Limitations, MissingSources: missing, AffectedIntervals: affected}
	status := model.AnalysisSucceeded
	if stage.Partial {
		status = model.AnalysisPartial
		snapshot.UncomputableMetrics = []string{"资源保护、输入覆盖或求值错误影响的周期与标签指标"}
	}
	return e.Commit(ctx, model.AnalysisBatch{ID: "comparison-complete", Processed: stage.Processed, Status: status, Stage: "comparison-complete", Snapshot: &snapshot})
}
func (s *Service) timeout(ctx context.Context, e *analytics.Execution) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	current, err := s.Analysis.Store.GetAnalysisRun(cleanup, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	var p model.RuleLabRunParameters
	if err = decode(current.Parameters, &p); err != nil {
		return err
	}
	if p.Phase == "DATASET" {
		if current.InputsFrozen {
			m, err := s.loadDatasetChunk(cleanup, current)
			if err != nil {
				return err
			}
			return s.finishDataset(cleanup, e, m, true)
		}
		snapshot := model.AnalysisSnapshot{ID: current.ID + ":snapshot", Statistics: json.RawMessage(`{"inputCount":0,"incomplete":true}`), InitialStateQuality: "UNKNOWN", Limitations: []string{"RUN_TIME_BUDGET_EXHAUSTED_BEFORE_DATASET_FREEZE"}, UncomputableMetrics: []string{"数据集成员"}}
		return e.Commit(cleanup, model.AnalysisBatch{ID: "dataset-timeout", Processed: current.Processed, Status: model.AnalysisPartial, Stage: "dataset-timeout", Snapshot: &snapshot})
	}
	snapshot := model.AnalysisSnapshot{ID: current.ID + ":snapshot", DataCutoff: current.DataCutoff, InputHashes: current.InputHashes, Sources: current.Sources, InitialStateQuality: "UNKNOWN", Statistics: json.RawMessage(fmt.Sprintf(`{"processedWorkItems":%d,"incomplete":true}`, current.Processed)), Limitations: []string{"RUN_TIME_BUDGET_EXHAUSTED"}, UncomputableMetrics: []string{"未完成的实验周期与标签指标"}}
	return e.Commit(cleanup, model.AnalysisBatch{ID: "comparison-timeout", Processed: current.Processed, Status: model.AnalysisPartial, Stage: "comparison-timeout", Snapshot: &snapshot})
}
