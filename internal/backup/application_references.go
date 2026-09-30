package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

var configReferenceFields = map[string]bool{"executionRevisionId": true, "procedureRevisionId": true, "interventionRevisionId": true, "assetRevisionId": true, "beforeAssetRevisionId": true, "afterAssetRevisionId": true, "contextRevisionIds": true, "admissionRevisionId": true, "scenarioRevisionId": true, "experimentRevisionId": true, "labelRevisionIds": true, "reviewRevisionId": true, "quoteRevisionId": true, "datasetId": true, "profileRevisionId": true, "profileRevisionIds": true, "observationRevisionIds": true}
var runReferenceFields = map[string]bool{"qualityRunIds": true, "observationRunId": true, "evaluationRunId": true}

func validateApplicationReferences(documents []applicationDocument) error {
	byID := map[string]applicationDocument{}
	for _, d := range documents {
		byID[applicationIdentity(d.Tenant, d.Kind, d.ID)] = d
	}
	for _, d := range documents {
		var body json.RawMessage
		switch d.Kind {
		case "config":
			v, _ := decodeApplication[model.AnalysisConfigRevision](d)
			body = v.Body
		case "run":
			v, _ := decodeApplication[model.AnalysisRun](d)
			if !v.InputsFrozen {
				continue
			}
			body = v.Parameters
		default:
			continue
		}
		var tree any
		if json.Unmarshal(body, &tree) != nil {
			return applicationInvalid(d, "invalid reference body")
		}
		var walk func(any) error
		walk = func(node any) error {
			switch value := node.(type) {
			case map[string]any:
				for field, child := range value {
					kind := ""
					if configReferenceFields[field] || (field == "baselineRevisionIds" && (d.ApplicationKind == analytics.KindDataQuality || d.ApplicationKind == model.DataQualityBaselineKind)) || (field == "attachments" && d.ApplicationKind == model.DataQualityCalibrationKind) {
						kind = "config"
					}
					if runReferenceFields[field] {
						kind = "run"
					}
					if kind != "" {
						refs := []string{}
						switch v := child.(type) {
						case string:
							if v != "" {
								refs = append(refs, v)
							}
						case []any:
							for _, raw := range v {
								if ref, ok := raw.(string); ok && ref != "" {
									refs = append(refs, ref)
								}
							}
						}
						for _, id := range refs {
							if _, ok := byID[applicationIdentity(d.Tenant, kind, id)]; !ok {
								return applicationInvalid(d, "fixed "+field+" reference missing")
							}
						}
					}
					if err := walk(child); err != nil {
						return err
					}
				}
			case []any:
				for _, child := range value {
					if err := walk(child); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := walk(tree); err != nil {
			return err
		}
	}
	return nil
}

func validateRestoredAIResult(d applicationDocument, job model.AnalysisAIRevision, documents []applicationDocument) error {
	if job.Status != model.AnalysisSucceeded {
		return nil
	}
	var result model.AnalysisAIResult
	dec := json.NewDecoder(bytes.NewReader(job.Interpretation))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return applicationInvalid(d, "strict AI result structure")
	}
	if _, err := analytics.ValidateAIWorkflowResult(job.WorkflowID, result, job.SentFactIDs, job.DeviceIDs); err != nil {
		return applicationInvalid(d, "AI workflow schema or references")
	}
	if result.CandidateDraft != nil && (job.WorkflowID != analytics.WorkflowRulePolicy || result.CandidateDraft.Enabled || result.CandidateRevisionID == "") {
		return applicationInvalid(d, "AI draft must reference disabled experiment candidate")
	}
	if result.CandidateRevisionID != "" {
		found := false
		for _, v := range documents {
			if v.Tenant == d.Tenant && v.Kind == "config" && v.ID == result.CandidateRevisionID && v.ApplicationKind == model.RuleLabExperimentKind {
				found = true
			}
		}
		if !found {
			return applicationInvalid(d, "AI candidate revision missing")
		}
	}
	scope := map[string][]string{job.SnapshotID + "/summary": job.DeviceIDs}
	for _, v := range documents {
		if v.Tenant != d.Tenant || v.RunID != job.RunID {
			continue
		}
		if v.Kind == "output" {
			output, _ := decodeApplication[model.AnalysisOutput](v)
			if output.DeviceID != "" {
				scope[v.ID] = []string{output.DeviceID}
			} else {
				scope[v.ID] = v.DeviceIDs
			}
		}
		if v.Kind == "evidence" {
			scope[v.ID] = []string{v.DeviceID}
		}
	}
	sets := [][]model.AnalysisAIStatement{result.Interpretations, result.SuggestedVerification, result.Limitations, result.ObservedWeaknesses, result.PrioritizedChecks, result.DependencyObservations, result.BehaviorDifferences, result.VerificationSuggestions, result.ObservedBottlenecks, result.EvidenceGaps, result.ImprovementSuggestions, result.ObservedChanges, result.Confounders, result.PriorityExplanations, result.DecisionConsiderations}
	all := []string{}
	for _, set := range sets {
		for _, statement := range set {
			if statement.Text == "" || len(statement.FactIDs) == 0 {
				return applicationInvalid(d, "AI statement missing evidence")
			}
			allowed := []string{}
			for _, id := range statement.FactIDs {
				devices, ok := scope[id]
				if !ok || !slices.Contains(job.SentFactIDs, id) || !slices.Contains(job.FactIDs, id) {
					return applicationInvalid(d, "AI statement refers to unsent fact")
				}
				allowed = append(allowed, devices...)
				all = append(all, id)
			}
			for _, device := range statement.DeviceIDs {
				if !slices.Contains(allowed, device) {
					return applicationInvalid(d, "AI statement object scope exceeds evidence")
				}
			}
		}
	}
	for _, id := range job.FactIDs {
		if !slices.Contains(all, id) {
			return errors.New("AI persisted references differ from interpretation")
		}
	}
	return nil
}
