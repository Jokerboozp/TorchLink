package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/maintenance"
	"iot-platform/internal/model"
)

// This is the versioned maintenance-facts-v1 wire contract, including its
// typed/RawMessage boundaries. Reconstructing the typed fields preserves the
// original digest; nested configuration bodies and snapshot statistics were
// already read from PostgreSQL when this manifest was frozen.
type restoredMaintenanceBusiness struct {
	SourceConfigurationHash string                         `json:"sourceConfigurationHash"`
	Parameters              model.MaintenanceRunParameters `json:"parameters"`
	Configs                 []model.AnalysisConfigRevision `json:"configs"`
	Snapshots               []model.AnalysisSnapshot       `json:"snapshots"`
	RequiredPermissions     []string                       `json:"requiredPermissions,omitempty"`
}

type restoredMaintenanceFault struct {
	Cycle                maintenance.FaultCycle `json:"cycle"`
	AssessmentRevisionID string                 `json:"assessmentRevisionId"`
	SourceEventID        string                 `json:"sourceEventId"`
	MessageID            string                 `json:"messageId,omitempty"`
	RawMessageID         string                 `json:"rawMessageId,omitempty"`
}

type restoredMaintenanceManifest struct {
	Version             string                          `json:"version"`
	Business            restoredMaintenanceBusiness     `json:"business"`
	DataCutoff          int64                           `json:"dataCutoff"`
	States              []model.DeviceStateIntervalFact `json:"states"`
	Faults              []restoredMaintenanceFault      `json:"faults"`
	FaultSourceKnown    []model.FactRange               `json:"faultSourceKnown"`
	Contexts            []model.AnalysisConfigRevision  `json:"contexts"`
	Costs               []model.AnalysisConfigRevision  `json:"costs,omitempty"`
	Sources             []model.AnalysisSourceCoverage  `json:"sources"`
	Confounders         []string                        `json:"confounders"`
	Limitations         []string                        `json:"limitations"`
	RepeatedMaintenance int                             `json:"repeatedMaintenance"`
}

func validateMaintenanceFrozenInputs(d applicationDocument, run model.AnalysisRun, byID map[string]applicationDocument) error {
	if !run.InputsFrozen || (run.Kind != analytics.KindMaintenance && run.Kind != analytics.KindInvestment) {
		return nil
	}
	var manifest restoredMaintenanceManifest
	count := 0
	for _, output := range byID {
		if output.Tenant != run.TenantID || output.Kind != "output" || output.RunID != run.ID {
			continue
		}
		v, err := decodeApplication[model.AnalysisOutput](output)
		if err != nil || v.Kind != "input-manifest" {
			continue
		}
		count++
		decoder := json.NewDecoder(bytes.NewReader(v.Body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&manifest) != nil {
			return applicationInvalid(d, "unknown fixed maintenance manifest structure")
		}
	}
	if count == 0 && run.Status == model.AnalysisPartial && len(run.InputHashes) == 1 && run.InputHashes[0] == run.ConfigurationVersion {
		// A timeout before any input was read deliberately contains no facts.
		// It must not be used to bypass fixed inputs on a populated result.
		preFreezeTimeout := false
		for _, source := range run.Sources {
			preFreezeTimeout = preFreezeTimeout || source.Reason == "TIME_BUDGET_EXHAUSTED_BEFORE_INPUT_FREEZE"
		}
		for _, other := range byID {
			if other.Tenant == run.TenantID && other.RunID == run.ID && (other.Kind == "output" || other.Kind == "evidence") {
				preFreezeTimeout = false
			}
		}
		if preFreezeTimeout {
			return nil
		}
	}
	if count != 1 || manifest.Version != "maintenance-facts-v1" {
		return applicationInvalid(d, "fixed maintenance manifest missing or unsupported")
	}
	b := manifest.Business
	var parameters model.MaintenanceRunParameters
	if json.Unmarshal(run.Parameters, &parameters) != nil || !sameApplicationHash(parameters, b.Parameters) || !slices.Equal(run.RequiredPermissions, b.RequiredPermissions) || run.ConfigurationVersion != b.SourceConfigurationHash || manifest.DataCutoff != run.DataCutoff || !sameApplicationHash(manifest.Sources, run.Sources) {
		return applicationInvalid(d, "fixed maintenance parameters, permissions or cutoff changed")
	}
	manifestHash, err := analytics.AnalysisHash(manifest)
	if err != nil || len(run.InputHashes) != 2 || run.InputHashes[0] != run.ConfigurationVersion || run.InputHashes[1] != manifestHash {
		return applicationInvalid(d, "fixed maintenance manifest digest cannot be verified")
	}
	refs := []string{}
	configs := map[string]model.AnalysisConfigRevision{}
	checkConfig := func(v model.AnalysisConfigRevision) error {
		doc, ok := byID[applicationIdentity(run.TenantID, "config", v.ID)]
		actual, err := decodeApplication[model.AnalysisConfigRevision](doc)
		compatibleScope := applicationDevicesSubset(v.DeviceIDs, run.DeviceIDs) || (v.Kind == maintenance.AdmissionKind && applicationDevicesSubset(run.DeviceIDs, v.DeviceIDs))
		if !ok || err != nil || v.TenantID != run.TenantID || !sameApplicationHash(v, actual) || !compatibleScope {
			return applicationInvalid(d, "fixed maintenance configuration differs from restored revision")
		}
		return nil
	}
	for _, v := range b.Configs {
		if err = checkConfig(v); err != nil {
			return err
		}
		configs[v.ID] = v
		refs = append(refs, fmt.Sprintf("config:%s:%s", v.ID, v.Hash))
	}
	snapshots := map[string]model.AnalysisSnapshot{}
	for _, v := range b.Snapshots {
		doc, ok := byID[applicationIdentity(run.TenantID, "snapshot", v.ID)]
		actual, er := decodeApplication[model.AnalysisSnapshot](doc)
		if !ok || er != nil || v.TenantID != run.TenantID || !sameApplicationHash(v, actual) || !applicationDevicesSubset(v.DeviceIDs, run.DeviceIDs) {
			return applicationInvalid(d, "fixed maintenance source snapshot changed")
		}
		snapshots[v.RunID] = v
		refs = append(refs, fmt.Sprintf("snapshot:%s:%d:%s", v.ID, v.Version, v.FactsHash))
	}
	slices.Sort(refs)
	refs = slices.Compact(refs)
	businessHash, err := analytics.AnalysisHash([]any{b.Parameters, refs, b.RequiredPermissions})
	if err != nil || businessHash != b.SourceConfigurationHash {
		return applicationInvalid(d, "fixed maintenance source configuration digest")
	}
	for _, v := range append(slices.Clone(manifest.Contexts), manifest.Costs...) {
		if err = checkConfig(v); err != nil {
			return err
		}
	}
	if !parameters.UseFinance && len(manifest.Costs) != 0 {
		return applicationInvalid(d, "ordinary maintenance manifest contains financial records")
	}
	required := []string{}
	if parameters.UseFinance {
		required = append(required, analytics.FinanceReadOperation)
	}
	if run.Kind == analytics.KindMaintenance {
		p := parameters.Observation
		if p == nil || parameters.ScenarioRevisionID != "" {
			return applicationInvalid(d, "fixed observation parameters")
		}
		for _, ref := range append([]string{p.InterventionRevisionID, p.BeforeAssetRevisionID, p.AfterAssetRevisionID}, p.ContextRevisionIDs...) {
			if _, ok := configs[ref]; !ok {
				return applicationInvalid(d, "observation configuration not fixed in manifest")
			}
		}
		if p.AdmissionRevisionID != "" {
			if _, ok := configs[p.AdmissionRevisionID]; !ok {
				return applicationInvalid(d, "observation admission not fixed in manifest")
			}
		}
		for _, id := range p.QualityRunIDs {
			snapshot, ok := snapshots[id]
			source, exists := byID[applicationIdentity(run.TenantID, "run", id)]
			if !ok || !exists || source.ApplicationKind != analytics.KindDataQuality || snapshot.RunID != id {
				return applicationInvalid(d, "quality snapshot not fixed in observation manifest")
			}
		}
		if len(p.QualityRunIDs) > 0 {
			required = append(required, analytics.QualityReadPermission)
		}
	} else {
		v, ok := configs[parameters.ScenarioRevisionID]
		var scenario model.InvestmentScenario
		if !ok || v.Kind != maintenance.ScenarioKind || parameters.Observation != nil || json.Unmarshal(v.Body, &scenario) != nil || scenario.UseFinance != parameters.UseFinance {
			return applicationInvalid(d, "fixed investment scenario financial binding")
		}
		for _, candidate := range scenario.Candidates {
			if _, ok := configs[candidate.AssetRevisionID]; !ok {
				return applicationInvalid(d, "investment asset not fixed in manifest")
			}
			if candidate.QuoteRevisionID != "" {
				if _, ok := configs[candidate.QuoteRevisionID]; !ok || !parameters.UseFinance {
					return applicationInvalid(d, "investment quote financial binding")
				}
			}
			if candidate.ObservationRunID == "" {
				continue
			}
			source, exists := byID[applicationIdentity(run.TenantID, "run", candidate.ObservationRunID)]
			prior, er := decodeApplication[model.AnalysisRun](source)
			_, fixed := snapshots[candidate.ObservationRunID]
			if !exists || er != nil || !fixed || prior.Kind != analytics.KindMaintenance || (!parameters.UseFinance && slices.Contains(prior.RequiredPermissions, analytics.FinanceReadOperation)) {
				return applicationInvalid(d, "investment observation financial binding")
			}
			required = append(required, prior.RequiredPermissions...)
		}
		slices.Sort(required)
		required = slices.Compact(required)
		if !slices.Equal(required, scenario.RequiredPermissions) {
			return applicationInvalid(d, "fixed investment scenario inherited permissions changed")
		}
	}
	slices.Sort(required)
	required = slices.Compact(required)
	if !slices.Equal(required, run.RequiredPermissions) {
		return applicationInvalid(d, "fixed maintenance inherited permissions changed")
	}
	return nil
}

func applicationDevicesSubset(want, have []string) bool {
	for _, device := range want {
		if !slices.Contains(have, device) {
			return false
		}
	}
	return len(want) > 0
}

func validateRestoredScenarioPermissions(d applicationDocument, v model.AnalysisConfigRevision, byID map[string]applicationDocument) error {
	if v.Kind != maintenance.ScenarioKind {
		return nil
	}
	var scenario model.InvestmentScenario
	if json.Unmarshal(v.Body, &scenario) != nil {
		return applicationInvalid(d, "investment scenario structure")
	}
	required := []string{}
	if scenario.UseFinance {
		required = append(required, analytics.FinanceReadOperation)
	} else if scenario.Budget != nil {
		return applicationInvalid(d, "ordinary scenario contains a financial budget")
	}
	for _, candidate := range scenario.Candidates {
		if !scenario.UseFinance && candidate.QuoteRevisionID != "" {
			return applicationInvalid(d, "ordinary scenario references a financial quote")
		}
		if candidate.ObservationRunID == "" {
			continue
		}
		source, ok := byID[applicationIdentity(v.TenantID, "run", candidate.ObservationRunID)]
		prior, err := decodeApplication[model.AnalysisRun](source)
		if !ok || err != nil || prior.Kind != analytics.KindMaintenance || prior.SnapshotID == "" || !applicationDevicesSubset(prior.DeviceIDs, v.DeviceIDs) || (!scenario.UseFinance && slices.Contains(prior.RequiredPermissions, analytics.FinanceReadOperation)) {
			return applicationInvalid(d, "investment scenario source observation financial binding")
		}
		required = append(required, prior.RequiredPermissions...)
	}
	slices.Sort(required)
	required = slices.Compact(required)
	if !slices.Equal(required, scenario.RequiredPermissions) {
		return applicationInvalid(d, "investment scenario inherited permissions changed")
	}
	return nil
}
