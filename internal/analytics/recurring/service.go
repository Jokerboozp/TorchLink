package recurring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"time"
)

const AlgorithmVersion = "recurring-evidence-v1"
const day = int64(24 * time.Hour / time.Millisecond)
const seedMaxAge = int64(24 * time.Hour / time.Millisecond)

type DiscoveryPolicy struct {
	Window               int64  `json:"window"`
	MinReportCount       int    `json:"minReportCount"`
	MinKnownCycleStarts  int    `json:"minKnownCycleStarts"`
	MinOpenDuration      int64  `json:"minOpenDuration"`
	PostCompletionWindow int64  `json:"postCompletionWindow"`
	Version              string `json:"version"`
}
type Parameters struct {
	JobMode                           string          `json:"jobMode,omitempty"`
	HistoricalSources                 []string        `json:"historicalSources,omitempty"`
	ProjectionLimit                   int             `json:"projectionLimit,omitempty"`
	HistoricalProjectionRunIDs        []string        `json:"historicalProjectionRunIds,omitempty"`
	CaseID                            string          `json:"caseId,omitempty"`
	RoundID                           string          `json:"roundId,omitempty"`
	PlanID                            string          `json:"planId,omitempty"`
	ExpectedDataRevision              int64           `json:"expectedDataRevision,omitempty"`
	ProfileRevisionID                 string          `json:"profileRevisionId,omitempty"`
	IncludeHistoricalStandardMessages bool            `json:"includeHistoricalStandardMessages,omitempty"`
	TimeBasis                         string          `json:"timeBasis"`
	DiscoveryPolicy                   DiscoveryPolicy `json:"discoveryPolicy"`
}
type Service struct {
	Analysis          *analytics.Service
	Store             ports.AlarmGovernanceStore
	Observe           func(string)
	RecordLimit       int
	HistoricalArchive ports.GovernanceHistoricalArchiveReader
}

func NewService(a *analytics.Service, store ports.AlarmGovernanceStore) *Service {
	return &Service{Analysis: a, Store: store, RecordLimit: a.Limits.RecordLimit}
}
func (s *Service) Register() error { return s.Analysis.Register(analytics.KindRecurring, s.Process) }
func invalid(s string) error       { return fmt.Errorf("%w: %s", model.ErrAnalysisInvalid, s) }
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return invalid("输入字段或结构无效")
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return invalid("输入正文不唯一")
	}
	return nil
}
func (s *Service) ValidateCreate(ctx context.Context, a analytics.Actor, q *analytics.CreateRequest) error {
	if s.Store == nil {
		return analytics.ErrUnsupported
	}
	if q.Start < 0 || q.End <= q.Start || q.End-q.Start > s.Analysis.Limits.MaxRange.Milliseconds() {
		return invalid("时间窗口无效")
	}
	current, err := s.Analysis.Current(ctx, a)
	if err != nil || !current.Allows(analytics.KindRecurring, "POST /api/v1/alarm-governance/runs", q.DeviceIDs) {
		return analytics.ErrForbidden
	}
	p := Parameters{TimeBasis: "EVENT_AT"}
	if len(q.Parameters) > 0 {
		if err = decode(q.Parameters, &p); err != nil {
			return err
		}
	}
	if p.JobMode == HistoricalProjectionMode {
		return s.validateProjectionCreate(ctx, current, q, p)
	}
	if p.JobMode != "" || len(p.HistoricalSources) > 0 || p.ProjectionLimit != 0 {
		return invalid("归一化来源与限制仅用于历史归一化任务")
	}
	if len(p.HistoricalProjectionRunIDs) > 0 && p.TimeBasis != "EVENT_AT" {
		return invalid("历史归一化引用仅保留事件时间，无法证明原始接收或规则执行口径")
	}
	if _, _, err = s.projectionInputs(ctx, model.AnalysisRun{TenantID: a.TenantID, DeviceIDs: q.DeviceIDs, Start: q.Start, End: q.End}, p.HistoricalProjectionRunIDs); err != nil {
		return err
	}
	if p.TimeBasis != "EVENT_AT" && p.TimeBasis != "RECEIVED_AT" {
		return invalid("时间依据须为EVENT_AT或RECEIVED_AT")
	}
	d := p.DiscoveryPolicy
	if d.MinReportCount < 0 || d.MinKnownCycleStarts < 0 || d.MinOpenDuration < 0 || d.PostCompletionWindow < 0 || d.Window < 0 || d.Window > 0 && d.Window != q.End-q.Start {
		return invalid("发现策略的独立条件或窗口无效")
	}
	if p.CaseID == "" && (d.MinReportCount == 0 && d.MinKnownCycleStarts == 0 && d.MinOpenDuration == 0) {
		return invalid("请选择至少一项确定性发现条件")
	}
	err = s.Store.GovernanceRead(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		_, _, _, e := configuration(tx, *q, &p)
		return e
	})
	if err != nil {
		return err
	}
	if d.Version == "" {
		p.DiscoveryPolicy.Version = model.GovernanceHash(d)
	}
	q.Parameters, _ = json.Marshal(p)
	q.ConfigurationVersion = model.GovernanceHash(p)
	return nil
}
func configuration(tx ports.AlarmGovernanceTx, q analytics.CreateRequest, p *Parameters) (model.GovernanceConfiguration, *model.GovernanceCase, *model.ObservationPlan, error) {
	var c *model.GovernanceCase
	var plan *model.ObservationPlan
	if p.CaseID != "" {
		doc, err := tx.Get(model.GovernanceCaseKind, p.CaseID)
		if err != nil {
			return model.GovernanceConfiguration{}, nil, nil, err
		}
		v, err := model.GovernanceBody[model.GovernanceCase](doc)
		if err != nil {
			return model.GovernanceConfiguration{}, nil, nil, err
		}
		if len(q.DeviceIDs) != 1 || q.DeviceIDs[0] != v.DeviceID {
			return model.GovernanceConfiguration{}, nil, nil, analytics.ErrForbidden
		}
		if p.ExpectedDataRevision != v.DataRevision {
			return model.GovernanceConfiguration{}, nil, nil, model.ErrAnalysisConflict
		}
		if p.RoundID == "" {
			p.RoundID = v.CurrentRoundID
		}
		rd, err := tx.Get(model.GovernanceRoundKind, p.RoundID)
		if err != nil || rd.CaseID != p.CaseID {
			return model.GovernanceConfiguration{}, nil, nil, model.ErrAnalysisConflict
		}
		round, err := model.GovernanceBody[model.GovernanceRound](rd)
		if err != nil {
			return model.GovernanceConfiguration{}, nil, nil, err
		}
		if p.ProfileRevisionID != "" && p.ProfileRevisionID != round.TypeProfileRevisionID {
			return model.GovernanceConfiguration{}, nil, nil, model.ErrAnalysisConflict
		}
		p.ProfileRevisionID = round.TypeProfileRevisionID
		c = &v
		if p.PlanID != "" {
			pd, err := tx.Get(model.GovernancePlanKind, p.PlanID)
			if err != nil || pd.RoundID != p.RoundID {
				return model.GovernanceConfiguration{}, nil, nil, model.ErrAnalysisConflict
			}
			v, err := model.GovernanceBody[model.ObservationPlan](pd)
			if err != nil {
				return model.GovernanceConfiguration{}, nil, nil, err
			}
			if v.Status != "CONFIRMED" || q.Start > min(v.BeforeStart, v.AfterStart) || q.End < max(v.BeforeEnd, v.AfterEnd) {
				return model.GovernanceConfiguration{}, nil, nil, invalid("分析窗口须覆盖已确认计划的前后窗口")
			}
			plan = &v
		}
	}
	var cfg model.GovernanceConfiguration
	if p.ProfileRevisionID != "" {
		doc, err := tx.Get(model.GovernanceProfileKind, p.ProfileRevisionID)
		if err != nil {
			return cfg, c, plan, err
		}
		cfg, err = model.GovernanceBody[model.GovernanceConfiguration](doc)
		if err != nil {
			return cfg, c, plan, err
		}
		if cfg.Status != "PUBLISHED" && c == nil {
			return cfg, c, plan, invalid("发现须使用已发布报警类型版本")
		}
		if len(cfg.DeviceIDs) > 0 {
			for _, id := range q.DeviceIDs {
				if !slices.Contains(cfg.DeviceIDs, id) {
					return cfg, c, plan, analytics.ErrForbidden
				}
			}
		}
	}
	if cfg.CycleMethod == "" {
		cfg.CycleMethod = "REPORT_ONLY"
	}
	if cfg.CycleMethod == "REPORT_ONLY" && (p.DiscoveryPolicy.MinKnownCycleStarts > 0 || p.DiscoveryPolicy.MinOpenDuration > 0) {
		return cfg, c, plan, invalid("REPORT_ONLY不能启用周期发现条件")
	}
	return cfg, c, plan, nil
}
func query(r model.AnalysisRun) analytics.CreateRequest {
	return analytics.CreateRequest{DeviceIDs: r.DeviceIDs, Start: r.Start, End: r.End, Parameters: r.Parameters, ConfigurationVersion: r.ConfigurationVersion}
}

type frozenInputs struct {
	HistoricalProjections []ProjectionReference           `json:"historicalProjections,omitempty"`
	Parameters            Parameters                      `json:"parameters"`
	AllowedActivityTypes  []string                        `json:"allowedActivityTypes"`
	Profile               model.GovernanceConfiguration   `json:"profile"`
	Case                  *model.GovernanceCase           `json:"case,omitempty"`
	Plan                  *model.ObservationPlan          `json:"plan,omitempty"`
	PlanVersion           int64                           `json:"planVersion,omitempty"`
	Observations          []model.AlarmObservation        `json:"observations"`
	Seeds                 []model.AlarmObservation        `json:"seeds"`
	Resources             []model.GovernanceDocument      `json:"resources"`
	SourceRevisionVector  []model.GovernanceSourceVersion `json:"sourceRevisionVector"`
	Cutoff                int64                           `json:"cutoff"`
	Limitations           []string                        `json:"limitations"`
	Clipped               bool                            `json:"clipped"`
}

func depKeys(point model.GovernancePoint, source string, start, end int64) []model.GovernanceSourceVersion {
	out := []model.GovernanceSourceVersion{{DependencyKey: point.Key(source), BucketStart: -1}}
	if start < 0 {
		start = 0
	}
	for b := start / day * day; b < end; b += day {
		out = append(out, model.GovernanceSourceVersion{DependencyKey: point.Key(source), BucketStart: b})
	}
	return out
}
func (s *Service) freeze(ctx context.Context, r model.AnalysisRun) (m frozenInputs, err error) {
	if s.Store == nil {
		return m, analytics.ErrUnsupported
	}
	var p Parameters
	if err = decode(r.Parameters, &p); err != nil {
		return m, err
	}
	limit := min(50000, max(1, s.RecordLimit))
	projectionFacts, projectionReferences, err := s.projectionInputs(ctx, r, p.HistoricalProjectionRunIDs)
	if err != nil {
		return m, err
	}
	err = s.Store.GovernanceTransaction(ctx, r.TenantID, func(tx ports.AlarmGovernanceTx) error {
		// Serializable retries discard every previous attempt's members.
		m = frozenInputs{Parameters: p, Cutoff: time.Now().UnixMilli(), Limitations: []string{}, Observations: []model.AlarmObservation{}, Seeds: []model.AlarmObservation{}, Resources: []model.GovernanceDocument{}}
		m.HistoricalProjections = projectionReferences
		for _, fact := range projectionFacts {
			if len(m.Observations) >= limit {
				m.Clipped = true
				break
			}
			m.Observations = append(m.Observations, fact)
		}
		if len(projectionReferences) > 0 {
			m.Limitations = append(m.Limitations, "HISTORICAL_PROJECTION_ORIGINAL_ACCEPTANCE_UNKNOWN")
		}
		cfg, c, plan, e := configuration(tx, query(r), &m.Parameters)
		if e != nil {
			return e
		}
		m.Profile, m.Case, m.Plan = cfg, c, plan
		if c != nil {
			sd, e := tx.Get(model.GovernanceSceneKind, c.ScenePresetRevisionID)
			if e != nil {
				return e
			}
			scene, e := model.GovernanceBody[model.GovernanceConfiguration](sd)
			if e != nil {
				return e
			}
			for _, o := range scene.ActivityOptions {
				m.AllowedActivityTypes = append(m.AllowedActivityTypes, o.Code)
			}
		} else {
			for _, o := range cfg.ActivityOptions {
				m.AllowedActivityTypes = append(m.AllowedActivityTypes, o.Code)
			}
		}
		if plan != nil && plan.ActivityType != "" && !slices.Contains(m.AllowedActivityTypes, plan.ActivityType) {
			m.AllowedActivityTypes = append(m.AllowedActivityTypes, plan.ActivityType)
		}
		if plan != nil {
			pd, e := tx.Get(model.GovernancePlanKind, m.Parameters.PlanID)
			if e != nil {
				return e
			}
			m.PlanVersion = pd.Version
		}
		reader, ok := tx.(ports.AlarmObservationReader)
		if !ok {
			return analytics.ErrUnsupported
		}
		f := ports.AlarmObservationFilter{DeviceIDs: r.DeviceIDs, Start: r.Start, End: r.End, Limit: 1000, TimeBasis: observationBasis(p, cfg)}
		if c != nil {
			f.DeviceID = c.DeviceID
			f.ComponentID = c.ComponentID
			f.AlarmType = c.AlarmType
			f.OriginKind = c.OriginKind
			f.SignalKey = c.SignalKey
		} else {
			f.AlarmType = cfg.AlarmType
			f.OriginKind = cfg.OriginKind
			f.SignalKey = cfg.SignalKey
		}
		m.Observations = slices.DeleteFunc(m.Observations, func(o model.AlarmObservation) bool {
			return f.DeviceID != "" && o.DeviceID != f.DeviceID || f.ComponentID != "" && o.ComponentID != f.ComponentID || f.AlarmType != "" && o.AlarmType != f.AlarmType || f.OriginKind != "" && o.OriginKind != f.OriginKind || f.SignalKey != "" && o.SignalKey != f.SignalKey
		})
		if p.IncludeHistoricalStandardMessages {
			if p.TimeBasis != "EVENT_AT" {
				return invalid("历史解析输入仅保留原事件时间分布，接收口径证据须另行提供")
			}
			historical, ok := tx.(ports.GovernanceHistoricalReader)
			if !ok {
				return analytics.ErrUnsupported
			}
			hf := ports.AlarmObservationFilter{DeviceIDs: r.DeviceIDs, Start: r.Start, End: r.End, Limit: 1000}
			for {
				page, e := historical.ListGovernanceHistoricalMessages(hf)
				if e != nil {
					return e
				}
				for _, msg := range page {
					normalized, e := NormalizeHistoricalMessage(msg, nil, m.Cutoff)
					if e != nil {
						return e
					}
					for _, o := range normalized.Observations {
						if len(m.Observations) >= limit {
							m.Clipped = true
							break
						}
						o.Payload = nil
						m.Observations = append(m.Observations, o)
					}
				}
				if m.Clipped || len(page) < hf.Limit {
					break
				}
				last := page[len(page)-1]
				hf.Cursor = fmt.Sprintf("%019d:%s", last.Timestamp, last.MessageID)
			}
			m.Limitations = append(m.Limitations, "HISTORICAL_PROJECTION_ORIGINAL_ACCEPTANCE_UNKNOWN", "HISTORICAL_STANDARD_SOURCE_REQUIRES_EXPLICIT_REFRESH")
		}
		points := map[string]model.GovernancePoint{}
		if c != nil {
			points[c.Key("")] = c.GovernancePoint
		}
		for {
			page, e := reader.ListAlarmObservations(f)
			if e != nil {
				return e
			}
			for _, o := range page {
				if len(m.Observations) >= limit {
					m.Clipped = true
					break
				}
				o.Payload = nil
				m.Observations = append(m.Observations, o)
				point := model.GovernancePoint{DeviceID: o.DeviceID, ComponentID: o.ComponentID, AlarmType: o.AlarmType, OriginKind: o.OriginKind, SignalKey: o.SignalKey}
				points[point.Key("")] = point
			}
			if m.Clipped || len(page) < f.Limit {
				break
			}
			last := page[len(page)-1]
			cursor := fmt.Sprintf("%d:%s", last.TimeAt(observationBasis(p, cfg)), last.ID)
			if f.Cursor == cursor {
				return invalid("事实游标未推进")
			}
			f.Cursor = cursor
		}
		for _, kind := range []string{model.GovernanceVerificationKind, model.GovernanceActivityKind, model.GovernanceCoverageKind, model.GovernanceCauseKind, model.GovernanceMeasureKind, model.GovernanceCaseKind, model.GovernanceBusinessLinkKind} {
			gf := model.GovernanceFilter{Kind: kind, DeviceIDs: r.DeviceIDs, Limit: 100}
			for {
				page, total, e := tx.List(gf)
				if e != nil {
					return e
				}
				for _, d := range page {
					if len(m.Resources)+len(m.Observations) >= limit {
						m.Clipped = true
						break
					}
					m.Resources = append(m.Resources, d)
				}
				gf.Offset += len(page)
				if m.Clipped || gf.Offset >= total {
					break
				}
			}
		}
		for _, o := range m.Observations {
			point := pointOf(o)
			points[point.Key("")] = point
		}
		uniqueObservations := make([]model.AlarmObservation, 0, len(m.Observations))
		seenObservation := map[string]string{}
		for _, o := range m.Observations {
			if hash, exists := seenObservation[o.ID]; exists {
				if hash != o.SourceContentHash {
					return model.ErrAnalysisConflict
				}
				continue
			}
			seenObservation[o.ID] = o.SourceContentHash
			uniqueObservations = append(uniqueObservations, o)
		}
		m.Observations = uniqueObservations
		keys := []model.GovernanceSourceVersion{}
		for _, point := range points {
			sf := ports.AlarmObservationFilter{DeviceIDs: r.DeviceIDs, DeviceID: point.DeviceID, ComponentID: point.ComponentID, AlarmType: point.AlarmType, OriginKind: point.OriginKind, SignalKey: point.SignalKey, TimeBasis: observationBasis(p, cfg)}
			seed, e := reader.GetAlarmSignalSeed(sf, r.Start)
			if e == nil && seed.TimeAt(observationBasis(p, cfg)) >= r.Start-seedMaxAge {
				seed.Payload = nil
				m.Seeds = append(m.Seeds, seed)
			} else if e != nil && !errors.Is(e, model.ErrNotFound) {
				return e
			}
			for _, source := range []string{observationSource(p, cfg), "VERIFICATION", "IDENTITY"} {
				keys = append(keys, depKeys(point, source, r.Start-seedMaxAge, r.End)...)
			}
		}
		for _, id := range r.DeviceIDs {
			point := model.GovernancePoint{DeviceID: id}
			for _, source := range []string{observationSource(p, cfg), "ACTIVITY", "COVERAGE", "CONFIGURATION"} {
				keys = append(keys, depKeys(point, source, r.Start-seedMaxAge, r.End)...)
			}
		}
		m.SourceRevisionVector, e = tx.SourceVersions(keys)
		if e != nil {
			return e
		}
		m.Limitations = append(m.Limitations, "HISTORICAL_COLLECTION_COVERAGE_NOT_PROVEN", "ASSET_IDENTITY_HISTORY_UNAVAILABLE")
		if m.Clipped {
			m.Limitations = append(m.Limitations, "READ_LIMIT_REACHED")
		}
		return nil
	})
	return
}

// AuthorizeInputs checks provenance from the fixed input manifest. A later
// unrelated link cannot expand an old report's inherited permissions.
func (s *Service) AuthorizeInputs(ctx context.Context, r model.AnalysisRun, check func(model.GovernanceDocument) error) error {
	if r.InputsFrozen {
		rows, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, r.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "input-manifest", Limit: 100})
		if err != nil {
			return err
		}
		for _, v := range rows {
			if v.ID == r.ID+":input-manifest" {
				var m frozenInputs
				if err = json.Unmarshal(v.Body, &m); err != nil {
					return err
				}
				for _, d := range m.Resources {
					if d.Kind == model.GovernanceBusinessLinkKind {
						if err = check(d); err != nil {
							return analytics.ErrForbidden
						}
					}
				}
				for _, ref := range m.HistoricalProjections {
					if err = check(model.GovernanceDocument{Kind: "historical-projection", ID: ref.RunID, DeviceIDs: ref.DeviceIDs}); err != nil {
						return analytics.ErrForbidden
					}
				}
				return nil
			}
		}
		return model.ErrNotFound
	}
	var p Parameters
	if err := json.Unmarshal(r.Parameters, &p); err != nil {
		return err
	}
	for _, id := range p.HistoricalProjectionRunIDs {
		projection, err := s.Analysis.Store.GetAnalysisRun(ctx, r.TenantID, id)
		if err != nil {
			return err
		}
		for _, device := range projection.DeviceIDs {
			if !slices.Contains(r.DeviceIDs, device) {
				return analytics.ErrForbidden
			}
		}
		if err = check(model.GovernanceDocument{Kind: "historical-projection", ID: id, DeviceIDs: projection.DeviceIDs}); err != nil {
			return analytics.ErrForbidden
		}
	}
	if p.CaseID == "" {
		return nil
	}
	var d model.GovernanceDocument
	err := s.Store.GovernanceRead(ctx, r.TenantID, func(tx ports.AlarmGovernanceTx) error {
		var e error
		d, e = tx.Get(model.GovernanceCaseKind, p.CaseID)
		return e
	})
	if err != nil {
		return err
	}
	if err = check(d); err != nil {
		return analytics.ErrForbidden
	}
	return nil
}

func observationBasis(p Parameters, cfg model.GovernanceConfiguration) string {
	if p.TimeBasis == "RECEIVED_AT" {
		return p.TimeBasis
	}
	if cfg.CycleMethod == "RECORDED_RULE_LIFECYCLE" {
		return "EVALUATION_AT"
	}
	return "EVENT_AT"
}

func observationSource(p Parameters, cfg model.GovernanceConfiguration) string {
	switch observationBasis(p, cfg) {
	case "RECEIVED_AT":
		return "OBSERVATION_RECEIVED"
	case "EVALUATION_AT":
		return "OBSERVATION_EVALUATION"
	default:
		return "OBSERVATION"
	}
}
