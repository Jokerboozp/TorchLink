package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type member struct {
	MessageID, Property, DeviceID, Hash string
	Metadata                            model.MeasurementFact
}
type parseCounters struct {
	ObservedRaw                int `json:"observedRaw"`
	Archived                   int `json:"archived"`
	Attempted                  int `json:"attempted"`
	NotAttempted               int `json:"notAttempted"`
	LastFailed                 int `json:"lastFailed"`
	LastSucceeded              int `json:"lastSucceeded"`
	UnknownLast                int `json:"unknownLast"`
	SuccessfulStandardMessages int `json:"successfulStandardMessages"`
}
type manifest struct {
	Version           string                          `json:"version"`
	ConfigurationHash string                          `json:"configurationHash"`
	Parameters        model.MonitoringRunParameters   `json:"parameters"`
	Members           []member                        `json:"members"`
	Connections       []model.DeviceStateIntervalFact `json:"connections"`
	Dependencies      []model.DependencyFact          `json:"dependencies"`
	ParseByDevice     map[string]parseCounters        `json:"parseByDevice"`
	ParseEvidence     []model.RawParseOutcomeFact     `json:"parseEvidence"`
	QualitySnapshots  []model.AnalysisSnapshot        `json:"qualitySnapshots"`
	Sources           []model.AnalysisSourceCoverage  `json:"sources"`
	ReceivedCoverage  []continuity.Range              `json:"receivedCoverage"`
	AvailableCoverage []continuity.Range              `json:"availableCoverage"`
	ReadStart         int64                           `json:"readStart"`
	DataCutoff        int64                           `json:"dataCutoff"`
	Limitations       []string                        `json:"limitations"`
}
type checkpoint struct {
	ManifestID string `json:"manifestId"`
	NextDevice int    `json:"nextDevice"`
	Phase      string `json:"phase"`
}

func unique(items []string, item string) []string {
	if !slices.Contains(items, item) {
		items = append(items, item)
	}
	return items
}
func measurementHash(v model.MeasurementFact) (string, error) {
	v.AvailableAt = 0
	v.AvailableAtSource = ""
	v.HistoricalReconstructionQuality = ""
	return analytics.AnalysisHash(v)
}
func (s *Service) runConfigs(ctx context.Context, r model.AnalysisRun) ([]model.AnalysisConfigRevision, []model.AnalysisSnapshot, model.MonitoringRunParameters, error) {
	var p model.MonitoringRunParameters
	if err := decode(r.Parameters, &p); err != nil {
		return nil, nil, p, err
	}
	configs := []model.AnalysisConfigRevision{}
	for _, spec := range []struct {
		ids  []string
		kind string
	}{{p.ProfileRevisionIDs, model.MonitoringProfileKind}, {p.ObservationRevisionIDs, model.MonitoringObservationKind}} {
		for _, id := range spec.ids {
			v, err := s.Analysis.Store.GetAnalysisConfig(ctx, r.TenantID, id)
			if err != nil {
				return nil, nil, p, err
			}
			if v.Kind != spec.kind || v.Scope == "PERSONAL" && v.Creator != r.Creator {
				return nil, nil, p, analytics.ErrForbidden
			}
			configs = append(configs, v)
		}
	}
	quality := []model.AnalysisSnapshot{}
	for _, id := range p.QualityRunIDs {
		qr, err := s.Analysis.Store.GetAnalysisRun(ctx, r.TenantID, id)
		if err != nil {
			return nil, nil, p, err
		}
		if qr.Kind != analytics.KindDataQuality || qr.SnapshotID == "" {
			return nil, nil, p, model.ErrAnalysisConflict
		}
		for _, device := range qr.DeviceIDs {
			if !slices.Contains(r.DeviceIDs, device) {
				return nil, nil, p, analytics.ErrForbidden
			}
		}
		v, err := s.Analysis.Store.GetAnalysisSnapshot(ctx, r.TenantID, qr.SnapshotID)
		if err != nil {
			return nil, nil, p, err
		}
		quality = append(quality, v)
	}
	hash, err := configurationHash(configs, quality)
	if err != nil {
		return nil, nil, p, err
	}
	if hash != r.ConfigurationVersion {
		return nil, nil, p, model.ErrAnalysisConflict
	}
	return configs, quality, p, nil
}
func addSource(sources []model.AnalysisSourceCoverage, c model.FactSourceCoverage, basis string) []model.AnalysisSourceCoverage {
	n := model.AnalysisSourceCoverage{Source: c.Source + ":" + basis, Start: c.CoverageStart, End: c.CoverageEnd, Complete: c.Complete, ReadAt: c.ReadAt, Version: c.SourceVersion, Reason: strings.Join(append([]string{c.Status, c.HistoricalReconstructionQuality, c.BackfillStatus}, c.Limitations...), ";")}
	for i, v := range sources {
		if v.Source == n.Source {
			sources[i].Complete = v.Complete && n.Complete
			sources[i].Start = max(v.Start, n.Start)
			sources[i].End = min(v.End, n.End)
			if !strings.Contains(sources[i].Reason, n.Reason) {
				sources[i].Reason += ";" + n.Reason
			}
			return sources
		}
	}
	return append(sources, n)
}

// Availability deficiencies do not erase proved reception coverage. All other
// deficiencies (retention, failed telemetry read, unknown reception, absent
// collection metadata) continue to make the affected track unknown.
func trackCoverage(c model.FactSourceCoverage, received bool) []continuity.Range {
	if c.CoverageStart >= c.CoverageEnd || c.Status != "AVAILABLE" {
		return nil
	}
	if !c.Complete {
		for _, reason := range c.Limitations {
			if received && (reason == "FIRST_AVAILABILITY_UNKNOWN" || reason == "FIRST_AVAILABILITY_RECONSTRUCTED_AT_PROCESSING_STAGE" || reason == "HISTORICAL_COLLECTION_OR_FUTURE_RANGE_NOT_COVERED") {
				continue
			}
			if !received && reason == "HISTORICAL_COLLECTION_OR_FUTURE_RANGE_NOT_COVERED" {
				continue
			}
			return nil
		}
		if len(c.Limitations) == 0 {
			return nil
		}
	}
	return []continuity.Range{{Start: c.CoverageStart, End: c.CoverageEnd}}
}
func intersectCoverage(existing, next []continuity.Range, w continuity.Range) []continuity.Range {
	return continuity.Intersection(existing, next, w)
}
func warmCoverage(ranges []continuity.Range, readStart, ttl int64) ([]continuity.Range, bool) {
	result := []continuity.Range{}
	missing := false
	for _, r := range ranges {
		if r.Start > readStart {
			missing = true
			// Missing prefix records may still be fresh for one entire TTL.
			// Actual samples remain positive proof before this absence boundary.
			if r.Start > math.MaxInt64-ttl {
				continue
			}
			r.Start += ttl
		}
		if r.Start < r.End {
			result = append(result, r)
		}
	}
	return result, missing
}
func (s *Service) freeze(ctx context.Context, e *analytics.Execution, configs []model.AnalysisConfigRevision, quality []model.AnalysisSnapshot, p model.MonitoringRunParameters) (manifest, error) {
	r := e.Run
	m := manifest{Version: "monitoring-manifest-v1", ConfigurationHash: r.ConfigurationVersion, Parameters: p, Members: []member{}, Connections: []model.DeviceStateIntervalFact{}, Dependencies: []model.DependencyFact{}, ParseByDevice: map[string]parseCounters{}, ParseEvidence: []model.RawParseOutcomeFact{}, QualitySnapshots: quality, Sources: []model.AnalysisSourceCoverage{}, DataCutoff: time.Now().UnixMilli(), Limitations: []string{}}
	ttl := int64(0)
	properties := []string{}
	for _, v := range configs {
		if v.Kind == model.MonitoringProfileKind {
			pr, _, err := profile(v)
			if err != nil {
				return m, err
			}
			ttl = max(ttl, pr.PeriodMs+pr.ToleranceMs)
			for _, a := range pr.Attributes {
				properties = append(properties, a.ID)
			}
		}
	}
	slices.Sort(properties)
	properties = slices.Compact(properties)
	m.ReadStart = max(int64(0), r.Start-ttl)
	window := continuity.Range{Start: m.ReadStart, End: r.End}
	members := map[string]member{}
	depIncomplete := false
	measurementClipped := false
	connectionClipped := false
	dependencyClipped := false
	rawClipped := false
	var readErr error
	if s.Facts == nil {
		readErr = analytics.ErrUnsupported
	} else {
		readErr = s.Facts.AnalyticsFactsRead(ctx, r.TenantID, func(reader ports.AnalyticsFactReader) error {
			for _, basis := range []string{"EVENT", "RECEIVED", "AVAILABLE"} {
				q := model.FactQuery{DeviceIDs: r.DeviceIDs, Properties: properties, Start: m.ReadStart, End: r.End, TimeBasis: basis, AvailabilitySource: s.AvailabilitySource, Limit: min(1000, s.Analysis.Limits.BatchSize)}
				track := []continuity.Range{window}
				for {
					if err := ctx.Err(); err != nil {
						return err
					}
					page, err := reader.QueryMeasurementSeries(q)
					if err != nil {
						return err
					}
					for _, c := range append([]model.FactSourceCoverage{page.Source}, page.AdditionalSources...) {
						m.Sources = addSource(m.Sources, c, basis)
						m.DataCutoff = max(m.DataCutoff, c.ReadAt)
						track = intersectCoverage(track, trackCoverage(c, basis == "RECEIVED"), window)
					}
					for _, v := range page.Items {
						key := v.DeviceID + "\x00" + v.ID
						if _, ok := members[key]; ok {
							continue
						}
						if len(members) >= s.recordLimit() {
							measurementClipped = true
							break
						}
						hash, err := measurementHash(v)
						if err != nil {
							return err
						}
						v.Value = nil
						members[key] = member{v.MessageID, v.Property, v.DeviceID, hash, v}
					}
					if len(members) >= s.recordLimit() && page.HasMore {
						measurementClipped = true
						break
					}
					if !page.HasMore {
						break
					}
					if page.Cursor == "" || page.Cursor == q.Cursor {
						return invalid("测量分页游标没有推进")
					}
					q.Cursor = page.Cursor
				}
				if basis == "RECEIVED" {
					m.ReceivedCoverage = track
				}
				if basis == "AVAILABLE" {
					m.AvailableCoverage = track
				}
			}
			q := model.FactQuery{DeviceIDs: r.DeviceIDs, Start: r.Start, End: r.End, Limit: min(1000, s.Analysis.Limits.BatchSize)}
			for {
				page, err := reader.ListDeviceStateIntervals(q)
				if err != nil {
					return err
				}
				m.Sources = addSource(m.Sources, page.Source, "CONNECTION")
				m.DataCutoff = max(m.DataCutoff, page.Source.ReadAt)
				for _, v := range page.Items {
					if len(m.Connections) >= s.recordLimit() {
						connectionClipped = true
						break
					}
					if v.State != nil {
						v.State = &model.DeviceState{TenantID: r.TenantID, DeviceID: v.DeviceID, ConnectionStatus: v.State.ConnectionStatus, StatusSource: v.State.StatusSource, Reason: v.State.Reason}
					}
					m.Connections = append(m.Connections, v)
				}
				if len(m.Connections) >= s.recordLimit() && page.HasMore {
					connectionClipped = true
					break
				}
				if !page.HasMore {
					break
				}
				if page.Cursor == "" || page.Cursor == q.Cursor {
					return invalid("连接分页游标没有推进")
				}
				q.Cursor = page.Cursor
			}
			q.Cursor = ""
			for {
				page, err := reader.GetDependencySnapshot(q)
				if err != nil {
					return err
				}
				m.Sources = addSource(m.Sources, page.Source, "DEPENDENCY")
				m.DataCutoff = max(m.DataCutoff, page.Source.ReadAt)
				depIncomplete = depIncomplete || !page.Complete
				for _, v := range page.Items {
					if len(m.Dependencies) >= s.recordLimit() {
						dependencyClipped = true
						m.Limitations = unique(m.Limitations, "DEPENDENCY_READ_CAP_REACHED")
						depIncomplete = true
						break
					}
					if v.Quality == "RECORDED" {
						v.Quality = "KNOWN"
					}
					m.Dependencies = append(m.Dependencies, v)
				}
				if len(m.Dependencies) >= s.recordLimit() && page.HasMore {
					dependencyClipped = true
					m.Limitations = unique(m.Limitations, "DEPENDENCY_READ_CAP_REACHED")
					depIncomplete = true
					break
				}
				if !page.HasMore {
					break
				}
				if page.Cursor == "" || page.Cursor == q.Cursor {
					return invalid("依赖分页游标没有推进")
				}
				q.Cursor = page.Cursor
			}
			q.Cursor = ""
			rawCount := 0
			for {
				page, err := reader.ListRawParseOutcomes(q)
				if err != nil {
					return err
				}
				m.Sources = addSource(m.Sources, page.Source, "RAW")
				m.DataCutoff = max(m.DataCutoff, page.Source.ReadAt)
				for _, v := range page.Items {
					if rawCount >= s.recordLimit() {
						rawClipped = true
						m.Limitations = unique(m.Limitations, "RAW_READ_CAP_REACHED")
						break
					}
					c := m.ParseByDevice[v.DeviceID]
					c.ObservedRaw++
					if v.ArchivedAt > 0 {
						c.Archived++
					}
					if v.ParseAttemptedAt > 0 {
						c.Attempted++
					} else {
						c.NotAttempted++
					}
					switch v.Outcome {
					case "LAST_ATTEMPT_FAILED":
						c.LastFailed++
						if len(m.ParseEvidence) < 200 {
							v.ParseError = ""
							m.ParseEvidence = append(m.ParseEvidence, v)
						}
					case "STANDARD_SAVED":
						c.LastSucceeded++
					case "ATTEMPT_OUTCOME_UNKNOWN":
						c.UnknownLast++
					}
					c.SuccessfulStandardMessages += v.SuccessfulStandardMessages
					m.ParseByDevice[v.DeviceID] = c
					rawCount++
				}
				if rawCount >= s.recordLimit() && page.HasMore {
					rawClipped = true
					m.Limitations = unique(m.Limitations, "RAW_READ_CAP_REACHED")
					break
				}
				if !page.HasMore {
					break
				}
				if page.Cursor == "" || page.Cursor == q.Cursor {
					return invalid("原文分页游标没有推进")
				}
				q.Cursor = page.Cursor
			}
			return nil
		})
	}
	if readErr != nil {
		if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
			return m, readErr
		}
		m.Members = nil
		m.Connections = nil
		m.Dependencies = nil
		m.ParseByDevice = map[string]parseCounters{}
		m.ParseEvidence = nil
		members = map[string]member{}
		m.ReceivedCoverage = nil
		m.AvailableCoverage = nil
		depIncomplete = true
		m.Limitations = unique(m.Limitations, "SOURCE_QUERY_FAILED_OR_UNAVAILABLE")
		m.Sources = []model.AnalysisSourceCoverage{{Source: "monitoring_facts", Start: r.Start, End: r.End, Complete: false, ReadAt: m.DataCutoff, Reason: "SOURCE_QUERY_FAILED_OR_UNAVAILABLE"}}
	}
	for _, v := range members {
		m.Members = append(m.Members, v)
	}
	var receivedWarmup, availableWarmup bool
	m.ReceivedCoverage, receivedWarmup = warmCoverage(m.ReceivedCoverage, m.ReadStart, ttl)
	m.AvailableCoverage, availableWarmup = warmCoverage(m.AvailableCoverage, m.ReadStart, ttl)
	if receivedWarmup || availableWarmup {
		m.Limitations = unique(m.Limitations, "SOURCE_WARMUP_NOT_COVERED")
	}
	slices.SortFunc(m.Members, func(a, b member) int {
		return strings.Compare(a.DeviceID+"\x00"+a.MessageID+"\x00"+a.Property, b.DeviceID+"\x00"+b.MessageID+"\x00"+b.Property)
	})
	if measurementClipped {
		m.Limitations = unique(m.Limitations, "MEASUREMENT_READ_CAP_REACHED")
		m.ReceivedCoverage = nil
		m.AvailableCoverage = nil
	}
	if connectionClipped {
		m.Limitations = unique(m.Limitations, "CONNECTION_READ_CAP_REACHED")
	}
	if depIncomplete && s.Catalog != nil {
		at := time.Now().UnixMilli()
		m.DataCutoff = max(m.DataCutoff, at)
		for _, id := range r.DeviceIDs {
			d, err := s.Catalog.GetManagedDevice(ctx, r.TenantID, id)
			if err != nil {
				m.Limitations = unique(m.Limitations, "CURRENT_DEPENDENCY_QUERY_UNAVAILABLE")
				continue
			}
			deps := []struct {
				kind, id string
				version  int64
			}{{"parent-device", d.GatewayID, 0}}
			if d.ConnectorProfileID != "" {
				p, err := s.Catalog.GetDeviceAccessProfile(ctx, r.TenantID, d.ConnectorProfileID)
				ownerAllowed := p.DeviceID == id || (p.DeviceID == d.GatewayID && slices.Contains(r.DeviceIDs, p.DeviceID))
				if err != nil || p.TenantID != r.TenantID || !ownerAllowed || p.ID != d.ConnectorProfileID {
					m.Limitations = unique(m.Limitations, "CURRENT_ACCESS_PROFILE_UNAVAILABLE_OR_NOT_OWNED")
				} else {
					deps = append(deps, struct {
						kind, id string
						version  int64
					}{"access-profile", p.ID, p.UpdatedAt})
					if p.CollectorID != "" {
						deps = append(deps, struct {
							kind, id string
							version  int64
						}{"collector", p.CollectorID, p.UpdatedAt})
					}
				}
			}
			for _, dep := range deps {
				if dep.id == "" || dep.kind == "parent-device" && !slices.Contains(r.DeviceIDs, dep.id) {
					continue
				}
				if len(m.Dependencies) >= s.recordLimit() {
					dependencyClipped = true
					m.Limitations = unique(m.Limitations, "DEPENDENCY_READ_CAP_REACHED")
					break
				}
				hash, _ := analytics.AnalysisHash([]any{id, dep.kind, dep.id, at})
				m.Dependencies = append(m.Dependencies, model.DependencyFact{ID: "current:" + hash, DeviceID: id, Kind: dep.kind, ResourceID: dep.id, EffectiveFrom: at, EffectiveTo: at, RecordedAt: at, ResourceVersion: dep.version, Quality: "CURRENT_SNAPSHOT"})
			}
		}
		m.Limitations = unique(m.Limitations, "CURRENT_DEPENDENCY_ASSUMPTION_ONLY")
	}
	if len(m.Sources) == 0 {
		m.Sources = []model.AnalysisSourceCoverage{{Source: "monitoring_facts", Start: r.Start, End: r.End, Complete: false, ReadAt: m.DataCutoff, Reason: "NO_SOURCE_COVERAGE"}}
	}
	if measurementClipped || connectionClipped || dependencyClipped || rawClipped {
		for i := range m.Sources {
			basis := m.Sources[i].Source
			if !measurementClipped && !(connectionClipped && strings.HasSuffix(basis, ":CONNECTION")) && !(dependencyClipped && strings.HasSuffix(basis, ":DEPENDENCY")) && !(rawClipped && strings.HasSuffix(basis, ":RAW")) {
				continue
			}
			m.Sources[i].Complete = false
			m.Sources[i].Reason += ";READ_CAP_REACHED"
		}
	}
	id := r.ID + ":input-manifest"
	body, _ := json.Marshal(m)
	hash, err := analytics.AnalysisHash(m)
	if err != nil {
		return m, err
	}
	cp, _ := json.Marshal(checkpoint{ManifestID: id, Phase: "frozen"})
	err = e.Commit(ctx, model.AnalysisBatch{ID: "freeze", FreezeInputs: true, InputHashes: []string{hash}, Sources: m.Sources, DataCutoff: m.DataCutoff, Checkpoint: cp, Processed: int64(len(m.Members)), Status: model.AnalysisRunning, Stage: "inputs-frozen", Outputs: []model.AnalysisOutput{{ID: id, Kind: "input-manifest", Body: body}}})
	return m, err
}
func (s *Service) loadManifest(ctx context.Context, r model.AnalysisRun) (manifest, error) {
	page, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "input-manifest", Limit: 100})
	if err != nil {
		return manifest{}, err
	}
	for _, v := range page {
		if v.ID == r.ID+":input-manifest" {
			var m manifest
			if err = json.Unmarshal(v.Body, &m); err != nil {
				return m, err
			}
			hash, err := analytics.AnalysisHash(m)
			if err != nil || len(r.InputHashes) != 1 || hash != r.InputHashes[0] || m.ConfigurationHash != r.ConfigurationVersion {
				return m, model.ErrAnalysisConflict
			}
			return m, nil
		}
	}
	return manifest{}, model.ErrNotFound
}
func (s *Service) readMembers(ctx context.Context, r model.AnalysisRun, members []member) ([]model.MeasurementFact, []string, error) {
	if s.Facts == nil {
		return nil, []string{"FROZEN_SOURCE_UNAVAILABLE"}, nil
	}
	found := map[string]model.MeasurementFact{}
	issues := []string{}
	err := s.Facts.AnalyticsFactsRead(ctx, r.TenantID, func(reader ports.AnalyticsFactReader) error {
		for offset := 0; offset < len(members); offset += 1000 {
			batch := members[offset:min(len(members), offset+1000)]
			ids := make([]model.MeasurementIdentity, len(batch))
			for i, m := range batch {
				ids[i] = model.MeasurementIdentity{MessageID: m.MessageID, Property: m.Property}
			}
			q := model.FactQuery{DeviceIDs: r.DeviceIDs, Start: 0, End: math.MaxInt64, Members: ids, AvailabilitySource: s.AvailabilitySource, Limit: 1000}
			for {
				page, err := reader.QueryMeasurementSeries(q)
				if err != nil {
					return err
				}
				for _, v := range page.Items {
					found[v.DeviceID+"\x00"+v.ID] = v
				}
				if !page.HasMore {
					break
				}
				if q.Cursor == page.Cursor || page.Cursor == "" {
					return invalid("固定成员分页游标没有推进")
				}
				q.Cursor = page.Cursor
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, err
		}
		return nil, []string{"FROZEN_SOURCE_QUERY_FAILED"}, nil
	}
	out := []model.MeasurementFact{}
	for _, m := range members {
		v, ok := found[m.DeviceID+"\x00"+m.Metadata.ID]
		if !ok || v.HistoricalReconstructionQuality == "SOURCE_RECORD_MISSING" {
			issues = unique(issues, "FROZEN_SOURCE_EXPIRED_OR_MISSING")
			continue
		}
		hash, err := measurementHash(v)
		if err != nil {
			return nil, nil, err
		}
		if hash != m.Hash {
			issues = unique(issues, "FROZEN_SOURCE_VERSION_CHANGED")
			continue
		}
		frozen := m.Metadata
		frozen.Value = v.Value
		out = append(out, frozen)
	}
	return out, issues, nil
}
