package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

type qualityAIResult struct {
	Summary               string                      `json:"summary"`
	Interpretations       []model.AnalysisAIStatement `json:"interpretations"`
	SuggestedVerification []model.AnalysisAIStatement `json:"suggestedVerification"`
	Limitations           []model.AnalysisAIStatement `json:"limitations"`
	Coverage              model.AnalysisAICoverage    `json:"coverage"`
}
type monitoringAIResult struct {
	Summary                string                      `json:"summary"`
	ObservedWeaknesses     []model.AnalysisAIStatement `json:"observedWeaknesses"`
	PrioritizedChecks      []model.AnalysisAIStatement `json:"prioritizedChecks"`
	DependencyObservations []model.AnalysisAIStatement `json:"dependencyObservations"`
	Limitations            []model.AnalysisAIStatement `json:"limitations"`
	Coverage               model.AnalysisAICoverage    `json:"coverage"`
}

func decodeAIJSON(answer string, target any) error {
	answer = strings.TrimSpace(answer)
	if strings.HasPrefix(answer, "```") {
		if i := strings.IndexByte(answer, '\n'); i >= 0 {
			answer = strings.TrimSpace(strings.TrimSuffix(answer[i+1:], "```"))
		}
	}
	if len(answer) > 48<<10 {
		return model.ErrAnalysisInvalid
	}
	decoder := json.NewDecoder(bytes.NewBufferString(answer))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: AI structure", model.ErrAnalysisInvalid)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return model.ErrAnalysisInvalid
	}
	return nil
}

func DecodeAnalysisAIResult(answer string, ids, devices []string, coverage model.AnalysisAICoverage) (model.AnalysisAIResult, error) {
	return DecodeAIWorkflowResult(WorkflowDataQuality, answer, ids, devices, coverage)
}

func DecodeAIWorkflowResult(workflow, answer string, ids, devices []string, coverage model.AnalysisAICoverage) (model.AnalysisAIResult, error) {
	var result model.AnalysisAIResult
	var err error
	switch workflow {
	case WorkflowDataQuality:
		var decoded qualityAIResult
		err = decodeAIJSON(answer, &decoded)
		result.Interpretations, result.SuggestedVerification, result.Limitations = decoded.Interpretations, decoded.SuggestedVerification, decoded.Limitations
	case WorkflowMonitoring:
		var decoded monitoringAIResult
		err = decodeAIJSON(answer, &decoded)
		result.ObservedWeaknesses, result.PrioritizedChecks, result.DependencyObservations, result.Limitations = decoded.ObservedWeaknesses, decoded.PrioritizedChecks, decoded.DependencyObservations, decoded.Limitations
	default:
		return result, ErrUnsupported
	}
	if err != nil {
		return result, err
	}
	result.Coverage = coverage
	kinds := "指标与发现"
	if workflow == WorkflowMonitoring {
		kinds = "指标、区间、依赖组与发现"
	}
	result.Summary = fmt.Sprintf("依据固定版本准确汇总、%d/%d 项%s、%d/%d 条证据解读；未提供的资料未纳入审阅，结论须人工核实。", coverage.OutputCount, coverage.TotalOutputs, kinds, coverage.EvidenceCount, coverage.TotalEvidence)
	_, err = ValidateAIWorkflowResult(workflow, result, ids, devices)
	return result, err
}

func aiStatements(workflow string, result model.AnalysisAIResult) [][]model.AnalysisAIStatement {
	if workflow == WorkflowMonitoring {
		return [][]model.AnalysisAIStatement{result.ObservedWeaknesses, result.PrioritizedChecks, result.DependencyObservations, result.Limitations}
	}
	return [][]model.AnalysisAIStatement{result.Interpretations, result.SuggestedVerification, result.Limitations}
}

func ValidateAIResult(result model.AnalysisAIResult, ids, devices []string) ([]string, error) {
	return ValidateAIWorkflowResult(WorkflowDataQuality, result, ids, devices)
}
func ValidateAIWorkflowResult(workflow string, result model.AnalysisAIResult, ids, devices []string) ([]string, error) {
	if !IsAnalysisWorkflow(workflow) || result.Summary == "" || len(result.Summary) > 2000 || !result.Coverage.SummaryProvided {
		return nil, model.ErrAnalysisInvalid
	}
	if workflow == WorkflowMonitoring {
		if result.ObservedWeaknesses == nil || result.PrioritizedChecks == nil || result.DependencyObservations == nil || result.Limitations == nil || result.Interpretations != nil || result.SuggestedVerification != nil {
			return nil, model.ErrAnalysisInvalid
		}
	} else if result.Interpretations == nil || result.SuggestedVerification == nil || result.Limitations == nil || result.ObservedWeaknesses != nil || result.PrioritizedChecks != nil || result.DependencyObservations != nil {
		return nil, model.ErrAnalysisInvalid
	}
	used := []string{}
	for _, list := range aiStatements(workflow, result) {
		if len(list) > 50 {
			return nil, model.ErrAnalysisInvalid
		}
		for _, statement := range list {
			if strings.TrimSpace(statement.Text) == "" || len(statement.Text) > 2000 || len(statement.FactIDs) == 0 || len(statement.FactIDs) > 100 {
				return nil, model.ErrAnalysisInvalid
			}
			for _, id := range statement.FactIDs {
				if !slices.Contains(ids, id) {
					return nil, fmt.Errorf("%w: unprovided AI reference", model.ErrAnalysisInvalid)
				}
				used = append(used, id)
			}
			for _, id := range statement.DeviceIDs {
				if !slices.Contains(devices, id) {
					return nil, ErrForbidden
				}
			}
		}
	}
	slices.Sort(used)
	return slices.Compact(used), nil
}

func marshalAIWorkflowResult(workflow string, result model.AnalysisAIResult) ([]byte, error) {
	if workflow == WorkflowMonitoring {
		return json.Marshal(monitoringAIResult{Summary: result.Summary, ObservedWeaknesses: result.ObservedWeaknesses, PrioritizedChecks: result.PrioritizedChecks, DependencyObservations: result.DependencyObservations, Limitations: result.Limitations, Coverage: result.Coverage})
	}
	return json.Marshal(qualityAIResult{Summary: result.Summary, Interpretations: result.Interpretations, SuggestedVerification: result.SuggestedVerification, Limitations: result.Limitations, Coverage: result.Coverage})
}
