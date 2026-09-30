package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"slices"
	"time"
)

func (s *Service) ProcessObservation(ctx context.Context, e *analytics.Execution) (resultErr error) {
	defer func() {
		if errors.Is(resultErr, context.DeadlineExceeded) {
			if s.timeout(ctx, e) == nil {
				resultErr = nil
			}
		}
	}()
	var m frozenManifest
	var err error
	if e.Run.InputsFrozen {
		m, err = s.loadManifest(ctx, e.Run)
	} else {
		var p model.MaintenanceRunParameters
		if err = decode(e.Run.Parameters, &p); err != nil {
			return err
		}
		var b businessInputs
		b, err = s.observationInputs(ctx, runActor(e.Run), p, e.Run.DeviceIDs)
		if err == nil && b.SourceConfigurationHash != e.Run.ConfigurationVersion {
			err = model.ErrAnalysisConflict
		}
		if err == nil {
			m, err = s.freezeObservation(ctx, e, b)
		}
		if err == nil {
			err = s.saveManifest(ctx, e, m)
		}
	}
	if err != nil {
		return err
	}
	if err = s.reauthorizeInputs(ctx, e.Run, m); err != nil {
		return err
	}
	result, supplement, err := observe(m)
	if err != nil {
		return invalid(err.Error())
	}
	outputs := []model.AnalysisOutput{{ID: e.Run.ID + ":observation", Kind: "observations", Body: jsonBody(result)}, {ID: e.Run.ID + ":changes", Kind: "change-metrics", Body: jsonBody(map[string]any{"comparison": result, "supplement": supplement})}}
	limitations := slices.Clone(m.Limitations)
	for _, v := range result.Limitations {
		limitations = unique(limitations, v)
	}
	for _, v := range result.Before.Limitations {
		limitations = unique(limitations, "BEFORE:"+v)
	}
	for _, v := range result.After.Limitations {
		limitations = unique(limitations, "AFTER:"+v)
	}
	for i, v := range limitations {
		outputs = append(outputs, model.AnalysisOutput{ID: fmt.Sprintf("%s:finding:%d", e.Run.ID, i), Kind: "findings", Body: jsonBody(map[string]any{"code": v, "status": result.Status, "deviceIds": e.Run.DeviceIDs, "basis": "FIXED_RECORDED_FACTS"})})
	}
	evidence := businessEvidence(e.Run, m)
	for i, v := range m.Faults {
		evidence = append(evidence, model.AnalysisEvidence{ID: fmt.Sprintf("%s:fault-evidence:%d", e.Run.ID, i), SourceKind: "ALARM_CREATED", SourceID: v.SourceEventID, DeviceID: v.Cycle.DeviceID, OccurredAt: v.Cycle.EventAt, RawMessageID: v.RawMessageID, ResourceVersion: v.AssessmentRevisionID, Summary: jsonBody(v), PermissionCategory: "maintenance-evidence", OriginalAvailability: "AVAILABLE_AT_FREEZE"})
	}
	partial := len(m.Limitations) > 0 || result.Before.StateUnknownMs > 0 || result.After.StateUnknownMs > 0 || result.Before.FaultSourceUnknownMs > 0 || result.After.FaultSourceUnknownMs > 0
	stats := jsonBody(result)
	// Extra fixed summaries do not replace denominators or alter the observation.
	var full map[string]any
	json.Unmarshal(stats, &full)
	full["supplement"] = supplement
	full["useFinance"] = m.Business.Parameters.UseFinance
	stats = jsonBody(full)
	return s.finish(ctx, e, m, outputs, evidence, stats, limitations, partial)
}
func observe(m frozenManifest) (model.MaintenanceObservation, map[string]any, error) {
	p := *m.Business.Parameters.Observation
	beforeRef, afterRef := configByID(m.Business, p.BeforeAssetRevisionID), configByID(m.Business, p.AfterAssetRevisionID)
	var beforeAsset, afterAsset model.AssetInstance
	decode(beforeRef.Body, &beforeAsset)
	decode(afterRef.Body, &afterAsset)
	var work model.MaintenanceIntervention
	decode(configByID(m.Business, p.InterventionRevisionID).Body, &work)
	verification := "UNKNOWN"
	if len(work.Verifications) > 0 {
		verification = work.Verifications[len(work.Verifications)-1].Result
	}
	before := SideInput{AssetID: beforeRef.ResourceID, Asset: beforeAsset, Window: p.Before, States: m.States, FaultSourceKnown: m.FaultSourceKnown, Admission: p.Admission}
	after := SideInput{AssetID: afterRef.ResourceID, Asset: afterAsset, Window: p.After, States: m.States, FaultSourceKnown: m.FaultSourceKnown, Admission: p.Admission}
	for _, v := range m.Faults {
		before.Faults = append(before.Faults, v.Cycle)
		after.Faults = append(after.Faults, v.Cycle)
	}
	for _, v := range m.Contexts {
		var c model.OperatingContext
		decode(v.Body, &c)
		if c.Confirmed && slices.Contains([]string{"PLANNED_STOP", "CONFIRMED_TEST", "PLATFORM_OBSERVATION_UNAVAILABLE"}, c.Kind) {
			x := model.MaintenanceExclusion{Kind: c.Kind, Start: c.Start, End: c.End, SourceRevisionID: v.ID}
			before.Exclusions = append(before.Exclusions, x)
			after.Exclusions = append(after.Exclusions, x)
		}
	}
	before.Window.End = min(before.Window.End, m.DataCutoff)
	after.Window.End = min(after.Window.End, m.DataCutoff)
	supplement := map[string]any{"dataCutoff": m.DataCutoff, "plannedBefore": p.Before, "plannedAfter": p.After, "observedBefore": before.Window, "observedAfter": after.Window, "repeatedMaintenanceRecords": m.RepeatedMaintenance, "functionalVerifications": work.Verifications, "qualitySnapshots": m.Business.Snapshots, "physicalAgeKnown": beforeAsset.CommissionedAt != nil && afterAsset.CommissionedAt != nil, "observedFaultCycles": len(m.Faults)}
	recurrence := firstRecurrence(after, work.EndedAt)
	if recurrence > 0 {
		supplement["firstRecurrenceEventAt"] = recurrence
		supplement["recurrenceElapsedMs"] = recurrence - work.EndedAt
	} else {
		supplement["recurrenceStatus"] = "NOT_OBSERVED_WITHIN_FIXED_SOURCE_COVERAGE"
	}
	if m.Business.Parameters.UseFinance {
		costs := []any{}
		for _, v := range m.Costs {
			var c model.MaintenanceCost
			decode(v.Body, &c)
			c.Requests = nil
			costs = append(costs, map[string]any{"revisionId": v.ID, "cost": c})
		}
		supplement["recordedActualCosts"] = costs
		supplement["costAggregation"] = "SEPARATE_RECORDED_CURRENCY_NO_LIFECYCLE_OR_ROI_INFERENCE"
	}
	comparable := contextsComparable(m.Contexts, p.Before, p.After)
	supplement["contextComparable"] = comparable
	confounders := slices.Clone(m.Confounders)
	beforeIntensity, afterIntensity := intensity(m.Contexts, p.Before), intensity(m.Contexts, p.After)
	supplement["beforeIntensity"], supplement["afterIntensity"] = beforeIntensity, afterIntensity
	if comparable && p.AdmissionRevisionID != "" {
		var admission model.MaintenanceAdmissionRecord
		decode(configByID(m.Business, p.AdmissionRevisionID).Body, &admission)
		basis, _ := analytics.AnalysisHash([]any{admission.ProductID, admission.FaultType, admission.Parameters, beforeIntensity})
		supplement["comparisonBasis"] = basis
	}
	if beforeIntensity != "" && afterIntensity != "" && beforeIntensity != afterIntensity {
		confounders = unique(confounders, "OPERATING_INTENSITY_CHANGED")
	}
	if after.Window.End <= after.Window.Start {
		result := model.MaintenanceObservation{Parameters: p, Status: "OBSERVING", Verification: verification, Confounders: confounders, Limitations: []string{"AFTER_OBSERVATION_NOT_STARTED", "DESCRIPTIVE_CHANGE_DOES_NOT_PROVE_CAUSALITY_OR_SAFETY"}}
		var err error
		result.Before, err = CalculateSide(before)
		return result, supplement, err
	}
	result, err := Compare(before, after, p, work.EndedAt, verification, comparable, confounders)
	if err != nil {
		return result, supplement, err
	}
	if p.After.End > m.DataCutoff {
		result.Status = "OBSERVING"
		result.Limitations = unique(result.Limitations, "PLANNED_AFTER_WINDOW_STILL_OPEN")
	}
	if len(m.Limitations) > 0 && result.Status == "READY" {
		result.Status = "INSUFFICIENT"
		result.Limitations = unique(result.Limitations, "SOURCE_OR_CONFIGURATION_HISTORY_INSUFFICIENT")
	}
	return result, supplement, nil
}

func firstRecurrence(after SideInput, completedAt int64) int64 {
	asset := after.Asset
	if asset.BoundaryStatus != "CONFIRMED" || asset.EffectiveStart <= 0 || asset.PhysicalID == "" {
		return 0
	}
	start, end := max(after.Window.Start, asset.EffectiveStart, completedAt), after.Window.End
	if asset.CommissionedAt != nil {
		start = max(start, *asset.CommissionedAt)
	}
	if asset.EffectiveEnd != nil {
		end = min(end, *asset.EffectiveEnd)
	}
	if asset.RetiredAt != nil {
		end = min(end, *asset.RetiredAt)
	}
	cycles := map[string]FaultCycle{}
	disputed := map[string]bool{}
	for _, cycle := range after.Faults {
		if prior, ok := cycles[cycle.ID]; ok && prior != cycle {
			disputed[cycle.ID] = true
		}
		cycles[cycle.ID] = cycle
	}
	recurrence := int64(0)
	for _, cycle := range cycles {
		if cycle.ID == "" || disputed[cycle.ID] || cycle.Type != "FAULT" || cycle.Classification != "PRODUCTION" || cycle.Confirmation != "CONFIRMED" || cycle.EventAt <= 0 || cycle.EventAt < start || cycle.EventAt >= end || cycle.DeviceID != asset.DeviceID || (asset.ComponentID != "" && cycle.ComponentID != asset.ComponentID) {
			continue
		}
		excluded := false
		for _, v := range after.Exclusions {
			if v.SourceRevisionID != "" && cycle.EventAt >= v.Start && cycle.EventAt < v.End && slices.Contains([]string{"PLANNED_STOP", "CONFIRMED_TEST", "PLATFORM_OBSERVATION_UNAVAILABLE"}, v.Kind) {
				excluded = true
				break
			}
		}
		if !excluded && (recurrence == 0 || cycle.EventAt < recurrence) {
			recurrence = cycle.EventAt
		}
	}
	return recurrence
}
func intensity(configs []model.AnalysisConfigRevision, w model.FactRange) string {
	// Require one consistent, confirmed operating intensity covering the complete
	// side; a partial overlap cannot silently certify both windows as comparable.
	value := ""
	ranges := []model.FactRange{}
	for _, v := range configs {
		var c model.OperatingContext
		decode(v.Body, &c)
		if !c.Confirmed || c.Kind != "OPERATING_INTENSITY" || c.End <= w.Start || c.Start >= w.End {
			continue
		}
		if value != "" && value != c.Intensity {
			return ""
		}
		value = c.Intensity
		ranges = append(ranges, model.FactRange{Start: max(c.Start, w.Start), End: min(c.End, w.End)})
	}
	slices.SortFunc(ranges, func(a, b model.FactRange) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return 0
	})
	cursor := w.Start
	for _, r := range ranges {
		if r.Start > cursor {
			return ""
		}
		cursor = max(cursor, r.End)
	}
	if cursor < w.End {
		return ""
	}
	return value
}
func contextsComparable(c []model.AnalysisConfigRevision, a, b model.FactRange) bool {
	x, y := intensity(c, a), intensity(c, b)
	return x != "" && x == y
}
func businessEvidence(r model.AnalysisRun, m frozenManifest) []model.AnalysisEvidence {
	out := []model.AnalysisEvidence{}
	seen := map[string]bool{}
	for _, v := range append(slices.Clone(m.Business.Configs), m.Costs...) {
		if seen[v.ID] {
			continue
		}
		seen[v.ID] = true
		for _, device := range v.DeviceIDs {
			out = append(out, model.AnalysisEvidence{ID: r.ID + ":business:" + v.ID + ":" + device, SourceKind: v.Kind, SourceID: v.ID, DeviceID: device, OccurredAt: v.CreatedAt, ResourceVersion: fmt.Sprint(v.Version), Summary: jsonBody(map[string]any{"resourceId": v.ResourceID, "revisionId": v.ID, "hash": v.Hash, "version": v.Version}), PermissionCategory: "maintenance-evidence", OriginalAvailability: "FIXED_IMMUTABLE_BUSINESS_REVISION"})
		}
	}
	return out
}
func (s *Service) ProcessInvestment(ctx context.Context, e *analytics.Execution) (resultErr error) {
	defer func() {
		if errors.Is(resultErr, context.DeadlineExceeded) {
			if s.timeout(ctx, e) == nil {
				resultErr = nil
			}
		}
	}()
	var m frozenManifest
	var err error
	if e.Run.InputsFrozen {
		m, err = s.loadManifest(ctx, e.Run)
	} else {
		var p model.MaintenanceRunParameters
		if err = decode(e.Run.Parameters, &p); err != nil {
			return err
		}
		var b businessInputs
		b, err = s.investmentInputs(ctx, runActor(e.Run), p, e.Run.DeviceIDs)
		if err == nil && b.SourceConfigurationHash != e.Run.ConfigurationVersion {
			err = model.ErrAnalysisConflict
		}
		if err == nil {
			m = frozenManifest{Version: "maintenance-facts-v1", Business: b, DataCutoff: s.now(), Sources: []model.AnalysisSourceCoverage{}, Limitations: []string{}}
			for _, v := range b.Configs {
				m.Sources = append(m.Sources, model.AnalysisSourceCoverage{Source: "business:" + v.Kind, Start: e.Run.Start, End: e.Run.End, Complete: true, ReadAt: m.DataCutoff, Version: v.ID, Watermark: v.Hash})
			}
			err = s.saveManifest(ctx, e, m)
		}
	}
	if err != nil {
		return err
	}
	var scenario model.InvestmentScenario
	if decode(configByID(m.Business, m.Business.Parameters.ScenarioRevisionID).Body, &scenario) != nil {
		return invalid("固定方案损坏")
	}
	quotes := map[string]InvestmentQuote{}
	if m.Business.Parameters.UseFinance {
		for _, v := range m.Business.Configs {
			if v.Kind == CostKind {
				var cost model.MaintenanceCost
				decode(v.Body, &cost)
				cost.Requests = nil
				quotes[v.ID] = InvestmentQuote{RevisionID: v.ID, Cost: cost}
			}
		}
	}
	result, err := RankInvestment(scenario, quotes, m.Business.Parameters.UseFinance)
	if err != nil {
		return invalid(err.Error())
	}
	outputs := []model.AnalysisOutput{}
	for i, v := range result.Items {
		outputs = append(outputs, model.AnalysisOutput{ID: fmt.Sprintf("%s:priority:%d", e.Run.ID, i), Kind: "investment-priorities", Body: jsonBody(v)})
		if result.UsesFinance {
			outputs = append(outputs, model.AnalysisOutput{ID: fmt.Sprintf("%s:budget:%d", e.Run.ID, i), Kind: "budget-lines", Body: jsonBody(v)})
		}
		if v.Status != "COMPARABLE" || result.UsesFinance && v.BudgetStatus != "INCLUDED_IN_ORDERED_TRIAL" {
			outputs = append(outputs, model.AnalysisOutput{ID: fmt.Sprintf("%s:investment-finding:%d", e.Run.ID, i), Kind: "findings", Body: jsonBody(v)})
		}
	}
	return s.finish(ctx, e, m, outputs, businessEvidence(e.Run, m), jsonBody(result), result.Limitations, false)
}
func (s *Service) finish(ctx context.Context, e *analytics.Execution, m frozenManifest, outputs []model.AnalysisOutput, evidence []model.AnalysisEvidence, statistics json.RawMessage, limitations []string, partial bool) error {
	// Stable batch IDs and checkpoint offsets make takeover replay idempotent.
	type cp struct {
		Next int `json:"next"`
	}
	var checkpoint cp
	json.Unmarshal(e.Run.Checkpoint, &checkpoint)
	documents := len(outputs) + len(evidence)
	batchSize := s.Analysis.Limits.BatchSize
	for start := checkpoint.Next; start < documents; {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(documents, start+batchSize)
		batch := model.AnalysisBatch{ID: fmt.Sprintf("maintenance-output-%d", start), Stage: "fixed-results", Status: model.AnalysisRunning, Processed: int64(end), Checkpoint: jsonBody(cp{Next: end})}
		for i := start; i < end; i++ {
			if i < len(outputs) {
				batch.Outputs = append(batch.Outputs, outputs[i])
			} else {
				batch.Evidence = append(batch.Evidence, evidence[i-len(outputs)])
			}
		}
		if err := e.Commit(ctx, batch); err != nil {
			return err
		}
		start = end
	}
	hash, _ := analytics.AnalysisHash(m)
	for _, source := range m.Sources {
		if !source.Complete {
			partial = true
			limitations = unique(limitations, "INCOMPLETE_SOURCE:"+source.Source)
		}
	}
	status := model.AnalysisSucceeded
	if partial {
		status = model.AnalysisPartial
	}
	snapshot := model.AnalysisSnapshot{ID: e.Run.ID + ":snapshot", DeviceIDs: e.Run.DeviceIDs, Start: e.Run.Start, End: e.Run.End, DataCutoff: m.DataCutoff, InputHashes: []string{e.Run.ConfigurationVersion, hash}, Sources: m.Sources, InitialStateQuality: "FIXED_RECORDED_FACTS_UNKNOWN_REMAINS_UNKNOWN", Statistics: statistics, Limitations: limitations}
	for _, source := range m.Sources {
		if !source.Complete {
			snapshot.AffectedIntervals = append(snapshot.AffectedIntervals, source)
		}
	}
	return e.Commit(ctx, model.AnalysisBatch{ID: "maintenance-final", Stage: "complete", Status: status, Processed: int64(documents), Checkpoint: jsonBody(map[string]any{"completed": true}), Snapshot: &snapshot})
}
func (s *Service) timeout(ctx context.Context, e *analytics.Execution) error {
	live, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	run, err := s.Analysis.Store.GetAnalysisRun(live, e.Run.TenantID, e.Run.ID)
	if err != nil {
		return err
	}
	e.Run = run
	limits := []string{"RUN_TIME_BUDGET_EXHAUSTED", "NO_COMPLETENESS_OR_ZERO_FAULT_INFERENCE"}
	stats := jsonBody(map[string]any{"status": "INSUFFICIENT", "processed": run.Processed, "useFinance": slices.Contains(run.RequiredPermissions, FinancePermission)})
	cutoff, hashes, sources := run.DataCutoff, run.InputHashes, run.Sources
	if !run.InputsFrozen {
		cutoff = s.now()
		hashes = []string{run.ConfigurationVersion}
		sources = []model.AnalysisSourceCoverage{{Source: "maintenance_facts", Start: run.Start, End: run.End, ReadAt: cutoff, Complete: false, Reason: "TIME_BUDGET_EXHAUSTED_BEFORE_INPUT_FREEZE"}}
	}
	snapshot := model.AnalysisSnapshot{ID: run.ID + ":timeout-snapshot", DeviceIDs: run.DeviceIDs, Start: run.Start, End: run.End, DataCutoff: cutoff, InputHashes: hashes, Sources: sources, Statistics: stats, Limitations: limits, UncomputableMetrics: []string{"unprocessed observations or priority items"}}
	return e.Commit(live, model.AnalysisBatch{ID: "maintenance-timeout", Stage: "partial", Status: model.AnalysisPartial, Checkpoint: jsonBody(map[string]any{"timeout": true}), Snapshot: &snapshot})
}
