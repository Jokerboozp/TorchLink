package dataquality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/quality"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type manifestMember struct {
	MessageID string `json:"messageId"`
	Property  string `json:"property"`
	DeviceID  string `json:"deviceId"`
	Hash      string `json:"hash"`
	// The three clocks and actual version metadata are frozen. No property
	// value is copied to the analysis store; it remains in its original store.
	Metadata model.MeasurementFact `json:"metadata"`
}
type frozenManifest struct {
	Version           string                         `json:"version"`
	ConfigurationHash string                         `json:"configurationHash"`
	Parameters        model.QualityRunParameters     `json:"parameters"`
	Members           []manifestMember               `json:"members"`
	Sources           []model.AnalysisSourceCoverage `json:"sources"`
	ParseByDevice     map[string]quality.ParseMetric `json:"parseByDevice"`
	DataCutoff        int64                          `json:"dataCutoff"`
	ReadStart         int64                          `json:"readStart"`
	ReadEnd           int64                          `json:"readEnd"`
	Limitations       []string                       `json:"limitations"`
}
type checkpoint struct {
	ManifestID string `json:"manifestId"`
	NextPair   int    `json:"nextPair"`
	Phase      string `json:"phase"`
}

func valueHash(v model.MeasurementFact) (string, error) {
	copy := v
	copy.AvailableAt = 0
	copy.AvailableAtSource = ""
	copy.HistoricalReconstructionQuality = ""
	return analytics.AnalysisHash(copy)
}
func coverage(q model.FactQuery, c model.FactSourceCoverage, basis string) model.AnalysisSourceCoverage {
	return model.AnalysisSourceCoverage{Source: c.Source + ":" + basis, Start: c.CoverageStart, End: c.CoverageEnd, Complete: c.Complete, ReadAt: c.ReadAt, Version: c.SourceVersion, Reason: strings.Join(append([]string{c.Status, c.HistoricalReconstructionQuality, c.BackfillStatus}, c.Limitations...), ";")}
}
func mergeSource(sources []model.AnalysisSourceCoverage, page model.FactPage[model.MeasurementFact], basis string) []model.AnalysisSourceCoverage {
	all := append([]model.FactSourceCoverage{page.Source}, page.AdditionalSources...)
	for _, c := range all {
		next := coverage(model.FactQuery{}, c, basis)
		found := false
		for i := range sources {
			if sources[i].Source == next.Source {
				sources[i].Complete = sources[i].Complete && next.Complete
				sources[i].Start = max(sources[i].Start, next.Start)
				sources[i].End = min(sources[i].End, next.End)
				if !strings.Contains(sources[i].Reason, next.Reason) {
					sources[i].Reason += ";" + next.Reason
				}
				found = true
				break
			}
		}
		if !found {
			sources = append(sources, next)
		}
	}
	return sources
}
func analysisCoverage(sources []model.AnalysisSourceCoverage, start, end int64) quality.Coverage {
	out := quality.Coverage{State: quality.Assessed, Window: quality.Window{Start: instant(start), End: instant(end)}}
	for _, source := range sources {
		if !source.Complete {
			out.State = quality.Partial
			out.Reasons = append(out.Reasons, source.Source+":"+source.Reason)
		}
		if out.SourceVersion == "" {
			out.SourceVersion = source.Version
		}
	}
	return out
}
func (s *Service) configsForRun(ctx context.Context, r model.AnalysisRun) ([]model.AnalysisConfigRevision, model.QualityRunParameters, error) {
	var parameters model.QualityRunParameters
	if err := strictDecode(r.Parameters, &parameters); err != nil {
		return nil, parameters, err
	}
	ids := append(slices.Clone(parameters.ProfileRevisionIDs), parameters.BaselineRevisionIDs...)
	revisions := []model.AnalysisConfigRevision{}
	for _, id := range ids {
		v, err := s.Analysis.Store.GetAnalysisConfig(ctx, r.TenantID, id)
		if err != nil {
			return nil, parameters, err
		}
		if v.Scope == "PERSONAL" && v.Creator != r.Creator {
			return nil, parameters, analytics.ErrForbidden
		}
		if v.Kind != model.DataQualityProfileKind && v.Kind != model.DataQualityBaselineKind {
			return nil, parameters, invalid("冻结配置种类不匹配")
		}
		revisions = append(revisions, v)
	}
	slices.SortFunc(revisions, func(a, b model.AnalysisConfigRevision) int { return strings.Compare(a.ID, b.ID) })
	hash, err := configurationHash(revisions)
	if err != nil {
		return nil, parameters, err
	}
	if hash != r.ConfigurationVersion {
		return nil, parameters, model.ErrAnalysisConflict
	}
	return revisions, parameters, nil
}

func configurationHash(revisions []model.AnalysisConfigRevision) (string, error) {
	type identity struct {
		ID, Kind, ResourceID, Scope, Creator, BodyHash string
		Version                                        int64
	}
	inputs := make([]identity, 0, len(revisions))
	for _, v := range revisions {
		hash, err := analytics.AnalysisHash(v.Body)
		if err != nil {
			return "", err
		}
		inputs = append(inputs, identity{v.ID, v.Kind, v.ResourceID, v.Scope, v.Creator, hash, v.Version})
	}
	slices.SortFunc(inputs, func(a, b identity) int { return strings.Compare(a.ID, b.ID) })
	return analytics.AnalysisHash(inputs)
}
func (s *Service) freeze(ctx context.Context, e *analytics.Execution, revisions []model.AnalysisConfigRevision, parameters model.QualityRunParameters) (frozenManifest, string, error) {
	run := e.Run
	manifest := frozenManifest{Version: "quality-manifest-v1", ConfigurationHash: run.ConfigurationVersion, Parameters: parameters, Members: []manifestMember{}, Sources: []model.AnalysisSourceCoverage{}, ParseByDevice: map[string]quality.ParseMetric{}, DataCutoff: time.Now().UnixMilli()}
	padding := int64(0)
	for _, v := range revisions {
		if v.Kind == model.DataQualityProfileKind {
			_, p, err := profile(v)
			if err != nil {
				return manifest, "", err
			}
			padding = max(padding, p.ToleranceMs)
		}
	}
	if padding > math.MaxInt64-run.End {
		return manifest, "", invalid("区间边界超限")
	}
	manifest.ReadStart = max(int64(1), run.Start-padding)
	manifest.ReadEnd = run.End + padding
	if s.Facts == nil {
		return manifest, "", analytics.ErrUnsupported
	}
	members := map[string]manifestMember{}
	rawByDevice := map[string][]quality.ParseOutcome{}
	rawCoverage := []model.AnalysisSourceCoverage{}
	err := s.Facts.AnalyticsFactsRead(ctx, run.TenantID, func(reader ports.AnalyticsFactReader) error {
		totalRaw := 0
		for _, basis := range []string{"EVENT", "RECEIVED"} {
			q := model.FactQuery{DeviceIDs: run.DeviceIDs, Properties: parameters.AttributeIDs, Start: manifest.ReadStart, End: manifest.ReadEnd, TimeBasis: basis, Limit: min(1000, s.Analysis.Limits.BatchSize)}
			for {
				if ctx.Err() != nil {
					manifest.Limitations = append(manifest.Limitations, "READ_TIME_BUDGET_EXHAUSTED")
					return nil
				}
				page, err := reader.QueryMeasurementSeries(q)
				if err != nil {
					return err
				}
				manifest.Sources = mergeSource(manifest.Sources, page, basis)
				for _, v := range page.Items {
					key := v.DeviceID + "\x00" + v.ID
					if _, exists := members[key]; exists {
						continue
					}
					if len(members) >= s.recordLimit() {
						manifest.Limitations = append(manifest.Limitations, "MEASUREMENT_READ_CAP_REACHED")
						break
					}
					hash, err := valueHash(v)
					if err != nil {
						return err
					}
					meta := v
					meta.Value = nil
					members[key] = manifestMember{MessageID: v.MessageID, Property: v.Property, DeviceID: v.DeviceID, Hash: hash, Metadata: meta}
				}
				if len(members) >= s.recordLimit() && page.HasMore {
					manifest.Limitations = append(manifest.Limitations, "MEASUREMENT_READ_CAP_REACHED")
					break
				}
				if !page.HasMore {
					break
				}
				q.Cursor = page.Cursor
			}
		}
		q := model.FactQuery{DeviceIDs: run.DeviceIDs, Start: run.Start, End: run.End, Limit: min(1000, s.Analysis.Limits.BatchSize)}
		for {
			if ctx.Err() != nil {
				manifest.Limitations = append(manifest.Limitations, "PARSE_READ_TIME_BUDGET_EXHAUSTED")
				break
			}
			page, err := reader.ListRawParseOutcomes(q)
			if err != nil {
				return err
			}
			next := coverage(q, page.Source, "RAW")
			rawCoverage = append(rawCoverage, next)
			for _, v := range page.Items {
				if totalRaw >= s.recordLimit() {
					manifest.Limitations = append(manifest.Limitations, "RAW_READ_CAP_REACHED")
					break
				}
				status := quality.ParseUnknown
				switch v.Outcome {
				case "ARCHIVED_NOT_ATTEMPTED":
					status = quality.ParseNotAttempted
				case "LAST_ATTEMPT_FAILED":
					status = quality.ParseFailed
				case "STANDARD_SAVED":
					status = quality.ParseSucceeded
				}
				rawByDevice[v.DeviceID] = append(rawByDevice[v.DeviceID], quality.ParseOutcome{RawMessageID: v.RawMessageID, ReceivedAt: instant(v.ReceivedAt), Archived: v.ArchivedAt > 0, Attempted: v.ParseAttemptedAt > 0, LastStatus: status, SuccessfulStandardMessages: v.SuccessfulStandardMessages})
				totalRaw++
			}
			if totalRaw >= s.recordLimit() && page.HasMore {
				manifest.Limitations = append(manifest.Limitations, "RAW_READ_CAP_REACHED")
				break
			}
			if !page.HasMore {
				break
			}
			q.Cursor = page.Cursor
		}
		return nil
	})
	if err != nil {
		return manifest, "", err
	}
	for _, v := range members {
		manifest.Members = append(manifest.Members, v)
	}
	slices.SortFunc(manifest.Members, func(a, b manifestMember) int {
		return strings.Compare(a.DeviceID+"\x00"+a.MessageID+"\x00"+a.Property, b.DeviceID+"\x00"+b.MessageID+"\x00"+b.Property)
	})
	parseCoverage := analysisCoverage(rawCoverage, run.Start, run.End)
	for _, device := range run.DeviceIDs {
		r, err := quality.Analyze(quality.Input{DeviceID: device, AttributeID: "parse", Window: quality.Window{Start: instant(run.Start), End: instant(run.End)}, Coverage: parseCoverage, ParseOutcomes: rawByDevice[device], ParseCoverage: parseCoverage})
		if err != nil {
			return manifest, "", err
		}
		manifest.ParseByDevice[device] = r.Parse
	}
	for _, next := range rawCoverage {
		found := false
		for i, v := range manifest.Sources {
			if v.Source == next.Source {
				manifest.Sources[i].Complete = v.Complete && next.Complete
				found = true
				break
			}
		}
		if !found {
			manifest.Sources = append(manifest.Sources, next)
		}
	}
	if len(manifest.Sources) == 0 {
		manifest.Sources = []model.AnalysisSourceCoverage{{Source: "facts", Start: run.Start, End: run.End, Complete: false, ReadAt: manifest.DataCutoff, Reason: "NO_SOURCE_COVERAGE"}}
	}
	if len(manifest.Limitations) > 0 {
		for i := range manifest.Sources {
			manifest.Sources[i].Complete = false
			manifest.Sources[i].Reason += ";" + strings.Join(manifest.Limitations, ";")
		}
	}
	manifestID := run.ID + "/input-manifest"
	body, _ := json.Marshal(manifest)
	hash, err := analytics.AnalysisHash(manifest)
	if err != nil {
		return manifest, "", err
	}
	cp, _ := json.Marshal(checkpoint{ManifestID: manifestID, Phase: "frozen"})
	err = e.Commit(ctx, model.AnalysisBatch{ID: "freeze", FreezeInputs: true, InputHashes: []string{hash}, Sources: manifest.Sources, DataCutoff: manifest.DataCutoff, Checkpoint: cp, Processed: max(e.Run.Processed, int64(len(manifest.Members))), Status: model.AnalysisRunning, Stage: "inputs-frozen", Outputs: []model.AnalysisOutput{{ID: manifestID, Kind: "input-manifest", Body: body}}})
	return manifest, manifestID, err
}
func (s *Service) loadManifest(ctx context.Context, r model.AnalysisRun) (frozenManifest, string, error) {
	var cp checkpoint
	if err := json.Unmarshal(r.Checkpoint, &cp); err != nil {
		return frozenManifest{}, "", err
	}
	items, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{Kind: "input-manifest", RunID: r.ID, Limit: 100})
	if err != nil {
		return frozenManifest{}, "", err
	}
	for _, item := range items {
		if item.ID == cp.ManifestID {
			var manifest frozenManifest
			if err = json.Unmarshal(item.Body, &manifest); err != nil {
				return manifest, "", err
			}
			hash, _ := analytics.AnalysisHash(manifest)
			if len(r.InputHashes) != 1 || r.InputHashes[0] != hash || manifest.ConfigurationHash != r.ConfigurationVersion {
				return manifest, "", model.ErrAnalysisConflict
			}
			return manifest, item.ID, nil
		}
	}
	return frozenManifest{}, "", model.ErrNotFound
}
func (s *Service) readMembers(ctx context.Context, r model.AnalysisRun, members []manifestMember) ([]quality.Sample, []string, error) {
	values := map[string]model.MeasurementFact{}
	issues := []string{}
	err := s.Facts.AnalyticsFactsRead(ctx, r.TenantID, func(reader ports.AnalyticsFactReader) error {
		for offset := 0; offset < len(members); offset += 1000 {
			batch := members[offset:min(len(members), offset+1000)]
			identities := make([]model.MeasurementIdentity, len(batch))
			for i, v := range batch {
				identities[i] = model.MeasurementIdentity{MessageID: v.MessageID, Property: v.Property}
			}
			q := model.FactQuery{DeviceIDs: r.DeviceIDs, Members: identities, Start: 0, End: math.MaxInt64, Limit: 1000}
			for {
				page, err := reader.QueryMeasurementSeries(q)
				if err != nil {
					return err
				}
				for _, v := range page.Items {
					values[v.DeviceID+"\x00"+v.ID] = v
				}
				if !page.HasMore {
					break
				}
				q.Cursor = page.Cursor
			}
		}
		return nil
	})
	if err != nil {
		return nil, issues, err
	}
	samples := make([]quality.Sample, 0, len(members))
	for _, member := range members {
		v, exists := values[member.DeviceID+"\x00"+member.MessageID+":"+member.Property]
		if !exists || v.HistoricalReconstructionQuality == "SOURCE_RECORD_MISSING" {
			issues = appendUnique(issues, "FROZEN_SOURCE_EXPIRED_OR_MISSING")
			continue
		}
		hash, err := valueHash(v)
		if err != nil {
			return nil, issues, err
		}
		if hash != member.Hash {
			issues = appendUnique(issues, "FROZEN_SOURCE_VERSION_CHANGED")
			continue
		}
		frozen := member.Metadata
		frozen.Value = v.Value
		samples = append(samples, sample(frozen))
	}
	return samples, issues, nil
}
func appendUnique(items []string, item string) []string {
	if !slices.Contains(items, item) {
		items = append(items, item)
	}
	return items
}
func (s *Service) Process(ctx context.Context, e *analytics.Execution) (resultErr error) {
	defer func() {
		if errors.Is(resultErr, context.DeadlineExceeded) {
			if err := s.finishTimeout(ctx, e); err == nil {
				resultErr = nil
			}
		}
	}()
	revisions, parameters, err := s.configsForRun(ctx, e.Run)
	if err != nil {
		return err
	}
	manifest := frozenManifest{}
	manifestID := ""
	if e.Run.InputsFrozen {
		manifest, manifestID, err = s.loadManifest(ctx, e.Run)
	} else {
		manifest, manifestID, err = s.freeze(ctx, e, revisions, parameters)
	}
	if err != nil {
		return err
	}
	var cp checkpoint
	_ = json.Unmarshal(e.Run.Checkpoint, &cp)
	cp.ManifestID = manifestID
	cp.Phase = "calculating"
	limitations := slices.Clone(manifest.Limitations)
	processed := max(e.Run.Processed, int64(len(manifest.Members)))
	pairIndex := 0
	for _, device := range e.Run.DeviceIDs {
		for _, attribute := range parameters.AttributeIDs {
			if pairIndex < cp.NextPair {
				pairIndex++
				continue
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			selected := []manifestMember{}
			for _, m := range manifest.Members {
				if m.DeviceID == device && m.Property == attribute {
					selected = append(selected, m)
				}
			}
			samples, issues, err := s.readMembers(ctx, e.Run, selected)
			if err != nil {
				return err
			}
			for _, issue := range issues {
				limitations = appendUnique(limitations, issue)
			}
			profiles := []quality.Profile{}
			baselines := []quality.Baseline{}
			for _, v := range revisions {
				if !slices.Contains(v.DeviceIDs, device) {
					continue
				}
				if v.Kind == model.DataQualityProfileKind {
					p, b, err := profile(v)
					if err != nil {
						return err
					}
					if b.AttributeID == attribute {
						profiles = append(profiles, p)
					}
				} else {
					b, raw, err := baseline(v)
					if err != nil {
						return err
					}
					if raw.DeviceID == device && raw.AttributeID == attribute {
						baselines = append(baselines, b)
					}
				}
			}
			cov := analysisCoverage(manifest.Sources, manifest.ReadStart, manifest.ReadEnd)
			if len(issues) > 0 {
				cov.State = quality.Partial
				cov.Reasons = append(cov.Reasons, issues...)
			}
			// The raw parsing counters were computed inside the frozen source read and
			// remain unchanged if a later retry changes raw.parse_attempted_at.
			parse := manifest.ParseByDevice[device]
			result, err := quality.Analyze(quality.Input{DeviceID: device, AttributeID: attribute, Window: quality.Window{Start: instant(e.Run.Start), End: instant(e.Run.End)}, Profiles: profiles, Baselines: baselines, Samples: samples, Coverage: cov, ParseCoverage: quality.Coverage{State: quality.Assessed, Window: quality.Window{Start: instant(e.Run.Start), End: instant(e.Run.End)}}, MaximumSlots: MaxSlots})
			if err != nil {
				return invalid(err.Error())
			}
			result.Parse = parse
			if parse.State != quality.Assessed && parse.State != quality.NotApplicable && result.State == quality.Assessed {
				result.State = quality.Partial
			}
			if result.State == quality.Partial || result.State == quality.Unknown {
				limitations = appendUnique(limitations, "SERIES_WITH_INSUFFICIENT_EVIDENCE")
			}
			outputs, evidence := resultDocuments(e.Run.ID, result)
			for offset := 0; offset < len(outputs)+len(evidence); {
				batch := model.AnalysisBatch{ID: fmt.Sprintf("result/%d/%d", pairIndex, offset), Status: model.AnalysisRunning, Stage: fmt.Sprintf("calculating:%d", pairIndex+1), Processed: processed}
				remaining := s.Analysis.Limits.BatchSize
				for offset < len(outputs) && remaining > 0 {
					batch.Outputs = append(batch.Outputs, outputs[offset])
					offset++
					remaining--
				}
				for offset >= len(outputs) && offset < len(outputs)+len(evidence) && remaining > 0 {
					batch.Evidence = append(batch.Evidence, evidence[offset-len(outputs)])
					offset++
					remaining--
				}
				next := cp
				if offset == len(outputs)+len(evidence) {
					next.NextPair = pairIndex + 1
				}
				batch.Checkpoint, _ = json.Marshal(next)
				if err = e.Commit(ctx, batch); err != nil {
					return err
				}
			}
			cp.NextPair = pairIndex + 1
			pairIndex++
		}
	}
	statistics, err := s.summary(ctx, e.Run, manifest)
	if err != nil {
		return err
	}
	current, err := s.Analysis.Store.GetAnalysisRun(ctx, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	status := model.AnalysisSucceeded
	missing := []string{}
	affected := []model.AnalysisSourceCoverage{}
	for _, source := range manifest.Sources {
		if !source.Complete {
			status = model.AnalysisPartial
			missing = append(missing, source.Source)
			affected = append(affected, source)
		}
	}
	if len(limitations) > 0 {
		status = model.AnalysisPartial
	}
	snapshot := model.AnalysisSnapshot{ID: e.Run.ID + "/snapshot", DataCutoff: manifest.DataCutoff, InputHashes: current.InputHashes, Sources: manifest.Sources, InitialStateQuality: "NOT_APPLICABLE", Statistics: statistics, Limitations: limitations, MissingSources: missing, AffectedIntervals: affected, UncomputableMetrics: []string{}}
	if len(missing)+len(limitations) > 0 {
		snapshot.UncomputableMetrics = []string{"完整窗口质量结论", "完整首次可用时间重建"}
	}
	cp.Phase = "complete"
	data, _ := json.Marshal(cp)
	return e.Commit(ctx, model.AnalysisBatch{ID: "final", Processed: processed, Checkpoint: data, Status: status, Stage: "complete", Snapshot: &snapshot})
}
func resultDocuments(runID string, result quality.Result) ([]model.AnalysisOutput, []model.AnalysisEvidence) {
	totalFindings, totalEvidence := len(result.Findings), len(result.Evidence)
	segmentCounts := make([]int, len(result.Metrics))
	for i, m := range result.Metrics {
		segmentCounts[i] = len(m.Sequence.Segments)
	}
	selectedEvidence := map[string]bool{}
	selectedFindings := []quality.Finding{}
	kindCount := map[string]int{}
	for _, f := range result.Findings {
		if kindCount[f.Kind] >= 20 || len(selectedFindings) >= 200 {
			continue
		}
		kindCount[f.Kind]++
		f.ID = runID + ":finding:" + f.ID
		f.MetricID = runID + ":metric:" + f.MetricID
		for i, id := range f.EvidenceIDs {
			selectedEvidence[id] = true
			f.EvidenceIDs[i] = runID + ":evidence:" + id
		}
		selectedFindings = append(selectedFindings, f)
	}
	for i := range result.Metrics {
		result.Metrics[i].ID = runID + ":metric:" + result.Metrics[i].ID
		result.Metrics[i].EventCompleteness.Slots = nil
		result.Metrics[i].ReceptionCompleteness.Slots = nil
		if len(result.Metrics[i].Sequence.Segments) > 100 {
			result.Metrics[i].Sequence.Segments = result.Metrics[i].Sequence.Segments[:100]
		}
	}
	evidence := []model.AnalysisEvidence{}
	for _, ev := range result.Evidence {
		if !selectedEvidence[ev.ID] {
			continue
		}
		ev.ID = runID + ":evidence:" + ev.ID
		evidence = append(evidence, model.AnalysisEvidence{ID: ev.ID, SourceKind: "standard_measurement", SourceID: ev.MessageID, DeviceID: result.DeviceID, OccurredAt: ev.EventAt.UnixMilli(), ResourceVersion: ev.ProtocolVersion + ":" + ev.ConfigurationVersion, Summary: publicJSON(ev), PermissionCategory: "data-quality-evidence", OriginalAvailability: "AVAILABLE_AT_FREEZE", RawMessageID: ev.RawMessageID})
	}
	result.Findings = nil
	result.Evidence = nil
	body := publicJSON(result)
	var metric map[string]any
	_ = json.Unmarshal(body, &metric)
	if items, ok := metric["metrics"].([]any); ok {
		for i, item := range items {
			m, _ := item.(map[string]any)
			sequence, _ := m["sequence"].(map[string]any)
			sequence["originalSegmentCount"] = segmentCounts[i]
			sequence["representedSegmentCount"] = min(100, segmentCounts[i])
			if segmentCounts[i] > 100 {
				sequence["representationLimitation"] = "仅展示前100个完整序列段统计，所有版本断点计数仍使用全量输入"
			}
		}
	}
	metric["originalFindingCount"] = totalFindings
	metric["representedFindingCount"] = len(selectedFindings)
	metric["originalEvidenceCount"] = totalEvidence
	metric["representedEvidenceCount"] = len(evidence)
	metric["representationPolicy"] = "最多每类20条发现，总计200条；分母使用全部冻结样本"
	data, _ := json.Marshal(metric)
	idHash, _ := analytics.AnalysisHash([]string{result.DeviceID, result.AttributeID})
	outputs := []model.AnalysisOutput{{ID: runID + ":series-metric:" + idHash, Kind: "metrics", DeviceID: result.DeviceID, Body: data}}
	for _, f := range selectedFindings {
		var value map[string]any
		_ = json.Unmarshal(publicJSON(f), &value)
		value["deviceId"] = result.DeviceID
		value["attributeId"] = result.AttributeID
		value["manualState"] = "UNREVIEWED"
		b, _ := json.Marshal(value)
		outputs = append(outputs, model.AnalysisOutput{ID: f.ID, Kind: "findings", DeviceID: result.DeviceID, Body: b})
	}
	return outputs, evidence
}
func (s *Service) summary(ctx context.Context, run model.AnalysisRun, manifest frozenManifest) (json.RawMessage, error) {
	type dimension struct {
		Numerator            int `json:"numerator"`
		Denominator          int `json:"denominator"`
		UnknownWindows       int `json:"unknownWindows"`
		PartialWindows       int `json:"partialWindows"`
		NotApplicableWindows int `json:"notApplicableWindows"`
	}
	dimensions := map[string]dimension{}
	states := map[string]int{}
	findingCount := 0
	offset := 0
	series := 0
	for {
		items, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, run.TenantID, model.AnalysisFilter{Kind: "metrics", RunID: run.ID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			var result struct {
				State                quality.State          `json:"state"`
				Metrics              []quality.WindowMetric `json:"metrics"`
				OriginalFindingCount int                    `json:"originalFindingCount"`
			} // Times are Unixms in storage, so decode only the numeric counters.
			var body map[string]json.RawMessage
			_ = json.Unmarshal(item.Body, &body)
			_ = json.Unmarshal(body["state"], &result.State)
			_ = json.Unmarshal(body["originalFindingCount"], &result.OriginalFindingCount)
			findingCount += result.OriginalFindingCount
			states[string(result.State)]++
			series++
			var metrics []map[string]json.RawMessage
			_ = json.Unmarshal(body["metrics"], &metrics)
			for _, m := range metrics {
				ratios := map[string]quality.Ratio{}
				var format, rangeRatio quality.Ratio
				_ = json.Unmarshal(m["format"], &format)
				_ = json.Unmarshal(m["range"], &rangeRatio)
				ratios["format"] = format
				ratios["range"] = rangeRatio
				for _, spec := range []struct{ field, key, name string }{{"eventCompleteness", "missing", "eventMissing"}, {"receptionCompleteness", "missing", "receptionMissing"}, {"time", "outOfOrder", "outOfOrder"}, {"time", "rollback", "rollback"}, {"time", "future", "future"}, {"sequence", "stable", "stable"}, {"sequence", "rate", "rate"}, {"sequence", "deviation", "deviation"}, {"sequence", "drift", "drift"}} {
					var parent map[string]json.RawMessage
					_ = json.Unmarshal(m[spec.field], &parent)
					var ratio quality.Ratio
					_ = json.Unmarshal(parent[spec.key], &ratio)
					ratios[spec.name] = ratio
				}
				for name, ratio := range ratios {
					d := dimensions[name]
					d.Numerator += ratio.Numerator
					d.Denominator += ratio.Denominator
					switch ratio.State {
					case quality.Unknown:
						d.UnknownWindows++
					case quality.Partial:
						d.PartialWindows++
					case quality.NotApplicable:
						d.NotApplicableWindows++
					}
					dimensions[name] = d
				}
			}
		}
		offset += len(items)
		if offset >= total {
			break
		}
	}
	return json.Marshal(map[string]any{"algorithmVersion": AlgorithmVersion, "processedMeasurements": len(manifest.Members), "analysedSeries": series, "seriesStates": states, "dimensions": dimensions, "originalFindingCount": findingCount, "parseByDevice": manifest.ParseByDevice, "limitations": manifest.Limitations, "sourceCoverage": manifest.Sources})
}

// A time budget stops computation at a durable boundary and reports which
// series were never computed. External shutdown cancellation keeps the lease
// available for takeover instead of inventing a completed partial run.
func (s *Service) finishTimeout(ctx context.Context, e *analytics.Execution) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	current, err := s.Analysis.Store.GetAnalysisRun(saveCtx, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	if analytics.TerminalAnalysisStatus(current.Status) {
		return nil
	}
	cutoff := current.DataCutoff
	if cutoff == 0 {
		cutoff = time.Now().UnixMilli()
	}
	sources := current.Sources
	if len(sources) == 0 {
		sources = []model.AnalysisSourceCoverage{{Source: "quality_input", Start: current.Start, End: current.End, Complete: false, ReadAt: cutoff, Reason: "RUN_TIME_BUDGET_EXHAUSTED"}}
	}
	manifest := frozenManifest{Sources: sources, ParseByDevice: map[string]quality.ParseMetric{}, Limitations: []string{"RUN_TIME_BUDGET_EXHAUSTED"}}
	if current.InputsFrozen {
		if frozen, _, e := s.loadManifest(saveCtx, current); e == nil {
			manifest = frozen
			manifest.Limitations = appendUnique(manifest.Limitations, "RUN_TIME_BUDGET_EXHAUSTED")
		}
	}
	statistics, err := s.summary(saveCtx, current, manifest)
	if err != nil {
		return err
	}
	var counters map[string]any
	_ = json.Unmarshal(statistics, &counters)
	var params model.QualityRunParameters
	_ = json.Unmarshal(current.Parameters, &params)
	counters["incomplete"] = true
	counters["expectedSeries"] = len(current.DeviceIDs) * len(params.AttributeIDs)
	counters["reason"] = "RUN_TIME_BUDGET_EXHAUSTED"
	statistics, _ = json.Marshal(counters)
	snapshot := model.AnalysisSnapshot{ID: current.ID + "/snapshot", DataCutoff: cutoff, InputHashes: current.InputHashes, Sources: sources, InitialStateQuality: "NOT_APPLICABLE", Statistics: statistics, Limitations: []string{"RUN_TIME_BUDGET_EXHAUSTED"}, AffectedIntervals: []model.AnalysisSourceCoverage{{Source: "unprocessed_quality_series", Start: current.Start, End: current.End, Complete: false, ReadAt: cutoff, Reason: "RUN_TIME_BUDGET_EXHAUSTED"}}, UncomputableMetrics: []string{"尚未完成系列的完整质量指标"}}
	return e.Commit(saveCtx, model.AnalysisBatch{ID: "partial-timeout", Checkpoint: current.Checkpoint, Processed: current.Processed, Status: model.AnalysisPartial, Stage: "time-limit", Snapshot: &snapshot})
}
