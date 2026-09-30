package recurring

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const HistoricalProjectionMode = "HISTORICAL_PROJECTION"
const ProjectionAlgorithmVersion = "historical-observation-projection-v1"

// ProjectionReference pins an immutable, completed projection and its entire
// device scope. Consumers cannot cut members out of a shared source.
type ProjectionReference struct {
	RunID      string   `json:"runId"`
	SnapshotID string   `json:"snapshotId"`
	FactsHash  string   `json:"factsHash"`
	DeviceIDs  []string `json:"deviceIds"`
	Start      int64    `json:"start"`
	End        int64    `json:"end"`
	DataCutoff int64    `json:"dataCutoff"`
	Status     string   `json:"status"`
}

type ProjectionSourceProgress struct {
	Source          string `json:"source"`
	MessageScanned  int    `json:"messageScanned"`
	OmittedMessages int    `json:"omittedMessages"`
	DuplicateInputs int    `json:"duplicateInputs"`
	Cursor          string `json:"cursor,omitempty"`
	ReadAt          int64  `json:"readAt"`
	Exhausted       bool   `json:"exhausted"`
	Status          string `json:"status"`
}
type ProjectionStatistics struct {
	JobMode          string                     `json:"jobMode"`
	MessageScanned   int                        `json:"messageScanned"`
	NormalizedFacts  int                        `json:"normalizedFacts"`
	OmittedMessages  int                        `json:"omittedMessages"`
	CompletedBatches int                        `json:"completedBatches"`
	Quality          string                     `json:"quality"`
	SourceProgress   []ProjectionSourceProgress `json:"sourceProgress"`
}
type projectionManifest struct {
	Parameters   Parameters                     `json:"parameters"`
	Observations []model.AlarmObservation       `json:"observations"`
	Progress     []ProjectionSourceProgress     `json:"sourceProgress"`
	Sources      []model.AnalysisSourceCoverage `json:"sources"`
	Cutoff       int64                          `json:"cutoff"`
	Limitations  []string                       `json:"limitations"`
	Missing      []string                       `json:"missingSources"`
}
type projectionCheckpoint struct {
	NextFact         int `json:"nextFact"`
	CompletedBatches int `json:"completedBatches"`
}

func (s *Service) validateProjectionCreate(_ context.Context, _ analytics.Actor, q *analytics.CreateRequest, p Parameters) error {
	if p.TimeBasis != "EVENT_AT" || p.CaseID != "" || p.RoundID != "" || p.PlanID != "" || p.ExpectedDataRevision != 0 || p.ProfileRevisionID != "" || p.IncludeHistoricalStandardMessages || len(p.HistoricalProjectionRunIDs) != 0 || p.DiscoveryPolicy != (DiscoveryPolicy{}) {
		return invalid("历史归一化仅接受明确设备、事件时间区间和历史来源")
	}
	if len(p.HistoricalSources) == 0 || len(p.HistoricalSources) > 2 {
		return invalid("请显式选择POSTGRESQL或CLICKHOUSE历史来源")
	}
	seen := map[string]bool{}
	for _, source := range p.HistoricalSources {
		if source != "POSTGRESQL" && source != "CLICKHOUSE" || seen[source] {
			return invalid("历史来源重复或不受支持")
		}
		seen[source] = true
	}
	slices.Sort(p.HistoricalSources)
	limit := min(50000, max(1, s.RecordLimit))
	if p.ProjectionLimit == 0 {
		p.ProjectionLimit = limit
	}
	if p.ProjectionLimit < 1 || p.ProjectionLimit > limit {
		return invalid("历史归一化读取上限超过服务端限制")
	}
	q.Parameters, _ = json.Marshal(p)
	q.ConfigurationVersion = model.GovernanceHash(p)
	return nil
}

func (s *Service) freezeProjection(ctx context.Context, r model.AnalysisRun, p Parameters) (projectionManifest, error) {
	m := projectionManifest{Parameters: p, Observations: []model.AlarmObservation{}, Cutoff: time.Now().UnixMilli(), Limitations: []string{"HISTORICAL_COLLECTION_COVERAGE_UNKNOWN", "ORIGINAL_PRODUCTION_ACCEPTANCE_UNKNOWN", "RULE_EVALUATIONS_NOT_RECONSTRUCTED"}, Missing: []string{"ORIGINAL_RULE_EVALUATION_LEDGER", "HISTORICAL_COLLECTION_COVERAGE"}}
	seen := map[string]string{}
	readLimit := p.ProjectionLimit
	if readLimit < 1 || readLimit > min(50000, max(1, s.RecordLimit)) {
		return m, invalid("冻结历史归一化上限无效")
	}
	total := 0
	consume := func(msg model.StandardMessage, progress *ProjectionSourceProgress) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if msg.TenantID != r.TenantID || !slices.Contains(r.DeviceIDs, msg.DeviceID) || msg.Timestamp < r.Start || msg.Timestamp >= r.End {
			return analytics.ErrForbidden
		}
		total++
		progress.MessageScanned++
		progress.Cursor = fmt.Sprintf("%019d:%s", msg.Timestamp, msg.MessageID)
		inputKey := msg.DeviceID + ":" + msg.MessageID
		if _, ok := seen[inputKey]; ok {
			// The processed PostgreSQL original takes precedence over the
			// lossy archive projection of the same immutable message.
			progress.DuplicateInputs++
			return nil
		}
		seen[inputKey] = model.ObservationHash(msg)
		normalized, err := NormalizeHistoricalMessage(msg, nil, m.Cutoff)
		if err != nil {
			progress.OmittedMessages++
			m.Limitations = addIssue(m.Limitations, "HISTORICAL_MESSAGE_NOT_NORMALIZABLE")
			return nil
		}
		if len(normalized.Observations) == 0 {
			progress.OmittedMessages++
			m.Limitations = addIssue(m.Limitations, "NO_RECONSTRUCTABLE_ALARM_SIGNAL")
		}
		for _, o := range normalized.Observations {
			if o.EventAt < r.Start || o.EventAt >= r.End {
				m.Limitations = addIssue(m.Limitations, "COMPONENT_TIME_OUTSIDE_REQUESTED_RANGE")
				continue
			}
			if len(m.Observations) >= readLimit {
				m.Limitations = addIssue(m.Limitations, "NORMALIZED_FACT_LIMIT_REACHED")
				break
			}
			o.Payload = nil
			if progress.Source == "CLICKHOUSE" {
				o.SourceSystem = "HISTORICAL_CLICKHOUSE_TELEMETRY"
				o.ID = ""
				o.TimeQuality = "UNVERIFIED"
				o.Reason = "ORIGINAL_STANDARD_METADATA_AND_ACCEPTANCE_UNKNOWN"
				o.SourceContentHash = ""
				if err := o.Normalize(); err != nil {
					return err
				}
			}
			m.Observations = append(m.Observations, o)
		}
		for _, issue := range normalized.Limitations {
			m.Limitations = addIssue(m.Limitations, issue)
		}
		return nil
	}
	// PostgreSQL is read first, in one repeatable snapshot. The selected
	// ClickHouse range has its own explicit read cutoff, never a fake shared one.
	for _, source := range []string{"POSTGRESQL", "CLICKHOUSE"} {
		if !slices.Contains(p.HistoricalSources, source) {
			continue
		}
		progress := ProjectionSourceProgress{Source: source, ReadAt: time.Now().UnixMilli(), Status: "AVAILABLE"}
		if source == "POSTGRESQL" {
			err := s.Store.GovernanceRead(ctx, r.TenantID, func(tx ports.AlarmGovernanceTx) error {
				reader, ok := tx.(ports.GovernanceHistoricalReader)
				if !ok {
					progress.Status = "UNAVAILABLE"
					return nil
				}
				for {
					remaining := readLimit - total
					page, err := reader.ListGovernanceHistoricalMessages(ports.AlarmObservationFilter{DeviceIDs: r.DeviceIDs, Start: r.Start, End: r.End, Cursor: progress.Cursor, Limit: min(1000, remaining+1)})
					if err != nil {
						return err
					}
					for _, msg := range page {
						if total >= readLimit {
							m.Limitations = addIssue(m.Limitations, "HISTORICAL_READ_LIMIT_REACHED")
							return nil
						}
						if err := consume(msg, &progress); err != nil {
							return err
						}
					}
					if len(page) < min(1000, remaining+1) {
						progress.Exhausted = true
						return nil
					}
				}
			})
			if err != nil {
				return m, err
			}
		} else {
			m.Limitations = addIssue(m.Limitations, "CLICKHOUSE_ORIGINAL_STANDARD_METADATA_MISSING")
			m.Limitations = addIssue(m.Limitations, "CROSS_STORE_READ_CUTOFFS_ARE_INDEPENDENT")
			if s.HistoricalArchive == nil {
				progress.Status = "UNAVAILABLE"
			} else {
				for {
					if total >= readLimit {
						m.Limitations = addIssue(m.Limitations, "HISTORICAL_READ_LIMIT_REACHED")
						break
					}
					remaining := readLimit - total
					page, err := s.HistoricalArchive.ListGovernanceHistoricalArchiveMessages(ctx, r.TenantID, ports.AlarmObservationFilter{DeviceIDs: r.DeviceIDs, Start: r.Start, End: r.End, Cursor: progress.Cursor, Limit: min(1000, remaining+1)})
					if err != nil {
						if ctx.Err() != nil {
							return m, ctx.Err()
						}
						progress.Status = "QUERY_FAILED"
						break
					}
					if page.Source.ReadAt > 0 {
						progress.ReadAt = max(progress.ReadAt, page.Source.ReadAt)
					}
					for _, msg := range page.Items {
						if total >= readLimit {
							m.Limitations = addIssue(m.Limitations, "HISTORICAL_READ_LIMIT_REACHED")
							break
						}
						if err := consume(msg, &progress); err != nil {
							return m, err
						}
					}
					if total >= readLimit && (len(page.Items) > remaining || page.HasMore) {
						m.Limitations = addIssue(m.Limitations, "HISTORICAL_READ_LIMIT_REACHED")
						break
					}
					if !page.HasMore {
						progress.Exhausted = true
						break
					}
					if page.Cursor == "" || page.Cursor != progress.Cursor {
						return m, invalid("历史来源游标不一致")
					}
				}
			}
		}
		if progress.Status != "AVAILABLE" {
			m.Missing = append(m.Missing, source)
			m.Limitations = addIssue(m.Limitations, source+"_"+progress.Status)
		}
		m.Progress = append(m.Progress, progress)
		m.Sources = append(m.Sources, model.AnalysisSourceCoverage{Source: source, Start: r.Start, End: r.End, ReadAt: progress.ReadAt, Watermark: progress.Cursor, Complete: false, Version: ProjectionAlgorithmVersion, Reason: progress.Status + "_HISTORICAL_COVERAGE_UNKNOWN"})
		m.Cutoff = max(m.Cutoff, progress.ReadAt)
	}
	return m, nil
}

func (s *Service) processProjection(ctx context.Context, e *analytics.Execution, p Parameters) error {
	var m projectionManifest
	checkpoint := projectionCheckpoint{}
	if e.Run.InputsFrozen {
		rows, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, e.Run.TenantID, model.AnalysisFilter{RunID: e.Run.ID, Kind: "input-manifest", Limit: 100})
		if err != nil {
			return err
		}
		found := false
		for _, o := range rows {
			if o.ID == e.Run.ID+":input-manifest" {
				if err = json.Unmarshal(o.Body, &m); err != nil {
					return err
				}
				found = true
			}
		}
		if !found {
			return model.ErrNotFound
		}
		if len(e.Run.InputHashes) != 1 || e.Run.InputHashes[0] != model.GovernanceHash(m) {
			return model.ErrAnalysisConflict
		}
		if len(e.Run.Checkpoint) > 0 && json.Unmarshal(e.Run.Checkpoint, &checkpoint) != nil {
			return model.ErrAnalysisConflict
		}
		if checkpoint.NextFact < 0 || checkpoint.NextFact > len(m.Observations) || int64(checkpoint.NextFact) != e.Run.Processed {
			return model.ErrAnalysisConflict
		}
	} else {
		var err error
		m, err = s.freezeProjection(ctx, e.Run, p)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(m)
		cp, _ := json.Marshal(checkpoint)
		if err = e.Commit(ctx, model.AnalysisBatch{ID: "projection/freeze", FreezeInputs: true, InputHashes: []string{model.GovernanceHash(m)}, Sources: m.Sources, DataCutoff: m.Cutoff, Checkpoint: cp, Status: model.AnalysisRunning, Stage: "historical-inputs-frozen", Outputs: []model.AnalysisOutput{{ID: e.Run.ID + ":input-manifest", Kind: "input-manifest", Body: body}}}); err != nil {
			return err
		}
	}
	size := max(1, s.Analysis.Limits.BatchSize)
	for at := checkpoint.NextFact; at < len(m.Observations); {
		end := min(at+size, len(m.Observations))
		out := []model.AnalysisOutput{}
		for _, o := range m.Observations[at:end] {
			body, _ := json.Marshal(o)
			out = append(out, model.AnalysisOutput{ID: e.Run.ID + ":projection-observations:" + o.ID, Kind: "projection-observations", DeviceID: o.DeviceID, Body: body})
		}
		checkpoint.NextFact = end
		checkpoint.CompletedBatches++
		cp, _ := json.Marshal(checkpoint)
		if err := e.Commit(ctx, model.AnalysisBatch{ID: fmt.Sprintf("projection/observations/%d", at), Checkpoint: cp, Processed: int64(end), Status: model.AnalysisRunning, Stage: "normalizing-history", Outputs: out}); err != nil {
			return err
		}
		at = end
	}
	stats := ProjectionStatistics{JobMode: HistoricalProjectionMode, NormalizedFacts: len(m.Observations), CompletedBatches: checkpoint.CompletedBatches, Quality: "HISTORICAL_UNRESOLVED", SourceProgress: m.Progress}
	for _, progress := range m.Progress {
		stats.MessageScanned += progress.MessageScanned
		stats.OmittedMessages += progress.OmittedMessages
	}
	raw, _ := json.Marshal(stats)
	snap := model.AnalysisSnapshot{ID: e.Run.ID + ":snapshot", InputHashes: []string{model.GovernanceHash(m)}, DataCutoff: m.Cutoff, Sources: m.Sources, InitialStateQuality: "HISTORICAL_UNRESOLVED", Statistics: raw, Limitations: m.Limitations, MissingSources: m.Missing, AffectedIntervals: m.Sources, UncomputableMetrics: []string{"原始规则执行与生产接受状态", "历史完整采集覆盖"}}
	cp, _ := json.Marshal(checkpoint)
	return e.Commit(ctx, model.AnalysisBatch{ID: "projection/final", Checkpoint: cp, Processed: int64(len(m.Observations)), Status: model.AnalysisPartial, Stage: "HISTORICAL_UNRESOLVED", Snapshot: &snap})
}

func (s *Service) projectionInputs(ctx context.Context, r model.AnalysisRun, ids []string) ([]model.AlarmObservation, []ProjectionReference, error) {
	facts := []model.AlarmObservation{}
	refs := []ProjectionReference{}
	if len(ids) > 8 {
		return nil, nil, invalid("历史归一化引用最多8项")
	}
	seen := map[string]bool{}
	seenFacts := map[string]string{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, nil, invalid("历史归一化引用重复或为空")
		}
		seen[id] = true
		projection, err := s.Analysis.Store.GetAnalysisRun(ctx, r.TenantID, id)
		if err != nil {
			return nil, nil, err
		}
		var p Parameters
		if json.Unmarshal(projection.Parameters, &p) != nil || projection.Kind != analytics.KindRecurring || p.JobMode != HistoricalProjectionMode || projection.AlgorithmVersion != ProjectionAlgorithmVersion || (projection.Status != model.AnalysisPartial && projection.Status != model.AnalysisSucceeded) || !projection.InputsFrozen || projection.SnapshotID == "" {
			return nil, nil, invalid("仅可引用已完成的不可变历史归一化结果")
		}
		for _, device := range projection.DeviceIDs {
			if !slices.Contains(r.DeviceIDs, device) {
				return nil, nil, analytics.ErrForbidden
			}
		}
		if r.Start < projection.Start || r.End > projection.End {
			return nil, nil, invalid("分析窗口须在历史归一化窗口之内")
		}
		snap, err := s.Analysis.Store.GetAnalysisSnapshot(ctx, r.TenantID, projection.SnapshotID)
		if err != nil {
			return nil, nil, err
		}
		if snap.RunID != projection.ID || snap.FactsHash == "" || snap.DataCutoff != projection.DataCutoff || !slices.Equal(snap.InputHashes, projection.InputHashes) {
			return nil, nil, model.ErrAnalysisConflict
		}
		refs = append(refs, ProjectionReference{RunID: id, SnapshotID: snap.ID, FactsHash: snap.FactsHash, DeviceIDs: slices.Clone(projection.DeviceIDs), Start: projection.Start, End: projection.End, DataCutoff: snap.DataCutoff, Status: projection.Status})
		for offset := 0; ; {
			rows, n, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: id, Kind: "projection-observations", Limit: 100, Offset: offset})
			if err != nil {
				return nil, nil, err
			}
			for _, output := range rows {
				var o model.AlarmObservation
				if json.Unmarshal(output.Body, &o) != nil || o.TenantID != r.TenantID || !slices.Contains(projection.DeviceIDs, o.DeviceID) || output.DeviceID != o.DeviceID || o.Acceptance != "HISTORICAL_UNRESOLVED" || o.Payload != nil || output.ID != id+":projection-observations:"+o.ID {
					return nil, nil, model.ErrAnalysisConflict
				}
				if o.EventAt < r.Start || o.EventAt >= r.End {
					continue
				}
				if hash, exists := seenFacts[o.ID]; exists {
					if hash != o.SourceContentHash {
						return nil, nil, model.ErrAnalysisConflict
					}
					continue
				}
				seenFacts[o.ID] = o.SourceContentHash
				if len(facts) >= min(50000, max(1, s.RecordLimit)) {
					return nil, nil, invalid("历史引用事实总量超过读取限制，请缩小窗口")
				}
				facts = append(facts, o)
			}
			offset += len(rows)
			if offset >= n {
				break
			}
			if len(rows) == 0 {
				return nil, nil, model.ErrAnalysisConflict
			}
		}
	}
	return facts, refs, nil
}
