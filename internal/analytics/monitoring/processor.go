package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/model"
)

func (s *Service) Process(ctx context.Context, e *analytics.Execution) (resultErr error) {
	defer func() {
		if errors.Is(resultErr, context.DeadlineExceeded) {
			if err := s.timeout(ctx, e); err == nil {
				resultErr = nil
			}
		}
	}()
	configs, quality, p, err := s.runConfigs(ctx, e.Run)
	if err != nil {
		return err
	}
	var m manifest
	if e.Run.InputsFrozen {
		m, err = s.loadManifest(ctx, e.Run)
	} else {
		m, err = s.freeze(ctx, e, configs, quality, p)
	}
	if err != nil {
		return err
	}
	var cp checkpoint
	_ = json.Unmarshal(e.Run.Checkpoint, &cp)
	cp.ManifestID = e.Run.ID + ":input-manifest"
	cp.Phase = "calculating"
	for deviceIndex, device := range e.Run.DeviceIDs {
		if deviceIndex < cp.NextDevice {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		outputs, evidence, staged, err := s.loadStage(ctx, e.Run, "device:"+device, device)
		if err != nil {
			return err
		}
		if !staged {
			members := []member{}
			for _, v := range m.Members {
				if v.DeviceID == device {
					members = append(members, v)
				}
			}
			measurements, issues, err := s.readMembers(ctx, e.Run, members)
			if err != nil {
				return err
			}
			in := continuity.DeviceInput{DeviceID: device, Window: continuity.Range{Start: e.Run.Start, End: e.Run.End}, Measurements: measurements, ReceivedCoverage: m.ReceivedCoverage, AvailableCoverage: m.AvailableCoverage, MaximumIntervals: MaxIntervals}
			if len(issues) > 0 {
				in.ReceivedCoverage = nil
				in.AvailableCoverage = nil
			}
			for _, v := range m.Connections {
				if v.DeviceID == device {
					in.Connection = append(in.Connection, v)
				}
			}
			profiles := []map[string]any{}
			for _, v := range configs {
				if !slices.Contains(v.DeviceIDs, device) {
					continue
				}
				if v.Kind == model.MonitoringProfileKind {
					pr, b, err := profile(v)
					if err != nil {
						return err
					}
					in.Profiles = append(in.Profiles, pr)
					profiles = append(profiles, map[string]any{"revisionId": v.ID, "importance": b.Importance, "merge": b.Merge, "attributes": b.Attributes, "mode": b.Mode})
				} else {
					var b model.MonitoringObservation
					if err = decode(v.Body, &b); err != nil {
						return err
					}
					if b.DeviceID == device && b.Status == "CONFIRMED" && (b.Type == "STOPPED" || b.Type == "MAINTENANCE") {
						in.Observations = append(in.Observations, continuity.Observation{ID: v.ID, Range: continuity.Range{Start: b.Start, End: b.End}, Reason: b.Reason, Basis: b.Basis, ConfirmedBy: b.ConfirmedBy})
					}
				}
			}
			result, err := continuity.Analyze(in)
			if err != nil {
				if strings.Contains(err.Error(), "resource limit") {
					// Resource exhaustion cannot turn a cropped series into a known
					// gap. Preserve observations and produce explicit unknown tracks.
					in.Measurements = nil
					in.Connection = nil
					in.ReceivedCoverage = nil
					in.AvailableCoverage = nil
					result, err = continuity.Analyze(in)
					issues = unique(issues, "CALCULATION_INTERVAL_CAP_REACHED")
				}
				if err != nil {
					return invalid(err.Error())
				}
			}
			for _, issue := range issues {
				result.Limitations = unique(result.Limitations, issue)
			}
			if counts := m.ParseByDevice[device]; counts.LastFailed > 0 {
				result.Findings = append(result.Findings, continuity.Finding{ID: device + ":parse-failures", DeviceID: device, Kind: "RECEIVED_RAW_PARSE_FAILED", Range: in.Window, Explanation: "已归档原文存在最后解析失败记录；连接与有效数据分别核实，历史尝试次数未知。", Values: map[string]any{"rawLastFailed": counts.LastFailed, "observedRaw": counts.ObservedRaw}})
			}
			_, represented, err := s.Analysis.Store.ListAnalysisOutputs(ctx, e.Run.TenantID, model.AnalysisFilter{RunID: e.Run.ID, Kind: "intervals", Limit: 1})
			if err != nil {
				return err
			}
			outputs, evidence = s.deviceDocuments(e.Run, m, result, profiles, max(0, s.recordLimit()-represented))
			if err = s.saveStage(ctx, e, "device:"+device, device, outputs, evidence, cp); err != nil {
				return err
			}
		}
		for offset := 0; offset < len(outputs)+len(evidence); {
			b := model.AnalysisBatch{ID: fmt.Sprintf("device/%d/%d", deviceIndex, offset), Processed: int64(len(m.Members)), Status: model.AnalysisRunning, Stage: fmt.Sprintf("calculating:%d", deviceIndex+1)}
			remaining := s.Analysis.Limits.BatchSize
			for offset < len(outputs) && remaining > 0 {
				b.Outputs = append(b.Outputs, outputs[offset])
				offset++
				remaining--
			}
			for offset >= len(outputs) && offset < len(outputs)+len(evidence) && remaining > 0 {
				b.Evidence = append(b.Evidence, evidence[offset-len(outputs)])
				offset++
				remaining--
			}
			next := cp
			if offset == len(outputs)+len(evidence) {
				next.NextDevice = deviceIndex + 1
			}
			b.Checkpoint, _ = json.Marshal(next)
			if err = e.Commit(ctx, b); err != nil {
				return err
			}
		}
		cp.NextDevice = deviceIndex + 1
	}
	if err = s.groupDocuments(ctx, e, m, &cp); err != nil {
		return err
	}
	return s.finish(ctx, e, m, cp, false)
}
func evidenceID(run, id string) string {
	hash, _ := analytics.AnalysisHash(id)
	return run + ":evidence:" + hash
}
func intervalID(run, id string) string { return run + ":interval:" + id }

type documentStage struct {
	Outputs  []model.AnalysisOutput   `json:"outputs"`
	Evidence []model.AnalysisEvidence `json:"evidence"`
}

func (s *Service) loadStage(ctx context.Context, r model.AnalysisRun, key, device string) ([]model.AnalysisOutput, []model.AnalysisEvidence, bool, error) {
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "calculation-stage", DeviceID: device, Limit: 100, Offset: offset})
		if err != nil {
			return nil, nil, false, err
		}
		for _, v := range page {
			if v.ID == r.ID+":stage:"+key {
				var staged documentStage
				if err = json.Unmarshal(v.Body, &staged); err != nil {
					return nil, nil, false, err
				}
				return staged.Outputs, staged.Evidence, true, nil
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
	}
	return nil, nil, false, nil
}
func (s *Service) saveStage(ctx context.Context, e *analytics.Execution, key, device string, outputs []model.AnalysisOutput, evidence []model.AnalysisEvidence, cp checkpoint) error {
	body, err := json.Marshal(documentStage{outputs, evidence})
	if err != nil {
		return err
	}
	checkpoint, _ := json.Marshal(cp)
	current, err := s.Analysis.Store.GetAnalysisRun(ctx, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	return e.Commit(ctx, model.AnalysisBatch{ID: "stage/" + key, Processed: current.Processed, Status: model.AnalysisRunning, Stage: "derived-result-frozen", Checkpoint: checkpoint, Outputs: []model.AnalysisOutput{{ID: e.Run.ID + ":stage:" + key, Kind: "calculation-stage", DeviceID: device, Body: body}}})
}
func (s *Service) deviceDocuments(run model.AnalysisRun, m manifest, result continuity.DeviceResult, profiles []map[string]any, remainingIntervals int) ([]model.AnalysisOutput, []model.AnalysisEvidence) {
	outputs := []model.AnalysisOutput{}
	evidence := []model.AnalysisEvidence{}
	intervals := map[string]continuity.Interval{}
	members := map[string]model.MeasurementFact{}
	for _, v := range m.Members {
		members[v.Metadata.ID] = v.Metadata
	}
	for _, v := range result.Intervals {
		intervals[v.ID] = v
	}
	needs := map[string]bool{}
	findings := result.Findings
	if len(findings) > 200 {
		findings = findings[:200]
		result.Limitations = unique(result.Limitations, "FINDING_REPRESENTATION_CAP_REACHED")
	}
	for _, f := range findings {
		for _, id := range f.EvidenceIDs {
			needs[id] = true
		}
	}
	// Only representative original samples and intervals used by findings are
	// evidence documents. Full intervals remain paginated derived output, while
	// source values stay in PostgreSQL/ClickHouse.
	shown := min(len(result.Intervals), 10000, remainingIntervals)
	if shown < len(result.Intervals) {
		result.Limitations = unique(result.Limitations, "INTERVAL_REPRESENTATION_CAP_REACHED")
	}
	sampleEvidence := map[string]bool{}
	for _, v := range result.Intervals[:shown] {
		public := v
		public.ID = intervalID(run.ID, v.ID)
		public.EvidenceIDs = []string{}
		for _, id := range v.EvidenceIDs {
			if metadata, ok := members[id]; ok && (sampleEvidence[id] || len(sampleEvidence) < 20) {
				public.EvidenceIDs = append(public.EvidenceIDs, evidenceID(run.ID, id))
				if !sampleEvidence[id] {
					sampleEvidence[id] = true
					summary, _ := json.Marshal(map[string]any{"measurement": metadata, "valueCopied": false, "coverage": m.Sources})
					evidence = append(evidence, model.AnalysisEvidence{ID: evidenceID(run.ID, id), SourceKind: "standard_measurement", SourceID: metadata.MessageID, DeviceID: result.DeviceID, OccurredAt: metadata.EventAt, ResourceVersion: metadata.ProtocolVersion + ":" + metadata.ConfigurationVersion, Summary: summary, PermissionCategory: "monitoring-evidence", OriginalAvailability: "AVAILABLE_AT_FREEZE", RawMessageID: metadata.RawMessageID})
				}
			}
		}
		if needs[v.ID] {
			public.EvidenceIDs = append(public.EvidenceIDs, evidenceID(run.ID, v.ID))
		}
		body, _ := json.Marshal(public)
		outputs = append(outputs, model.AnalysisOutput{ID: public.ID, Kind: "intervals", DeviceID: result.DeviceID, Body: body})
	}
	for id := range needs {
		if v, ok := intervals[id]; ok {
			summary, _ := json.Marshal(map[string]any{"interval": v, "sourceCoverage": m.Sources, "inputFrozen": true})
			evidence = append(evidence, model.AnalysisEvidence{ID: evidenceID(run.ID, id), SourceKind: "monitoring_interval", SourceID: intervalID(run.ID, id), DeviceID: result.DeviceID, OccurredAt: v.Start, ResourceVersion: run.ConfigurationVersion, Summary: summary, PermissionCategory: "monitoring-evidence", OriginalAvailability: "FIXED_DERIVED_FACT"})
		}
	}
	for _, f := range findings {
		f.ID = run.ID + ":finding:" + f.ID
		for i, id := range f.EvidenceIDs {
			f.EvidenceIDs[i] = evidenceID(run.ID, id)
		}
		var body map[string]any
		raw, _ := json.Marshal(f)
		_ = json.Unmarshal(raw, &body)
		body["manualState"] = "UNREVIEWED"
		body["observedSource"] = "monitoring-continuity"
		data, _ := json.Marshal(body)
		outputs = append(outputs, model.AnalysisOutput{ID: f.ID, Kind: "findings", DeviceID: result.DeviceID, Body: data})
	}
	for _, raw := range m.ParseEvidence {
		if raw.DeviceID != result.DeviceID {
			continue
		}
		summary, _ := json.Marshal(raw)
		evidence = append(evidence, model.AnalysisEvidence{ID: evidenceID(run.ID, "raw:"+raw.RawMessageID), SourceKind: "raw_parse_outcome", SourceID: raw.RawMessageID, DeviceID: raw.DeviceID, OccurredAt: raw.ReceivedAt, ResourceVersion: raw.ProtocolVersion, Summary: summary, PermissionCategory: "monitoring-evidence", OriginalAvailability: raw.EvidenceAvailability, RawMessageID: raw.RawMessageID})
	}
	metrics, _ := json.Marshal(map[string]any{"deviceId": result.DeviceID, "metrics": result.Metrics, "intervalCount": len(result.Intervals), "representedIntervalCount": shown, "originalFindingCount": len(result.Findings), "representedFindingCount": len(findings), "limitations": result.Limitations, "profiles": profiles})
	outputs = append(outputs, model.AnalysisOutput{ID: run.ID + ":metrics:" + result.DeviceID, Kind: "metrics", DeviceID: result.DeviceID, Body: metrics})
	gapInput := continuity.DeviceResult{DeviceID: result.DeviceID, Intervals: []continuity.Interval{}}
	for _, v := range result.Intervals {
		if v.Track == "data" && v.AttributeID == "" && v.State == continuity.Unavailable {
			gapInput.Intervals = append(gapInput.Intervals, v)
		}
	}
	if len(gapInput.Intervals) > s.recordLimit() {
		gapInput.Intervals = nil
		gapInput.Limitations = []string{"COMMON_GAP_INPUT_CAP_REACHED"}
	}
	gapBody, _ := json.Marshal(gapInput)
	outputs = append(outputs, model.AnalysisOutput{ID: run.ID + ":common-gap-input:" + result.DeviceID, Kind: "common-gap-input", DeviceID: result.DeviceID, Body: gapBody})
	slices.SortFunc(evidence, func(a, b model.AnalysisEvidence) int { return strings.Compare(a.ID, b.ID) })
	return outputs, evidence
}
func (s *Service) groupDocuments(ctx context.Context, e *analytics.Execution, m manifest, cp *checkpoint) error {
	outputs, evidence, staged, err := s.loadStage(ctx, e.Run, "groups", "")
	if err != nil {
		return err
	}
	if !staged {
		groups, limits, err := continuity.DependencyGroups(e.Run.DeviceIDs, continuity.Range{Start: e.Run.Start, End: e.Run.End}, m.Dependencies, m.DataCutoff)
		if err != nil {
			limits = []string{"DEPENDENCY_GROUP_CALCULATION_CAP_REACHED"}
			groups = nil
		}
		outputs = []model.AnalysisOutput{}
		evidence = []model.AnalysisEvidence{}
		dependencies := map[string]model.DependencyFact{}
		for _, v := range m.Dependencies {
			dependencies[v.ID] = v
		}
		for _, g := range groups {
			original := g.ID
			sourceFacts := []model.DependencyFact{}
			for _, id := range g.EvidenceIDs {
				if v, ok := dependencies[id]; ok {
					sourceFacts = append(sourceFacts, v)
				}
			}
			g.ID = e.Run.ID + ":group:" + original
			g.EvidenceIDs = []string{evidenceID(e.Run.ID, "group:"+original)}
			body, _ := json.Marshal(g)
			outputs = append(outputs, model.AnalysisOutput{ID: g.ID, Kind: "dependency-groups", Body: body})
			summary, _ := json.Marshal(map[string]any{"group": g, "relationSources": sourceFacts, "sourceCoverage": m.Sources})
			evidence = append(evidence, model.AnalysisEvidence{ID: g.EvidenceIDs[0], SourceKind: "dependency_snapshot", SourceID: g.ID, DeviceID: g.MemberIDs[0], OccurredAt: g.Start, ResourceVersion: e.Run.ConfigurationVersion, Summary: summary, PermissionCategory: "monitoring-evidence", OriginalAvailability: "FIXED_DERIVED_FACT"})
			f := continuity.Finding{ID: e.Run.ID + ":finding:dependency:" + original, Kind: "DEPENDENCY_CONCENTRATION", Range: g.Range, Explanation: "接入组集中比例只描述本次授权对象的登记关系，不认定实体设施或单点故障。", EvidenceIDs: g.EvidenceIDs, Values: map[string]any{"groupId": g.ID, "visibleDeviceCount": g.VisibleDeviceCount, "analysisDeviceCount": g.AnalysisDeviceCount, "concentration": g.Concentration, "historyQuality": g.HistoryQuality}}
			data, _ := json.Marshal(f)
			outputs = append(outputs, model.AnalysisOutput{ID: f.ID, Kind: "findings", Body: data})
		}
		if m.Parameters.CommonGapPolicy != nil {
			results := []continuity.DeviceResult{}
			for offset := 0; ; {
				page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, e.Run.TenantID, model.AnalysisFilter{RunID: e.Run.ID, Kind: "common-gap-input", Limit: 100, Offset: offset})
				if err != nil {
					return err
				}
				for _, v := range page {
					var r continuity.DeviceResult
					if err = json.Unmarshal(v.Body, &r); err != nil {
						return err
					}
					results = append(results, r)
					limits = append(limits, r.Limitations...)
				}
				offset += len(page)
				if offset >= total {
					break
				}
			}
			if !slices.Contains(limits, "COMMON_GAP_INPUT_CAP_REACHED") {
				findings, err := continuity.CommonGaps(results, continuity.Range{Start: e.Run.Start, End: e.Run.End}, commonPolicy(m.Parameters.CommonGapPolicy), MaxPairs)
				if err != nil {
					limits = unique(limits, "COMMON_GAP_CALCULATION_CAP_REACHED")
				} else {
					byID := map[string]continuity.Interval{}
					for _, r := range results {
						for _, v := range r.Intervals {
							byID[v.ID] = v
						}
					}
					seen := map[string]bool{}
					for _, f := range findings {
						f.ID = e.Run.ID + ":finding:" + f.ID
						for i, id := range f.EvidenceIDs {
							f.EvidenceIDs[i] = evidenceID(e.Run.ID, id)
							if !seen[id] {
								seen[id] = true
								v := byID[id]
								body, _ := json.Marshal(v)
								evidence = append(evidence, model.AnalysisEvidence{ID: evidenceID(e.Run.ID, id), SourceKind: "monitoring_interval", SourceID: intervalID(e.Run.ID, id), DeviceID: v.DeviceID, OccurredAt: v.Start, ResourceVersion: e.Run.ConfigurationVersion, Summary: body, PermissionCategory: "monitoring-evidence", OriginalAvailability: "FIXED_DERIVED_FACT"})
							}
						}
						body, _ := json.Marshal(f)
						outputs = append(outputs, model.AnalysisOutput{ID: f.ID, Kind: "findings", Body: body})
					}
				}
			}
		}
		limitsBody, _ := json.Marshal(map[string]any{"limitations": limits, "dependencyGroupCount": len(groups)})
		outputs = append(outputs, model.AnalysisOutput{ID: e.Run.ID + ":group-summary", Kind: "group-summary", Body: limitsBody})
		// The same interval can justify both a per-device and a common-gap finding.
		// Reuse its previously immutable evidence document rather than rewriting it.
		existing := map[string]bool{}
		for offset := 0; ; {
			page, total, err := s.Analysis.Store.ListAnalysisEvidence(ctx, e.Run.TenantID, model.AnalysisFilter{RunID: e.Run.ID, Limit: 100, Offset: offset})
			if err != nil {
				return err
			}
			for _, v := range page {
				existing[v.ID] = true
			}
			offset += len(page)
			if offset >= total {
				break
			}
		}
		filtered := []model.AnalysisEvidence{}
		for _, v := range evidence {
			if !existing[v.ID] {
				filtered = append(filtered, v)
				existing[v.ID] = true
			}
		}
		evidence = filtered
		if err = s.saveStage(ctx, e, "groups", "", outputs, evidence, *cp); err != nil {
			return err
		}
	}
	for offset := 0; offset < len(outputs)+len(evidence); {
		b := model.AnalysisBatch{ID: fmt.Sprintf("groups/%d", offset), Processed: int64(len(m.Members)), Status: model.AnalysisRunning, Stage: "dependency-and-common-gaps"}
		remaining := s.Analysis.Limits.BatchSize
		for offset < len(outputs) && remaining > 0 {
			b.Outputs = append(b.Outputs, outputs[offset])
			offset++
			remaining--
		}
		for offset >= len(outputs) && offset < len(outputs)+len(evidence) && remaining > 0 {
			b.Evidence = append(b.Evidence, evidence[offset-len(outputs)])
			offset++
			remaining--
		}
		b.Checkpoint, _ = json.Marshal(cp)
		if err = e.Commit(ctx, b); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) statistics(ctx context.Context, r model.AnalysisRun, m manifest) (json.RawMessage, []string, bool, error) {
	totals := map[string]continuity.Metric{}
	devices := 0
	originalDeviceFindings := 0
	limitations := slices.Clone(m.Limitations)
	unknown := false
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "metrics", Limit: 100, Offset: offset})
		if err != nil {
			return nil, nil, false, err
		}
		for _, v := range page {
			var b struct {
				Metrics              []continuity.Metric `json:"metrics"`
				Limitations          []string            `json:"limitations"`
				OriginalFindingCount int                 `json:"originalFindingCount"`
			}
			if err = json.Unmarshal(v.Body, &b); err != nil {
				return nil, nil, false, err
			}
			devices++
			originalDeviceFindings += b.OriginalFindingCount
			for _, l := range b.Limitations {
				limitations = unique(limitations, l)
			}
			for _, metric := range b.Metrics {
				if metric.AttributeID != "" || metric.ProfileID != "" {
					continue
				}
				sum := totals[metric.Track]
				sum.Track = metric.Track
				sum.WindowMs += metric.WindowMs
				sum.PlannedMs += metric.PlannedMs
				sum.ExcludedMs += metric.ExcludedMs
				sum.AvailableMs += metric.AvailableMs
				sum.UnavailableMs += metric.UnavailableMs
				sum.UnknownMs += metric.UnknownMs
				sum.NotApplicableMs += metric.NotApplicableMs
				sum.GapCount += metric.GapCount
				sum.LongestGapMs = max(sum.LongestGapMs, metric.LongestGapMs)
				totals[metric.Track] = sum
				unknown = unknown || metric.UnknownMs > 0
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
	}
	for track, sum := range totals {
		known := sum.AvailableMs + sum.UnavailableMs
		if known > 0 {
			x := float64(sum.AvailableMs) / float64(known)
			sum.KnownAvailability = &x
		}
		if sum.PlannedMs > 0 {
			x := float64(known) / float64(sum.PlannedMs)
			sum.KnownCoverage = &x
		}
		if sum.UnknownMs == 0 && sum.NotApplicableMs == 0 && sum.PlannedMs > 0 {
			x := float64(sum.AvailableMs) / float64(sum.PlannedMs)
			sum.FullWindowAvailability = &x
		}
		totals[track] = sum
	}
	_, findingCount, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "findings", Limit: 1})
	if err != nil {
		return nil, nil, false, err
	}
	page, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "group-summary", Limit: 100})
	if err != nil {
		return nil, nil, false, err
	}
	groupCount := 0
	for _, v := range page {
		var b struct {
			Limitations          []string `json:"limitations"`
			DependencyGroupCount int      `json:"dependencyGroupCount"`
		}
		_ = json.Unmarshal(v.Body, &b)
		for _, l := range b.Limitations {
			limitations = unique(limitations, l)
		}
		groupCount = b.DependencyGroupCount
	}
	quality := []map[string]any{}
	availabilitySources := map[string]int{}
	for _, v := range m.Members {
		availabilitySources[v.Metadata.AvailableAtSource]++
	}
	for _, v := range m.QualitySnapshots {
		quality = append(quality, map[string]any{"id": v.ID, "runId": v.RunID, "version": v.Version, "factsHash": v.FactsHash, "statistics": v.Statistics, "limitations": v.Limitations})
	}
	if len(quality) == 0 {
		limitations = unique(limitations, "QUALITY_SNAPSHOT_NOT_SELECTED")
	}
	data, err := json.Marshal(map[string]any{"algorithmVersion": AlgorithmVersion, "analysedDevices": devices, "expectedDevices": len(r.DeviceIDs), "processedMeasurements": len(m.Members), "availabilitySources": availabilitySources, "tracks": totals, "parseByDevice": m.ParseByDevice, "parseAttemptFailure": "UNKNOWN_NO_IMMUTABLE_ATTEMPT_HISTORY", "qualitySnapshots": quality, "dependencyGroupCount": groupCount, "findingCount": findingCount, "representedFindingCount": findingCount, "originalDeviceFindingCount": originalDeviceFindings, "limitations": limitations, "sourceCoverage": m.Sources})
	return data, limitations, unknown, err
}
func (s *Service) finish(ctx context.Context, e *analytics.Execution, m manifest, cp checkpoint, timedOut bool) error {
	statistics, limitations, unknown, err := s.statistics(ctx, e.Run, m)
	if err != nil {
		return err
	}
	if timedOut {
		limitations = unique(limitations, "RUN_TIME_BUDGET_EXHAUSTED")
	}
	current, err := s.Analysis.Store.GetAnalysisRun(ctx, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	sources := current.Sources
	cutoff := current.DataCutoff
	if cutoff == 0 {
		cutoff = time.Now().UnixMilli()
		sources = []model.AnalysisSourceCoverage{{Source: "monitoring_facts", Start: current.Start, End: current.End, Complete: false, ReadAt: cutoff, Reason: "RUN_TIME_BUDGET_EXHAUSTED"}}
	}
	status := model.AnalysisSucceeded
	missing := []string{}
	affected := []model.AnalysisSourceCoverage{}
	for _, v := range sources {
		if !v.Complete {
			missing = append(missing, v.Source)
			affected = append(affected, v)
			status = model.AnalysisPartial
		}
	}
	if unknown || timedOut || len(limitations) > 0 {
		status = model.AnalysisPartial
	}
	if unknown || timedOut {
		affected = append(affected, model.AnalysisSourceCoverage{Source: "unknown_or_unprocessed_monitoring_tracks", Start: current.Start, End: current.End, Complete: false, ReadAt: cutoff, Reason: "UNKNOWN_OR_UNPROCESSED_TRACKS"})
	}
	snapshot := model.AnalysisSnapshot{ID: current.ID + ":snapshot", DataCutoff: cutoff, InputHashes: current.InputHashes, Sources: sources, Statistics: statistics, InitialStateQuality: "SOURCE_SEED_AND_HISTORY", Limitations: limitations, MissingSources: missing, AffectedIntervals: affected}
	if status == model.AnalysisPartial {
		snapshot.UncomputableMetrics = []string{"未知或未完成区间的完整窗口可用率", "缺失历史关系的过去影响范围"}
	}
	cp.Phase = "complete"
	stage := "complete"
	id := "final"
	if timedOut {
		stage = "time-limit"
		id = "partial-timeout"
	}
	checkpoint, _ := json.Marshal(cp)
	return e.Commit(ctx, model.AnalysisBatch{ID: id, Processed: current.Processed, Checkpoint: checkpoint, Status: status, Stage: stage, Snapshot: &snapshot})
}
func (s *Service) timeout(ctx context.Context, e *analytics.Execution) error {
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	current, err := s.Analysis.Store.GetAnalysisRun(save, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	if analytics.TerminalAnalysisStatus(current.Status) {
		return nil
	}
	m := manifest{ParseByDevice: map[string]parseCounters{}, Sources: current.Sources, Limitations: []string{"RUN_TIME_BUDGET_EXHAUSTED"}}
	if current.InputsFrozen {
		m, err = s.loadManifest(save, current)
		if err != nil {
			return err
		}
	}
	var cp checkpoint
	_ = json.Unmarshal(current.Checkpoint, &cp)
	return s.finish(save, e, m, cp, true)
}
