// Package response stores exercise records and evaluates fixed response facts.
// It deliberately has no production ingress, alarm mutation, notification, or
// device control capability.
package response

import (
	"fmt"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

const AlgorithmVersion = "response-evidence-v1"

func ValidateProcedure(p model.ResponseProcedure) error {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Scenario) == "" || len(p.Steps) == 0 || len(p.Steps) > 100 {
		return fmt.Errorf("流程名称、场景与步骤无效")
	}
	steps := map[string]model.ResponseStep{}
	for _, step := range p.Steps {
		if step.ID == "" || step.Name == "" || step.Role == "" || step.TargetMs < 0 || len(step.RequiredEvidence) > 20 {
			return fmt.Errorf("步骤标识、岗位、目标或证据要求无效")
		}
		if _, exists := steps[step.ID]; exists {
			return fmt.Errorf("重复步骤 %s", step.ID)
		}
		if len(unique(step.RequiredEvidence)) != len(step.RequiredEvidence) {
			return fmt.Errorf("步骤证据要求重复或为空")
		}
		if step.SystemEventType != "" && !slices.Contains([]string{"ALARM_ACKNOWLEDGED", "ALARM_RECOVERED", "ALARM_CLOSED"}, step.SystemEventType) {
			return fmt.Errorf("系统节点只能引用真实告警事务事件")
		}
		steps[step.ID] = step
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("步骤前置或计时关系存在循环")
		}
		if visited[id] {
			return nil
		}
		step, ok := steps[id]
		if !ok {
			return fmt.Errorf("步骤引用不存在：%s", id)
		}
		visiting[id] = true
		deps := slices.Clone(step.Prerequisites)
		if step.ClockStart != "RUN_START" && step.ClockStart != "PLATFORM_RECEIVED" {
			deps = append(deps, step.ClockStart)
		}
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id], visited[id] = false, true
		return nil
	}
	for _, step := range p.Steps {
		if err := visit(step.ID); err != nil {
			return err
		}
	}
	return nil
}

func unique(values []string) []string {
	result := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

// Evaluate uses only the frozen execution revision and a server cutoff. A
// browser cannot label a click as arrival or turn disputed evidence into fact.
func Evaluate(execution model.ResponseExecution, revisionID string, cutoff int64) (model.ResponseEvaluation, error) {
	result := model.ResponseEvaluation{ExecutionRevisionID: revisionID, ProcedureRevisionID: execution.ProcedureRevisionID, Source: execution.Source, Cutoff: cutoff, Steps: []model.ResponseStepEvaluation{}, Limitations: []string{"完成率和证据覆盖率仅评价记录与单位流程，不评价事故处置安全程度；单位目标不是法规时限"}}
	if err := ValidateProcedure(execution.Procedure); err != nil {
		return result, err
	}
	if revisionID == "" || cutoff <= 0 || !slices.Contains([]string{"DRILL", "REAL_CASE"}, execution.Source) {
		return result, fmt.Errorf("执行版本、截止时间或来源无效")
	}
	if len(execution.Milestones) > 5000 {
		return result, fmt.Errorf("节点数量超过保护上限")
	}
	byID := map[string]model.ResponseMilestone{}
	superseded := map[string]bool{}
	for _, milestone := range execution.Milestones {
		if milestone.ID == "" || milestone.RecordedAt <= 0 || milestone.RecordedAt > cutoff {
			continue
		}
		if _, exists := byID[milestone.ID]; exists {
			return result, fmt.Errorf("重复节点身份")
		}
		byID[milestone.ID] = milestone
		if milestone.CorrectsID != "" {
			superseded[milestone.CorrectsID] = true
		}
	}
	selected := map[string]model.ResponseMilestone{}
	conflicts := map[string]bool{}
	for _, milestone := range byID {
		if superseded[milestone.ID] {
			continue
		}
		if old, ok := selected[milestone.StepID]; ok {
			conflicts[milestone.StepID] = true
			if old.RecordedAt > milestone.RecordedAt || (old.RecordedAt == milestone.RecordedAt && old.ID < milestone.ID) {
				continue
			}
		}
		selected[milestone.StepID] = milestone
	}
	valid := map[string]bool{}
	for _, step := range execution.Procedure.Steps {
		m, exists := selected[step.ID]
		valid[step.ID] = exists && !conflicts[step.ID] && m.Status == "CONFIRMED" && m.OccurredAt > 0 && m.OccurredAt <= m.RecordedAt && m.OccurredAt <= cutoff
	}
	// A dependent node cannot inherit a disputed clock or an unproved required
	// predecessor. Procedure validation has already rejected cycles.
	stepByID := map[string]model.ResponseStep{}
	for _, step := range execution.Procedure.Steps {
		stepByID[step.ID] = step
	}
	qualified := map[string]bool{}
	computed := map[string]bool{}
	var qualifies func(string) bool
	qualifies = func(id string) bool {
		if computed[id] {
			return qualified[id]
		}
		computed[id] = true
		step, m := stepByID[id], selected[id]
		if !valid[id] {
			return false
		}
		kinds := []string{}
		for _, e := range m.Evidence {
			if e.ID != "" && e.SourceID != "" && e.Description != "" {
				kinds = append(kinds, e.Kind)
			}
		}
		for _, required := range step.RequiredEvidence {
			if !slices.Contains(kinds, required) {
				return false
			}
		}
		for _, dep := range step.Prerequisites {
			if !qualifies(dep) || selected[dep].OccurredAt > m.OccurredAt {
				return false
			}
		}
		start := int64(0)
		switch step.ClockStart {
		case "RUN_START":
			start = execution.StartedAt
		case "PLATFORM_RECEIVED":
			start = execution.PlatformReceivedAt
		default:
			if !qualifies(step.ClockStart) {
				return false
			}
			start = selected[step.ClockStart].OccurredAt
		}
		if start > 0 && m.OccurredAt < start {
			return false
		}
		qualified[id] = true
		return true
	}
	for _, step := range execution.Procedure.Steps {
		qualifies(step.ID)
	}
	for _, step := range execution.Procedure.Steps {
		value := model.ResponseStepEvaluation{StepID: step.ID, Name: step.Name, Required: step.Required, Status: "NOT_RECORDED", TargetMs: step.TargetMs, TargetResult: "NOT_EVALUATED", RequiredEvidence: len(step.RequiredEvidence), MissingEvidence: slices.Clone(step.RequiredEvidence), Limitations: []string{}}
		if step.Required {
			result.RequiredSteps++
			result.RequiredEvidence += len(step.RequiredEvidence)
		}
		m, exists := selected[step.ID]
		if exists {
			value.MilestoneID, value.OccurredAt, value.RecordedAt = m.ID, m.OccurredAt, m.RecordedAt
			value.Status = m.Status
			if conflicts[step.ID] {
				value.Status = "DISPUTED"
				value.Limitations = append(value.Limitations, "同一步骤存在未建立更正关系的多条记录")
			}
			if m.OccurredAt <= 0 || m.OccurredAt > m.RecordedAt || m.OccurredAt > cutoff {
				value.Status = "TIME_CONFLICT"
				value.Limitations = append(value.Limitations, "发生时间与登记时间或截止时间冲突")
			}
		}
		start := int64(0)
		switch step.ClockStart {
		case "RUN_START":
			start = execution.StartedAt
		case "PLATFORM_RECEIVED":
			start = execution.PlatformReceivedAt
		default:
			if qualified[step.ClockStart] {
				start = selected[step.ClockStart].OccurredAt
			}
		}
		value.StartAt = start
		prerequisitesValid := true
		if step.ClockStart != "RUN_START" && step.ClockStart != "PLATFORM_RECEIVED" && !qualified[step.ClockStart] {
			prerequisitesValid = false
			value.Limitations = append(value.Limitations, "计时所依赖的步骤未确认："+step.ClockStart)
		}
		for _, id := range step.Prerequisites {
			if !qualified[id] {
				prerequisitesValid = false
				value.Limitations = append(value.Limitations, "前置步骤未确认："+id)
			}
			if valid[id] && exists && selected[id].OccurredAt > m.OccurredAt {
				prerequisitesValid = false
				value.Status = "TIME_CONFLICT"
				value.Limitations = append(value.Limitations, "节点发生早于前置步骤："+id)
			}
		}
		if start <= 0 {
			value.Limitations = append(value.Limitations, "计时起点未确认，不计算耗时")
		}
		if exists && start > 0 && m.OccurredAt < start {
			value.Status = "TIME_CONFLICT"
			value.Limitations = append(value.Limitations, "负耗时保留为时间异常，不修成零")
		}
		if valid[step.ID] && !prerequisitesValid && value.Status != "TIME_CONFLICT" {
			value.Status = "PREREQUISITE_UNCONFIRMED"
		}
		if valid[step.ID] && prerequisitesValid {
			available := []string{}
			for _, evidence := range m.Evidence {
				if evidence.ID != "" && evidence.SourceID != "" && evidence.Description != "" {
					available = append(available, evidence.Kind)
				}
			}
			value.MissingEvidence = []string{}
			for _, required := range step.RequiredEvidence {
				if slices.Contains(available, required) {
					value.ConfirmedEvidence++
				} else {
					value.MissingEvidence = append(value.MissingEvidence, required)
				}
			}
			if start > 0 {
				duration := m.OccurredAt - start
				if duration < 0 {
					value.Status = "TIME_CONFLICT"
					value.Limitations = append(value.Limitations, "负耗时保留为时间异常，不修成零")
				} else {
					value.DurationMs = &duration
					if step.TargetMs > 0 {
						value.TargetResult = "WITHIN_UNIT_TARGET"
						if duration > step.TargetMs {
							value.TargetResult = "EXCEEDS_UNIT_TARGET"
						}
					}
				}
			}
			if value.Status != "TIME_CONFLICT" {
				value.Status = "CONFIRMED"
				if len(value.MissingEvidence) > 0 {
					value.Status = "EVIDENCE_MISSING"
				}
			}
		}
		if (!qualified[step.ID] || value.Status != "CONFIRMED") && start > 0 && cutoff >= start {
			waiting := cutoff - start
			value.WaitingMs = &waiting
		}
		if step.Required {
			result.ConfirmedEvidence += value.ConfirmedEvidence
			if qualified[step.ID] && value.Status == "CONFIRMED" && len(value.MissingEvidence) == 0 {
				result.CompletedRequiredSteps++
			}
		}
		if !exists {
			value.Limitations = append(value.Limitations, "未登记不代表人员未执行")
		}
		result.Steps = append(result.Steps, value)
	}
	if result.RequiredSteps > 0 {
		rate := float64(result.CompletedRequiredSteps) / float64(result.RequiredSteps)
		result.CompletionRate = &rate
	}
	if result.RequiredEvidence > 0 {
		rate := float64(result.ConfirmedEvidence) / float64(result.RequiredEvidence)
		result.EvidenceCoverage = &rate
	}
	if execution.Source == "REAL_CASE" && execution.PlatformReceivedAt == 0 {
		result.Limitations = append(result.Limitations, "可靠平台接收时间缺失，平台确认耗时不可计算")
	}
	return result, nil
}
