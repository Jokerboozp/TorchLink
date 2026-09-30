package recurring

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
)

type PointMetrics struct {
	CycleMethod           string                     `json:"cycleMethod"`
	Point                 model.GovernancePoint      `json:"point"`
	Period                string                     `json:"period"`
	Window                Interval                   `json:"window"`
	ReportCount           int                        `json:"reportCount"`
	KnownStarts           int                        `json:"knownStarts"`
	OpenCycles            int                        `json:"openCycles"`
	BoundaryUnknown       int                        `json:"boundaryUnknown"`
	Cycles                []model.AlarmCycleRevision `json:"cycles"`
	Activity              ActivityMetrics            `json:"activity"`
	VerificationCount     int                        `json:"verificationCount"`
	UnverifiedCount       int                        `json:"unverifiedCount"`
	NewStartsPer1000Hours *float64                   `json:"newStartsPer1000Hours"`
	Limitations           []string                   `json:"limitations"`
}
type Statistics struct {
	InputHash            string                          `json:"inputHash"`
	SourceRevisionVector []model.GovernanceSourceVersion `json:"sourceRevisionVector"`
	DataRevision         int64                           `json:"dataRevision"`
	CaseID               string                          `json:"caseId,omitempty"`
	RoundID              string                          `json:"roundId,omitempty"`
	PlanID               string                          `json:"planId,omitempty"`
	PlanVersion          int64                           `json:"planVersion,omitempty"`
	Metrics              []PointMetrics                  `json:"metrics"`
	Comparison           Comparison                      `json:"comparison"`
	PolicyVersion        string                          `json:"policyVersion"`
}

func pointOf(o model.AlarmObservation) model.GovernancePoint {
	return model.GovernancePoint{DeviceID: o.DeviceID, ComponentID: o.ComponentID, AlarmType: o.AlarmType, OriginKind: o.OriginKind, SignalKey: o.SignalKey}
}
func pointMatch(p model.GovernancePoint, o model.AlarmObservation) bool { return pointOf(o) == p }
func (s *Service) Process(ctx context.Context, e *analytics.Execution) (err error) {
	var parameters Parameters
	if err = decode(e.Run.Parameters, &parameters); err != nil {
		return err
	}
	if parameters.JobMode == HistoricalProjectionMode {
		return s.processProjection(ctx, e, parameters)
	}
	defer func() {
		if s.Observe != nil {
			if err != nil {
				s.Observe("alarm_governance_analysis_failed_total")
			} else {
				s.Observe("alarm_governance_analysis_completed_total")
			}
		}
	}()
	var m frozenInputs
	if e.Run.InputsFrozen {
		outputs, _, err := s.Analysis.Store.ListAnalysisOutputs(ctx, e.Run.TenantID, model.AnalysisFilter{RunID: e.Run.ID, Kind: "input-manifest", Limit: 100})
		if err != nil {
			return err
		}
		found := false
		for _, o := range outputs {
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
		hash := model.GovernanceHash(m)
		if len(e.Run.InputHashes) != 1 || hash != e.Run.InputHashes[0] {
			return model.ErrAnalysisConflict
		}
	} else {
		var err error
		m, err = s.freeze(ctx, e.Run)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(m)
		if err = e.Commit(ctx, model.AnalysisBatch{ID: "freeze", Status: model.AnalysisRunning, Stage: "inputs-frozen", FreezeInputs: true, InputHashes: []string{model.GovernanceHash(m)}, DataCutoff: m.Cutoff, Sources: []model.AnalysisSourceCoverage{{Source: "alarm_governance_postgresql", Start: e.Run.Start, End: e.Run.End, Complete: false, ReadAt: m.Cutoff, Reason: "HISTORICAL_COVERAGE_NOT_PROVEN"}}, Outputs: []model.AnalysisOutput{{ID: e.Run.ID + ":input-manifest", Kind: "input-manifest", Body: body}}}); err != nil {
			return err
		}
	}
	stats := Statistics{InputHash: model.GovernanceHash(m), SourceRevisionVector: m.SourceRevisionVector, CaseID: m.Parameters.CaseID, RoundID: m.Parameters.RoundID, PlanID: m.Parameters.PlanID, PolicyVersion: m.Parameters.DiscoveryPolicy.Version, Metrics: []PointMetrics{}, Comparison: Comparison{Conclusion: "INSUFFICIENT_DATA", Limitations: []string{"NO_CONFIRMED_OBSERVATION_PLAN"}}}
	if m.Case != nil {
		stats.DataRevision = m.Case.DataRevision
	}
	if m.Plan != nil {
		stats.PlanVersion = m.PlanVersion
	}
	points := map[string]model.GovernancePoint{}
	for _, o := range m.Observations {
		p := pointOf(o)
		points[p.Key("")] = p
	}
	if m.Case != nil {
		points[m.Case.Key("")] = m.Case.GovernancePoint
	}
	keys := []string{}
	for k := range points {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	outputs := []model.AnalysisOutput{}
	appendOutput := func(kind, id, device string, body any) {
		raw, _ := json.Marshal(body)
		outputs = append(outputs, model.AnalysisOutput{ID: e.Run.ID + ":" + kind + ":" + id, Kind: kind, DeviceID: device, Body: raw})
	}
	for _, key := range keys {
		p := points[key]
		if err := ctx.Err(); err != nil {
			return err
		}
		metric := metricsFor(p, Interval{e.Run.Start, e.Run.End}, "WINDOW", m)
		stats.Metrics = append(stats.Metrics, metric)
		id := model.GovernanceHash(key)[:24]
		appendOutput("metrics", id+":window", p.DeviceID, metric)
		for _, c := range metric.Cycles {
			appendOutput("cycles", c.ID, p.DeviceID, c)
		}
		conditions := []string{}
		policy := m.Parameters.DiscoveryPolicy
		if policy.MinReportCount > 0 && metric.ReportCount >= policy.MinReportCount {
			conditions = append(conditions, "REPORT_COUNT")
		}
		if policy.MinKnownCycleStarts > 0 && metric.KnownStarts >= policy.MinKnownCycleStarts {
			conditions = append(conditions, "KNOWN_CYCLE_STARTS")
		}
		if policy.MinOpenDuration > 0 {
			for _, c := range metric.Cycles {
				if c.Status == "OPEN" && c.KnownDurationLowerBound != nil && *c.KnownDurationLowerBound >= policy.MinOpenDuration {
					conditions = append(conditions, "OPEN_DURATION")
					break
				}
			}
		}
		if len(conditions) > 0 {
			ids := []string{}
			for _, o := range m.Observations {
				if pointMatch(p, o) && (o.FactKind == "REPORT" || o.FactKind == "ASSERT") {
					ids = append(ids, o.ID)
				}
			}
			existing := ""
			for _, d := range m.Resources {
				if d.Kind == model.GovernanceCaseKind {
					c, _ := model.GovernanceBody[model.GovernanceCase](d)
					if c.GovernancePoint == p {
						existing = d.ID
					}
				}
			}
			appendOutput("findings", id, p.DeviceID, map[string]any{"point": p, "conditions": conditions, "observationIds": ids, "window": metric.Window, "policyVersion": policy.Version, "coverage": "PARTIAL_HISTORY", "existingCaseId": existing, "cycleMethod": m.Profile.CycleMethod, "limitations": metric.Limitations})
		}
		if m.Plan != nil {
			before := metricsFor(p, Interval{m.Plan.BeforeStart, m.Plan.BeforeEnd}, "BEFORE", m)
			after := metricsFor(p, Interval{m.Plan.AfterStart, m.Plan.AfterEnd}, "AFTER", m)
			stats.Metrics = append(stats.Metrics, before, after)
			appendOutput("metrics", id+":before", p.DeviceID, before)
			appendOutput("metrics", id+":after", p.DeviceID, after)
			stats.Comparison = comparePlan(m, before.Activity, after.Activity)
			appendOutput("observation-results", id, p.DeviceID, stats.Comparison)
		}
	}
	for _, candidate := range ActivityCandidates(m.Observations, m.Resources, m.AllowedActivityTypes) {
		appendOutput("activity-candidates", model.GovernanceHash(candidate)[:24], candidate.Point.DeviceID, candidate)
	}
	for _, o := range m.Observations {
		appendOutput("observations", o.ID, o.DeviceID, o)
	}
	for _, d := range m.Resources {
		kind := ""
		switch d.Kind {
		case model.GovernanceVerificationKind:
			kind = "verifications"
		case model.GovernanceActivityKind:
			kind = "activities"
		case model.GovernanceCoverageKind:
			kind = "coverages"
		case model.GovernanceMeasureKind:
			kind = "measures"
		}
		if kind != "" {
			appendOutput(kind, d.ID, "", d)
		}
	}
	// Batch IDs and output IDs are stable across lease recovery. Only complete
	// batches are checkpoints; interrupted calculation restarts from frozen data.
	size := max(1, s.Analysis.Limits.BatchSize)
	for at := 0; at < len(outputs); at += size {
		if err := e.Commit(ctx, model.AnalysisBatch{ID: fmt.Sprintf("outputs/%d", at), Status: model.AnalysisRunning, Stage: "calculating", Processed: int64(min(at+size, len(outputs))), Outputs: outputs[at:min(at+size, len(outputs))]}); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(stats)
	sources := []model.AnalysisSourceCoverage{{Source: "alarm_governance_postgresql", Start: e.Run.Start, End: e.Run.End, Complete: false, ReadAt: m.Cutoff, Reason: "HISTORICAL_COVERAGE_NOT_PROVEN"}}
	snap := model.AnalysisSnapshot{ID: e.Run.ID + ":snapshot", DataCutoff: m.Cutoff, InputHashes: []string{model.GovernanceHash(m)}, Statistics: raw, Sources: sources, Limitations: m.Limitations, InitialStateQuality: "EXPLICIT_SEED_OR_UNKNOWN", UncomputableMetrics: []string{}}
	for _, metric := range stats.Metrics {
		if metric.NewStartsPer1000Hours == nil {
			snap.UncomputableMetrics = addIssue(snap.UncomputableMetrics, "每千有效监测小时新发数")
		}
		if metric.Activity.RelatedFraction == nil {
			snap.UncomputableMetrics = addIssue(snap.UncomputableMetrics, "完整活动登记范围的已确认活动相关占比")
		}
	}
	return e.Commit(ctx, model.AnalysisBatch{ID: "final", Status: model.AnalysisPartial, Stage: "complete", Processed: int64(len(outputs)), Snapshot: &snap})
}
func metricsFor(p model.GovernancePoint, w Interval, period string, m frozenInputs) PointMetrics {
	out := PointMetrics{CycleMethod: m.Profile.CycleMethod, Point: p, Period: period, Window: w, Limitations: slices.Clone(m.Limitations), Cycles: []model.AlarmCycleRevision{}}
	obs := []model.AlarmObservation{}
	for _, o := range m.Observations {
		if !pointMatch(p, o) {
			continue
		}
		at := o.TimeAt(observationBasis(m.Parameters, m.Profile))
		if m.Parameters.TimeBasis == "RECEIVED_AT" {
			at = o.ReceivedAt
		}
		if at >= w.Start && at < w.End {
			obs = append(obs, o)
			if o.Acceptance == "ACCEPTED" && (o.FactKind == "ASSERT" || o.FactKind == "REPORT") {
				out.ReportCount++
			}
		}
	}
	var seed *model.AlarmObservation
	for _, o := range append(slices.Clone(m.Seeds), m.Observations...) {
		if pointMatch(p, o) && o.TimeAt(observationBasis(m.Parameters, m.Profile)) < w.Start && usableCycleFact(o, m.Profile.CycleMethod) && (seed == nil || o.TimeAt(observationBasis(m.Parameters, m.Profile)) > seed.TimeAt(observationBasis(m.Parameters, m.Profile))) {
			v := o
			seed = &v
		}
	}
	usable := []Interval{}
	excluded := []Interval{}
	coverage := []CycleCoverage{}
	if m.Plan != nil {
		for _, v := range m.Plan.MonitoringIntervals {
			if v.DeviceID != p.DeviceID || v.Basis == "" || v.ConfirmedBy == "" || v.ConfirmedAt <= 0 {
				continue
			}
			if v.Kind == "VERIFIED_MONITORING" {
				usable = append(usable, Interval{v.Start, v.End})
			} else {
				excluded = append(excluded, Interval{v.Start, v.End})
			}
		}
	}
	if m.Clipped {
		usable = nil
	}
	for _, v := range EffectiveIntervals(Interval{max(0, w.Start-seedMaxAge), w.End}, usable, excluded) {
		coverage = append(coverage, CycleCoverage{Start: v.Start, End: v.End, Reliable: true})
	}
	if m.Parameters.TimeBasis == "EVENT_AT" {
		out.Cycles = RebuildCycles(CycleInput{TenantID: func() string {
			if len(obs) > 0 {
				return obs[0].TenantID
			}
			return ""
		}(), Method: m.Profile.CycleMethod, Start: w.Start, End: w.End, Observations: obs, Seed: seed, SeedMaxAge: seedMaxAge, Coverage: coverage, AlgorithmVersion: AlgorithmVersion, ConfigVersion: m.Parameters.ProfileRevisionID})
	} else {
		out.Limitations = addIssue(out.Limitations, "RECEIVED_DISTRIBUTION_NOT_PHYSICAL_CYCLES")
	}
	for _, c := range out.Cycles {
		if c.NewStart {
			out.KnownStarts++
		}
		if c.Status == "OPEN" {
			out.OpenCycles++
		}
		if c.BoundaryQuality != "COMPLETE" {
			out.BoundaryUnknown++
		}
	}
	activities := []model.FieldActivityRevision{}
	coverages := []model.ActivityCoverage{}
	relations := []ActivityRelation{}
	verified := map[string]bool{}
	superseded := map[string]bool{}
	for _, d := range m.Resources {
		if d.Kind == model.GovernanceVerificationKind && d.Status == "CONFIRMED" && d.CorrectsID != "" {
			superseded[d.CorrectsID] = true
		}
	}
	for _, d := range m.Resources {
		switch d.Kind {
		case model.GovernanceActivityKind:
			v, e := model.GovernanceBody[model.FieldActivityRevision](d)
			if e == nil {
				activities = append(activities, v)
			}
		case model.GovernanceCoverageKind:
			v, e := model.GovernanceBody[model.ActivityCoverage](d)
			if e == nil {
				coverages = append(coverages, v)
			}
		case model.GovernanceVerificationKind:
			v, e := model.GovernanceBody[model.FieldVerification](d)
			if superseded[d.ID] || e != nil || !v.Complete || v.GovernancePoint != p || v.Status != "CONFIRMED" || v.VerifiedAt == nil || v.VerificationMethod == "NOT_VERIFIED" || v.VerificationMethod == "DOCUMENT_REVIEW" {
				continue
			}
			for _, id := range v.ObservationIDs {
				verified[id] = true
				for _, aid := range v.ActivityIDs {
					state := "UNRESOLVED"
					if v.ActivityRelation == "CONFIRMED_RELATED" {
						state = "CONFIRMED_RELATED"
					}
					if v.ActivityRelation == "CONFIRMED_UNRELATED" {
						state = "CONFIRMED_UNRELATED"
					}
					relations = append(relations, ActivityRelation{aid, id, state})
				}
			}
		}
	}
	for _, o := range obs {
		if o.FactKind != "ASSERT" && o.FactKind != "REPORT" {
			continue
		}
		if verified[o.ID] {
			out.VerificationCount++
		} else {
			out.UnverifiedCount++
		}
	}
	typ, unit := "", ""
	if m.Plan != nil {
		typ = m.Plan.ActivityType
		unit = m.Plan.ActivityUnit
	}
	out.Activity = ComputeActivityMetrics(ActivityMetricInput{Window: w, DeviceID: p.DeviceID, ActivityType: typ, Unit: unit, Monitoring: usable, Excluded: excluded, Activities: activities, Coverages: coverages, Observations: obs, Relations: relations})
	clockReliable := true
	for _, o := range obs {
		if o.Acceptance == "ACCEPTED" && !comparableTime(o.TimeQuality) {
			clockReliable = false
		}
	}
	if m.Profile.CycleMethod != "REPORT_ONLY" && out.BoundaryUnknown == 0 && clockReliable && out.Activity.Hours != nil && *out.Activity.Hours > 0 && !m.Clipped && m.Parameters.TimeBasis == "EVENT_AT" {
		n := float64(out.KnownStarts) / *out.Activity.Hours * 1000
		out.NewStartsPer1000Hours = &n
	}
	return out
}
func (s *Service) ValidateReview(ctx context.Context, tenant string, r model.ObservationReview) error {
	snap, err := s.Analysis.Store.GetAnalysisSnapshot(ctx, tenant, r.AnalysisSnapshotID)
	if err != nil {
		return err
	}
	run, err := s.Analysis.Store.GetAnalysisRun(ctx, tenant, snap.RunID)
	if err != nil {
		return err
	}
	if run.Kind != analytics.KindRecurring || (run.Status != model.AnalysisSucceeded && run.Status != model.AnalysisPartial) || r.FactsHash != snap.FactsHash {
		return model.ErrGovernanceConflict
	}
	var stats Statistics
	if err = json.Unmarshal(snap.Statistics, &stats); err != nil {
		return err
	}
	if stats.RoundID != r.RoundID || stats.PlanID != r.PlanID || stats.PlanVersion != r.PlanVersion || stats.DataRevision != r.DataRevision || !slices.Equal(stats.SourceRevisionVector, r.SourceRevisionVector) {
		return model.ErrGovernanceConflict
	}
	if r.Conclusion != stats.Comparison.Conclusion {
		return invalid("评价结论与确定性快照不一致")
	}
	for _, limitation := range stats.Comparison.Limitations {
		if !slices.Contains(r.Limitations, limitation) {
			return invalid("评价须保留快照资料限制：" + limitation)
		}
	}
	return nil
}
func (s *Service) ValidateSnapshot(ctx context.Context, r model.AnalysisRun) error {
	snap, err := s.Analysis.Store.GetAnalysisSnapshot(ctx, r.TenantID, r.SnapshotID)
	if err != nil {
		return err
	}
	var stats Statistics
	if err = json.Unmarshal(snap.Statistics, &stats); err != nil {
		return err
	}
	if s.Store == nil {
		return analytics.ErrUnsupported
	}
	return s.Store.GovernanceRead(ctx, r.TenantID, func(tx ports.AlarmGovernanceTx) error {
		versions, e := tx.SourceVersions(stats.SourceRevisionVector)
		if e != nil {
			return e
		}
		if !slices.Equal(versions, stats.SourceRevisionVector) {
			return model.ErrAnalysisConflict
		}
		if stats.CaseID != "" {
			doc, e := tx.Get(model.GovernanceCaseKind, stats.CaseID)
			if e != nil {
				return e
			}
			c, e := model.GovernanceBody[model.GovernanceCase](doc)
			if e != nil {
				return e
			}
			if c.DataRevision != stats.DataRevision {
				return model.ErrAnalysisConflict
			}
		}
		return nil
	})
}

func comparePlan(m frozenInputs, before, after ActivityMetrics) Comparison {
	equal := m.Plan.BeforeConditionsHash != "" && m.Plan.BeforeConditionsHash == m.Plan.AfterConditionsHash
	comparison := CompareActivities(before, after, equal, m.Plan.MinimumActivities, m.Plan.MinimumMonitoringHours)
	if m.Case == nil || m.Case.IdentityQuality != "CONFIRMED" {
		comparison.Conclusion = "INSUFFICIENT_DATA"
		comparison.Difference = nil
		comparison.Limitations = addIssue(comparison.Limitations, "POINT_IDENTITY_UNCONFIRMED")
	}
	if m.Parameters.TimeBasis == "RECEIVED_AT" {
		comparison.Conclusion = "INSUFFICIENT_DATA"
		comparison.Difference = nil
		comparison.Limitations = addIssue(comparison.Limitations, "RECEIVED_TIME_BASIS_ONLY")
	}
	return comparison
}
