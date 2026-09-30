package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"strings"
)

type frozenFault struct {
	Cycle                FaultCycle `json:"cycle"`
	AssessmentRevisionID string     `json:"assessmentRevisionId"`
	SourceEventID        string     `json:"sourceEventId"`
	MessageID            string     `json:"messageId,omitempty"`
	RawMessageID         string     `json:"rawMessageId,omitempty"`
}
type frozenManifest struct {
	Version             string                          `json:"version"`
	Business            businessInputs                  `json:"business"`
	DataCutoff          int64                           `json:"dataCutoff"`
	States              []model.DeviceStateIntervalFact `json:"states"`
	Faults              []frozenFault                   `json:"faults"`
	FaultSourceKnown    []model.FactRange               `json:"faultSourceKnown"`
	Contexts            []model.AnalysisConfigRevision  `json:"contexts"`
	Costs               []model.AnalysisConfigRevision  `json:"costs,omitempty"`
	Sources             []model.AnalysisSourceCoverage  `json:"sources"`
	Confounders         []string                        `json:"confounders"`
	Limitations         []string                        `json:"limitations"`
	RepeatedMaintenance int                             `json:"repeatedMaintenance"`
}

func jsonBody(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func sourceCoverage(c model.FactSourceCoverage) model.AnalysisSourceCoverage {
	return model.AnalysisSourceCoverage{Source: c.Source, Start: c.CoverageStart, End: c.CoverageEnd, Complete: c.Complete, ReadAt: c.ReadAt, Version: c.SourceVersion, Reason: strings.Join(append([]string{c.Status, c.HistoricalReconstructionQuality, c.BackfillStatus}, c.Limitations...), ";")}
}
func recordSource(m *frozenManifest, c model.FactSourceCoverage) {
	v := sourceCoverage(c)
	for i, old := range m.Sources {
		if old.Source == v.Source {
			m.Sources[i].Complete = old.Complete && v.Complete
			m.Sources[i].Start = max(old.Start, v.Start)
			m.Sources[i].End = min(old.End, v.End)
			return
		}
	}
	m.Sources = append(m.Sources, v)
}

// Coverage can be complete within its actual collection/retention span even
// when the deliberately broad ledger query starts before collection began.
func faultCoverage(c model.FactSourceCoverage) []model.FactRange {
	if c.Status != "AVAILABLE" || c.CoverageEnd <= c.CoverageStart {
		return nil
	}
	for _, v := range c.Limitations {
		if v != "HISTORICAL_COLLECTION_OR_FUTURE_RANGE_NOT_COVERED" {
			return nil
		}
	}
	if !c.Complete && len(c.Limitations) == 0 {
		return nil
	}
	return []model.FactRange{{Start: c.CoverageStart, End: c.CoverageEnd}}
}
func (s *Service) allConfigs(ctx context.Context, a analytics.Actor, kind string) ([]model.AnalysisConfigRevision, error) {
	out := []model.AnalysisConfigRevision{}
	for offset := 0; ; {
		page, total, err := s.List(ctx, a, kind, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		offset += len(page)
		if offset >= total {
			return out, nil
		}
		if offset >= s.limit() || len(page) == 0 {
			return nil, invalid("配置集合读取保护上限")
		}
	}
}
func (s *Service) freezeObservation(ctx context.Context, e *analytics.Execution, b businessInputs) (frozenManifest, error) {
	r := e.Run
	a := runActor(r)
	m := frozenManifest{Version: "maintenance-facts-v1", Business: b, DataCutoff: s.now(), States: []model.DeviceStateIntervalFact{}, Faults: []frozenFault{}, FaultSourceKnown: []model.FactRange{}, Sources: []model.AnalysisSourceCoverage{}, Confounders: []string{}, Limitations: []string{}, Contexts: []model.AnalysisConfigRevision{}, Costs: []model.AnalysisConfigRevision{}}
	for _, v := range b.Configs {
		m.Sources = append(m.Sources, model.AnalysisSourceCoverage{Source: "business:" + v.Kind, Start: r.Start, End: r.End, Complete: true, ReadAt: m.DataCutoff, Version: v.ID, Watermark: v.Hash})
		if v.Kind == ContextKind {
			m.Contexts = append(m.Contexts, v)
		}
	}
	assessments, err := s.allConfigs(ctx, a, FaultAssessmentKind)
	if err != nil {
		return m, err
	}
	assessed := map[string][]model.AnalysisConfigRevision{}
	for _, v := range assessments {
		if !subset(v.DeviceIDs, r.DeviceIDs) {
			continue
		}
		var f model.MaintenanceFaultAssessment
		if decode(v.Body, &f) != nil {
			return m, invalid("故障核实记录损坏")
		}
		if f.Status == "CONFIRMED" {
			assessed[f.AlarmID] = append(assessed[f.AlarmID], v)
		}
	}
	created := map[string]frozenFault{}
	if s.Facts == nil {
		m.Limitations = unique(m.Limitations, "FACT_SOURCE_UNAVAILABLE")
	} else {
		err = s.Facts.AnalyticsFactsRead(ctx, r.TenantID, func(reader ports.AnalyticsFactReader) error {
			end := min(r.End, m.DataCutoff+1)
			if end > r.Start {
				q := model.FactQuery{DeviceIDs: r.DeviceIDs, Start: r.Start, End: end, Limit: 1000}
				for n := 0; ; {
					page, er := reader.ListDeviceStateIntervals(q)
					if er != nil {
						if errors.Is(er, context.DeadlineExceeded) {
							return er
						}
						m.Limitations = unique(m.Limitations, "STATE_QUERY_FAILED")
						break
					}
					recordSource(&m, page.Source)
					remain := s.limit() - n
					if len(page.Items) > remain {
						page.Items = page.Items[:remain]
						m.Limitations = unique(m.Limitations, "STATE_RECORD_CAP_REACHED")
					}
					m.States = append(m.States, page.Items...)
					n += len(page.Items)
					if !page.HasMore {
						break
					}
					if n >= s.limit() || page.Cursor == "" || page.Cursor == q.Cursor {
						m.Limitations = unique(m.Limitations, "STATE_RECORD_CAP_REACHED")
						break
					}
					q.Cursor = page.Cursor
					q.SourceVersion = page.Source.SourceVersion
				}
			}
			// Transaction ledger is ordered by processing clock. Query its whole
			// covered prefix to capture late reports, then use the embedded immutable
			// StandardMessage event clock for physical instance attribution.
			q := model.FactQuery{DeviceIDs: r.DeviceIDs, Start: 0, End: m.DataCutoff + 1, Limit: 1000}
			known := []model.FactRange{}
			sourceSafe := true
			for n := 0; ; {
				page, er := reader.ListAlarmReportEvents(q)
				if er != nil {
					if errors.Is(er, context.DeadlineExceeded) {
						return er
					}
					m.Limitations = unique(m.Limitations, "FAULT_QUERY_FAILED")
					sourceSafe = false
					break
				}
				recordSource(&m, page.Source)
				if n == 0 {
					known = faultCoverage(page.Source)
				}
				remain := s.limit() - n
				if len(page.Items) > remain {
					page.Items = page.Items[:remain]
					sourceSafe = false
					m.Limitations = unique(m.Limitations, "FAULT_RECORD_CAP_REACHED")
				}
				for _, event := range page.Items {
					if event.Type != "ALARM_CREATED" {
						continue
					}
					var alarm model.Alarm
					if json.Unmarshal(event.Body, &alarm) != nil || alarm.ID != event.ResourceID || alarm.DeviceID != event.DeviceID || !slices.Contains(r.DeviceIDs, event.DeviceID) {
						sourceSafe = false
						m.Limitations = unique(m.Limitations, "FAULT_EVENT_BODY_UNKNOWN")
						continue
					}
					var msg model.StandardMessage
					encoded, _ := json.Marshal(alarm.Details["message"])
					json.Unmarshal(encoded, &msg)
					if event.EventAt > 0 && event.MessageID != "" && event.MessageID == alarm.TriggerID {
						msg = model.StandardMessage{MessageID: event.MessageID, RawMessageID: event.RawMessageID, Timestamp: event.EventAt, DeviceID: event.DeviceID}
					}
					if msg.MessageID == "" || msg.MessageID != alarm.TriggerID || msg.Timestamp <= 0 || msg.DeviceID != alarm.DeviceID {
						sourceSafe = false
						m.Limitations = unique(m.Limitations, "FAULT_EVENT_CLOCK_UNKNOWN")
						continue
					}
					if msg.Timestamp < r.Start || msg.Timestamp >= r.End {
						continue
					}
					created[alarm.ID] = frozenFault{Cycle: FaultCycle{ID: alarm.ID, DeviceID: alarm.DeviceID, ComponentID: alarm.ComponentID, EventAt: msg.Timestamp, Type: "OTHER", Classification: "UNKNOWN", Confirmation: "UNCONFIRMED"}, SourceEventID: event.SourceEventID, MessageID: msg.MessageID, RawMessageID: msg.RawMessageID}
				}
				n += len(page.Items)
				if !page.HasMore {
					break
				}
				if n >= s.limit() || page.Cursor == "" || page.Cursor == q.Cursor {
					sourceSafe = false
					m.Limitations = unique(m.Limitations, "FAULT_RECORD_CAP_REACHED")
					break
				}
				q.Cursor = page.Cursor
				q.SourceVersion = page.Source.SourceVersion
			}
			if sourceSafe {
				m.FaultSourceKnown = known
			}
			for id, v := range created {
				revisions := assessed[id]
				if len(revisions) == 0 {
					sourceSafe = false
					m.Limitations = unique(m.Limitations, "FAULT_CLASSIFICATION_UNCONFIRMED")
				}
				for _, revision := range revisions {
					var f model.MaintenanceFaultAssessment
					decode(revision.Body, &f)
					admissionCode := "FAULT"
					if p := b.Parameters.Observation; p.AdmissionRevisionID != "" {
						var admission model.MaintenanceAdmissionRecord
						decode(configByID(b, p.AdmissionRevisionID).Body, &admission)
						admissionCode = admission.FaultType
					}
					cycle := v
					cycle.AssessmentRevisionID = revision.ID
					cycle.Cycle.Type = f.Type
					if admissionCode != "FAULT" && f.FaultCode != admissionCode {
						cycle.Cycle.Type = "OTHER"
					}
					cycle.Cycle.Classification = f.Classification
					cycle.Cycle.Confirmation = f.Status
					m.Faults = append(m.Faults, cycle)
					m.Business.Configs = append(m.Business.Configs, revision)
				}
				if len(revisions) == 0 {
					m.Faults = append(m.Faults, v)
				}
			}
			if !sourceSafe {
				m.FaultSourceKnown = nil
			}
			// A configuration change is a confounder, while absent historical
			// configuration coverage is a named limitation, never proof of stability.
			q = model.FactQuery{DeviceIDs: r.DeviceIDs, Start: r.Start, End: max(r.Start+1, min(r.End, m.DataCutoff+1)), Limit: 1000}
			for n := 0; ; {
				page, er := reader.GetConfigurationHistory(q)
				if er != nil {
					if errors.Is(er, context.DeadlineExceeded) {
						return er
					}
					m.Limitations = unique(m.Limitations, "CONFIGURATION_HISTORY_QUERY_FAILED")
					break
				}
				recordSource(&m, page.Source)
				if !page.Complete {
					m.Limitations = unique(m.Limitations, "CONFIGURATION_HISTORY_INCOMPLETE")
				}
				for _, v := range page.Items {
					if !v.InitialSnapshot && v.OccurredAt > r.Start && v.OccurredAt < r.End {
						m.Confounders = unique(m.Confounders, "CONFIGURATION_CHANGED:"+v.Source)
					}
				}
				n += len(page.Items)
				if !page.HasMore {
					break
				}
				if n >= s.limit() || page.Cursor == "" || page.Cursor == q.Cursor {
					m.Limitations = unique(m.Limitations, "CONFIGURATION_RECORD_CAP_REACHED")
					break
				}
				q.Cursor = page.Cursor
				q.SourceVersion = page.Source.SourceVersion
			}
			return nil
		})
		if err != nil {
			return m, err
		}
	}
	for _, v := range m.Contexts {
		var c model.OperatingContext
		decode(v.Body, &c)
		if c.Confirmed && c.End > r.Start && c.Start < r.End && !slices.Contains([]string{"OPERATING_INTENSITY", "PLANNED_STOP", "CONFIRMED_TEST", "PLATFORM_OBSERVATION_UNAVAILABLE"}, c.Kind) {
			m.Confounders = unique(m.Confounders, "RECORDED_CONTEXT_CHANGE:"+c.Kind)
		}
	}
	records, err := s.allConfigs(ctx, a, InterventionKind)
	if err != nil {
		return m, err
	}
	beforeResource := configByID(b, b.Parameters.Observation.BeforeAssetRevisionID).ResourceID
	afterResource := configByID(b, b.Parameters.Observation.AfterAssetRevisionID).ResourceID
	for _, v := range records {
		if !subset(v.DeviceIDs, r.DeviceIDs) {
			continue
		}
		var work model.MaintenanceIntervention
		decode(v.Body, &work)
		if work.Status != "COMPLETED" {
			continue
		}
		asset, err := s.Revision(ctx, a, AssetKind, work.AssetRevisionID)
		if err != nil {
			return m, err
		}
		if (asset.ResourceID == beforeResource || asset.ResourceID == afterResource) && v.ResourceID != configByID(b, b.Parameters.Observation.InterventionRevisionID).ResourceID && work.EndedAt >= r.Start && work.EndedAt < r.End {
			m.RepeatedMaintenance++
		}
	}
	if b.Parameters.UseFinance {
		costs, err := s.allConfigs(ctx, a, CostKind)
		if err != nil {
			return m, err
		}
		intervention := configByID(b, b.Parameters.Observation.InterventionRevisionID).ResourceID
		for _, v := range costs {
			if !subset(v.DeviceIDs, r.DeviceIDs) {
				continue
			}
			var cost model.MaintenanceCost
			decode(v.Body, &cost)
			if cost.Type == "ACTUAL" && (cost.SourceKind == AssetKind && (cost.SourceID == beforeResource || cost.SourceID == afterResource) || cost.SourceKind == InterventionKind && cost.SourceID == intervention) {
				m.Costs = append(m.Costs, v)
			}
		}
	}
	slices.SortFunc(m.Faults, func(a, b frozenFault) int {
		if a.Cycle.EventAt != b.Cycle.EventAt {
			if a.Cycle.EventAt < b.Cycle.EventAt {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Cycle.ID+":"+a.AssessmentRevisionID, b.Cycle.ID+":"+b.AssessmentRevisionID)
	})
	return m, nil
}
func (s *Service) saveManifest(ctx context.Context, e *analytics.Execution, m frozenManifest) error {
	body := jsonBody(m)
	if len(body) > 64<<20 {
		return invalid("固定事实清单超过64MiB保护上限")
	}
	hash, err := analytics.AnalysisHash(m)
	if err != nil {
		return err
	}
	return e.Commit(ctx, model.AnalysisBatch{ID: "maintenance-freeze", FreezeInputs: true, InputHashes: []string{e.Run.ConfigurationVersion, hash}, Sources: m.Sources, DataCutoff: m.DataCutoff, Status: model.AnalysisRunning, Stage: "fixed-inputs", Checkpoint: jsonBody(map[string]any{"phase": "frozen"}), Outputs: []model.AnalysisOutput{{ID: e.Run.ID + ":input-manifest", Kind: "input-manifest", Body: body}}})
}
func (s *Service) loadManifest(ctx context.Context, r model.AnalysisRun) (frozenManifest, error) {
	var m frozenManifest
	page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "input-manifest", Limit: 2})
	if err != nil {
		return m, err
	}
	if total != 1 || len(page) != 1 {
		return m, model.ErrAnalysisConflict
	}
	if err = json.Unmarshal(page[0].Body, &m); err != nil {
		return m, err
	}
	hash, err := analytics.AnalysisHash(m)
	if err != nil {
		return m, err
	}
	if m.Version != "maintenance-facts-v1" || m.Business.SourceConfigurationHash != r.ConfigurationVersion || len(r.InputHashes) != 2 || r.InputHashes[1] != hash {
		return m, fmt.Errorf("%w: fixed fact manifest mismatch", model.ErrAnalysisConflict)
	}
	return m, nil
}
