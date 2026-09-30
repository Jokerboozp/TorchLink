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

type rulePolicyAIResult struct {
	Summary                 string                      `json:"summary"`
	BehaviorDifferences     []model.AnalysisAIStatement `json:"behaviorDifferences"`
	VerificationSuggestions []model.AnalysisAIStatement `json:"verificationSuggestions"`
	Limitations             []model.AnalysisAIStatement `json:"limitations"`
	CandidateDraft          *model.AlarmRule            `json:"candidateDraft,omitempty"`
	CandidateRevisionID     string                      `json:"candidateRevisionId,omitempty"`
	Coverage                model.AnalysisAICoverage    `json:"coverage"`
}

type responseAIResult struct {
	Summary                string                      `json:"summary"`
	ObservedBottlenecks    []model.AnalysisAIStatement `json:"observedBottlenecks"`
	EvidenceGaps           []model.AnalysisAIStatement `json:"evidenceGaps"`
	ImprovementSuggestions []model.AnalysisAIStatement `json:"improvementSuggestions"`
	Limitations            []model.AnalysisAIStatement `json:"limitations"`
	Coverage               model.AnalysisAICoverage    `json:"coverage"`
}
type maintenanceAIResult struct {
	Summary                 string                      `json:"summary"`
	ObservedChanges         []model.AnalysisAIStatement `json:"observedChanges"`
	Confounders             []model.AnalysisAIStatement `json:"confounders"`
	VerificationSuggestions []model.AnalysisAIStatement `json:"verificationSuggestions"`
	Limitations             []model.AnalysisAIStatement `json:"limitations"`
	Coverage                model.AnalysisAICoverage    `json:"coverage"`
}
type investmentAIResult struct {
	Summary                string                      `json:"summary"`
	PriorityExplanations   []model.AnalysisAIStatement `json:"priorityExplanations"`
	DecisionConsiderations []model.AnalysisAIStatement `json:"decisionConsiderations"`
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
	case WorkflowRulePolicy:
		var decoded rulePolicyAIResult
		err = decodeAIJSON(answer, &decoded)
		if decoded.CandidateRevisionID != "" {
			return result, model.ErrAnalysisInvalid
		}
		result.BehaviorDifferences, result.VerificationSuggestions, result.Limitations, result.CandidateDraft = decoded.BehaviorDifferences, decoded.VerificationSuggestions, decoded.Limitations, decoded.CandidateDraft
	case WorkflowResponse:
		var decoded responseAIResult
		err = decodeAIJSON(answer, &decoded)
		result.ObservedBottlenecks, result.EvidenceGaps, result.ImprovementSuggestions, result.Limitations = decoded.ObservedBottlenecks, decoded.EvidenceGaps, decoded.ImprovementSuggestions, decoded.Limitations
	case WorkflowMaintenance:
		var decoded maintenanceAIResult
		err = decodeAIJSON(answer, &decoded)
		result.ObservedChanges, result.Confounders, result.VerificationSuggestions, result.Limitations = decoded.ObservedChanges, decoded.Confounders, decoded.VerificationSuggestions, decoded.Limitations
	case WorkflowInvestment:
		var decoded investmentAIResult
		err = decodeAIJSON(answer, &decoded)
		result.PriorityExplanations, result.DecisionConsiderations, result.Limitations = decoded.PriorityExplanations, decoded.DecisionConsiderations, decoded.Limitations
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
	} else if workflow == WorkflowRulePolicy {
		kinds = "结果、差异、指标、发现与标签"
	} else if workflow == WorkflowResponse {
		kinds = "流程节点、计算结果与记录缺口"
	} else if workflow == WorkflowMaintenance {
		kinds = "维护观察、变化指标与发现"
	} else if workflow == WorkflowInvestment {
		kinds = "既有排序、预算试算与发现"
	}
	result.Summary = fmt.Sprintf("依据固定版本准确汇总、%d/%d 项%s、%d/%d 条证据解读；未提供的资料未纳入审阅，结论须人工核实。", coverage.OutputCount, coverage.TotalOutputs, kinds, coverage.EvidenceCount, coverage.TotalEvidence)
	_, err = ValidateAIWorkflowResult(workflow, result, ids, devices)
	return result, err
}

func aiResultFields(result model.AnalysisAIResult) map[string][]model.AnalysisAIStatement {
	return map[string][]model.AnalysisAIStatement{
		"interpretations": result.Interpretations, "suggestedVerification": result.SuggestedVerification,
		"observedWeaknesses": result.ObservedWeaknesses, "prioritizedChecks": result.PrioritizedChecks, "dependencyObservations": result.DependencyObservations,
		"behaviorDifferences": result.BehaviorDifferences, "verificationSuggestions": result.VerificationSuggestions,
		"observedBottlenecks": result.ObservedBottlenecks, "evidenceGaps": result.EvidenceGaps, "improvementSuggestions": result.ImprovementSuggestions,
		"observedChanges": result.ObservedChanges, "confounders": result.Confounders,
		"priorityExplanations": result.PriorityExplanations, "decisionConsiderations": result.DecisionConsiderations,
		"limitations": result.Limitations,
	}
}
func aiResultFieldNames(workflow string) []string {
	switch workflow {
	case WorkflowDataQuality:
		return []string{"interpretations", "suggestedVerification", "limitations"}
	case WorkflowMonitoring:
		return []string{"observedWeaknesses", "prioritizedChecks", "dependencyObservations", "limitations"}
	case WorkflowRulePolicy:
		return []string{"behaviorDifferences", "verificationSuggestions", "limitations"}
	case WorkflowResponse:
		return []string{"observedBottlenecks", "evidenceGaps", "improvementSuggestions", "limitations"}
	case WorkflowMaintenance:
		return []string{"observedChanges", "confounders", "verificationSuggestions", "limitations"}
	case WorkflowInvestment:
		return []string{"priorityExplanations", "decisionConsiderations", "limitations"}
	}
	return nil
}
func aiStatements(workflow string, result model.AnalysisAIResult) [][]model.AnalysisAIStatement {
	fields := aiResultFields(result)
	lists := [][]model.AnalysisAIStatement{}
	for _, name := range aiResultFieldNames(workflow) {
		lists = append(lists, fields[name])
	}
	return lists
}

func ValidateAIResult(result model.AnalysisAIResult, ids, devices []string) ([]string, error) {
	return ValidateAIWorkflowResult(WorkflowDataQuality, result, ids, devices)
}
func ValidateAIWorkflowResult(workflow string, result model.AnalysisAIResult, ids, devices []string) ([]string, error) {
	if !IsAnalysisWorkflow(workflow) || result.Summary == "" || len(result.Summary) > 2000 || !result.Coverage.SummaryProvided {
		return nil, model.ErrAnalysisInvalid
	}
	if workflow != WorkflowRulePolicy && (result.CandidateDraft != nil || result.CandidateRevisionID != "" || result.PreparedCandidate != nil) {
		return nil, model.ErrAnalysisInvalid
	}
	if workflow == WorkflowRulePolicy && result.CandidateDraft != nil && result.CandidateDraft.Enabled {
		return nil, model.ErrAnalysisInvalid
	}
	allowed := aiResultFieldNames(workflow)
	for name, list := range aiResultFields(result) {
		if slices.Contains(allowed, name) {
			if list == nil {
				return nil, model.ErrAnalysisInvalid
			}
		} else if list != nil {
			return nil, model.ErrAnalysisInvalid
		}
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
	if workflow == WorkflowResponse {
		return json.Marshal(responseAIResult{Summary: result.Summary, ObservedBottlenecks: result.ObservedBottlenecks, EvidenceGaps: result.EvidenceGaps, ImprovementSuggestions: result.ImprovementSuggestions, Limitations: result.Limitations, Coverage: result.Coverage})
	}
	if workflow == WorkflowMaintenance {
		return json.Marshal(maintenanceAIResult{Summary: result.Summary, ObservedChanges: result.ObservedChanges, Confounders: result.Confounders, VerificationSuggestions: result.VerificationSuggestions, Limitations: result.Limitations, Coverage: result.Coverage})
	}
	if workflow == WorkflowInvestment {
		return json.Marshal(investmentAIResult{Summary: result.Summary, PriorityExplanations: result.PriorityExplanations, DecisionConsiderations: result.DecisionConsiderations, Limitations: result.Limitations, Coverage: result.Coverage})
	}
	if workflow == WorkflowRulePolicy {
		return json.Marshal(rulePolicyAIResult{Summary: result.Summary, BehaviorDifferences: result.BehaviorDifferences, VerificationSuggestions: result.VerificationSuggestions, Limitations: result.Limitations, CandidateDraft: result.CandidateDraft, CandidateRevisionID: result.CandidateRevisionID, Coverage: result.Coverage})
	}
	if workflow == WorkflowMonitoring {
		return json.Marshal(monitoringAIResult{Summary: result.Summary, ObservedWeaknesses: result.ObservedWeaknesses, PrioritizedChecks: result.PrioritizedChecks, DependencyObservations: result.DependencyObservations, Limitations: result.Limitations, Coverage: result.Coverage})
	}
	return json.Marshal(qualityAIResult{Summary: result.Summary, Interpretations: result.Interpretations, SuggestedVerification: result.SuggestedVerification, Limitations: result.Limitations, Coverage: result.Coverage})
}
