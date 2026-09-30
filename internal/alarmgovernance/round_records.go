package alarmgovernance

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"strings"
)

func (s *Service) roundRecord(ctx context.Context, tx ports.AlarmGovernanceTx, a Actor, c Command, d model.GovernanceDocument, q commandFields) (model.GovernanceDocument, error) {
	var scope struct {
		RoundID string `json:"roundId"`
	}
	_ = json.Unmarshal(c.Body, &scope)
	roundID := c.RoundID
	if roundID == "" {
		roundID = scope.RoundID
	}
	if roundID == "" {
		roundID = d.RoundID
	}
	rd, caseBody, e := s.round(tx, a, roundID)
	if e != nil {
		return d, e
	}
	if c.Operation == "create" || c.Operation == "corrections" {
		old := d
		d = model.GovernanceDocument{Kind: c.Kind, ID: uuid.NewString(), CaseID: rd.CaseID, RoundID: rd.ID, CreatedBy: a.Username, DeviceIDs: rd.DeviceIDs, OccurredAt: s.Now().UnixMilli()}
		if c.Operation == "corrections" {
			d.CorrectsID = old.ID
		}
		c.ExpectedVersion = 0
	} else if d.RoundID != rd.ID {
		return d, invalid("资源不属于当前轮次")
	}
	switch c.Kind {
	case model.GovernanceAlarmLinkKind:
		if c.Operation != "create" && c.Operation != "corrections" {
			return d, invalid("报警关联只能追加更正")
		}
		b, e := decode[model.GovernanceAlarmLink](c.Body)
		if e != nil {
			return d, e
		}
		if len(b.ObservationIDs) == 0 || b.Reason == "" || len(b.CycleRevisionIDs) > 0 {
			return d, invalid("关联须明确具体观察事件与理由；周期关联应使用其明确成员")
		}
		b.RoundID = rd.ID
		b.ConfirmedBy = a.Username
		if e = validateObservations(tx, caseBody.GovernancePoint, b.ObservationIDs); e != nil {
			return d, e
		}
		b.CorrectsID = d.CorrectsID
		if d.CorrectsID != "" && b.CorrectionReason == "" {
			return d, invalid("关联更正须说明原因")
		}
		return put(tx, d, b, 0)
	case model.GovernanceVerificationLinkKind:
		if c.Operation != "create" && c.Operation != "corrections" {
			return d, invalid("核实关联只能追加更正")
		}
		b, e := decode[model.VerificationRoundLink](c.Body)
		if e != nil {
			return d, e
		}
		vdoc, v, e := read[model.FieldVerification](tx, model.GovernanceVerificationKind, b.VerificationID)
		if e != nil {
			return d, e
		}
		if e = s.scope(vdoc, a); e != nil {
			return d, e
		}
		if v.GovernancePoint != caseBody.GovernancePoint || b.VerificationVersion != vdoc.Version || len(b.ObservationIDs) == 0 {
			return d, invalid("核实版本、点位或事件范围不合法")
		}
		for _, id := range b.ObservationIDs {
			if !slices.Contains(v.ObservationIDs, id) {
				return d, invalid("关联不能扩大原核实事件范围")
			}
		}
		b.RoundID = rd.ID
		b.CorrectsID = d.CorrectsID
		if b.Reason == "" {
			return d, invalid("须说明关联依据")
		}
		if d.CorrectsID != "" && b.CorrectionReason == "" {
			return d, invalid("须说明更正原因")
		}
		d.ParentID = b.VerificationID
		return put(tx, d, b, 0)
	case model.GovernanceCauseKind:
		var b model.CauseAssessment
		if c.Operation == "create" || c.Operation == "update" || c.Operation == "corrections" {
			b, e = decode[model.CauseAssessment](c.Body)
		} else {
			b, e = model.GovernanceBody[model.CauseAssessment](d)
		}
		if e != nil {
			return d, e
		}
		if c.Operation == "create" || c.Operation == "corrections" {
			b.Status = "CANDIDATE"
			b.ConfirmedBy = ""
			b.CorrectsID = d.CorrectsID
		} else if c.Operation == "update" {
			old, _ := model.GovernanceBody[model.CauseAssessment](d)
			if old.Status != "CANDIDATE" && old.Status != "PENDING" {
				return d, invalid("正式原因不可覆盖，请更正")
			}
			b.Status = old.Status
			b.CorrectsID = old.CorrectsID
		} else {
			if !slices.Contains([]string{"CANDIDATE", "PENDING"}, b.Status) {
				return d, invalid("正式原因不可重复确认，请更正")
			}
			switch c.Operation {
			case "confirm":
				if len(b.VerificationIDs) == 0 || b.ConfirmationBasis == "" {
					return d, invalid("确认原因须具备核实记录与确认依据")
				}
				covered := map[string]bool{}
				for _, id := range b.VerificationIDs {
					vdoc, v, e := read[model.FieldVerification](tx, model.GovernanceVerificationKind, id)
					if e != nil {
						return d, e
					}
					if e = s.scope(vdoc, a); e != nil {
						return d, e
					}
					if v.Status != "CONFIRMED" || !v.Complete || v.GovernancePoint != caseBody.GovernancePoint {
						return d, invalid("确认原因需要本点位已完整现场核实的记录")
					}
					_, corrections, e := tx.List(model.GovernanceFilter{Kind: model.GovernanceVerificationKind, CorrectsID: vdoc.ID, Status: "CONFIRMED", AllDevices: true, Limit: 1})
					if e != nil {
						return d, e
					}
					if corrections > 0 {
						return d, invalid("原核实已有正式更正，请选择当前有效核实作为确认依据")
					}
					for _, obs := range v.ObservationIDs {
						covered[obs] = true
					}
				}
				for _, obs := range b.ObservationIDs {
					if !covered[obs] {
						return d, invalid("原因事件范围超出核实依据")
					}
				}
				if len(b.SupportEvidenceIDs) == 0 {
					return d, invalid("确认原因须提供支持证据")
				}
				b.Status = "CONFIRMED"
			case "dispute":
				if q.Reason == "" {
					return d, invalid("争议须说明原因")
				}
				b.Status = "DISPUTED"
				b.ConfirmationBasis = q.Reason
			case "reject":
				if q.Reason == "" {
					return d, invalid("拒绝须说明原因")
				}
				b.Status = "REJECTED"
				b.ConfirmationBasis = q.Reason
			default:
				return d, invalid("原因动作不支持")
			}
			b.ConfirmedBy = a.Username
		}
		for _, id := range append(append([]string{}, b.SupportEvidenceIDs...), b.ConflictEvidenceIDs...) {
			if slices.Contains(b.ObservationIDs, id) || slices.Contains(b.VerificationIDs, id) {
				continue
			}
			ad, e := tx.Get(model.GovernanceAttachmentKind, id)
			if e != nil {
				return d, invalid("证据引用不存在或未通过来源授权")
			}
			if !a.covers(ad.DeviceIDs) || !slices.Contains(ad.DeviceIDs, caseBody.DeviceID) {
				return d, ErrForbidden
			}
		}
		if b.Cause == "" || len(b.ObservationIDs) == 0 {
			return d, invalid("原因须明确内容与适用事件")
		}
		if e = validateObservations(tx, caseBody.GovernancePoint, b.ObservationIDs); e != nil {
			return d, e
		}
		if d.CorrectsID != "" && b.CorrectionReason == "" {
			return d, invalid("原因更正须说明原因")
		}
		b.RoundID = rd.ID
		d.Status = b.Status
		return put(tx, d, b, c.ExpectedVersion)
	case model.GovernanceMeasureKind:
		return s.measure(tx, a, c, d, rd, caseBody, q)
	case model.GovernancePlanKind:
		var b model.ObservationPlan
		if c.Operation == "confirm" {
			b, e = model.GovernanceBody[model.ObservationPlan](d)
		} else {
			b, e = decode[model.ObservationPlan](c.Body)
		}
		if e != nil {
			return d, e
		}
		if c.Operation == "create" {
			b.BeforeConditionsHash = model.GovernanceHash(b.BeforeConditions)
			b.AfterConditionsHash = model.GovernanceHash(b.AfterConditions)
			if b.BeforeConditions == "" {
				b.BeforeConditionsHash = ""
			}
			if b.AfterConditions == "" {
				b.AfterConditionsHash = ""
			}
			b.Status = "DRAFT"
		} else if c.Operation == "confirm" {
			if b.Status != "DRAFT" {
				return d, invalid("观察计划已确认，不可覆盖")
			}
			if e = s.measureGate(tx, rd.CaseID, rd.ID); e != nil {
				return d, e
			}
			for id, version := range b.MeasureVersions {
				md, m, e := read[model.ImprovementMeasure](tx, model.GovernanceMeasureKind, id)
				if e != nil {
					return d, e
				}
				if md.Version != version || m.RoundID != rd.ID || !slices.Contains([]string{"IMPLEMENTED", "VERIFIED", "CANCELLED"}, m.Status) {
					return d, model.ErrGovernanceConflict
				}
			}
			for i := range b.MonitoringIntervals {
				v := &b.MonitoringIntervals[i]
				if v.DeviceID != caseBody.DeviceID || v.Start <= 0 || v.End <= v.Start || v.Basis == "" || !slices.Contains([]string{"VERIFIED_MONITORING", "TEST", "STOPPED", "OFFLINE", "GAP", "CLOCK_UNKNOWN"}, v.Kind) {
					return d, invalid("监测区间须具备明确点位、时间、类型和依据")
				}
				v.ConfirmedBy = a.Username
				v.ConfirmedAt = s.Now().UnixMilli()
			}
			b.Status = "CONFIRMED"
		} else {
			return d, invalid("观察计划动作不支持")
		}
		if b.BeforeStart <= 0 || b.BeforeEnd <= b.BeforeStart || b.AfterStart < b.BeforeEnd || b.AfterEnd <= b.AfterStart || b.MinimumMonitoringHours < 0 || b.MinimumActivities < 0 || b.ReviewerUserID == "" || b.CoverageRequirement == "" || b.TimeBasis == "" {
			return d, invalid("观察计划窗口、最低要求、覆盖、时间依据与责任人不完整")
		}
		if len(b.MonitoringIntervals) > 500 {
			return d, invalid("监测区间超过限制")
		}
		b.RoundID = rd.ID
		d.Status = b.Status
		return put(tx, d, b, c.ExpectedVersion)
	case model.GovernanceReviewKind:
		var b model.ObservationReview
		if c.Operation == "confirm" {
			b, e = model.GovernanceBody[model.ObservationReview](d)
		} else {
			b, e = decode[model.ObservationReview](c.Body)
		}
		if e != nil {
			return d, e
		}
		if caseBody.Status != "OBSERVING" {
			return d, invalid("仅观察阶段可以保存评价")
		}
		pd, p, e := read[model.ObservationPlan](tx, model.GovernancePlanKind, b.PlanID)
		if e != nil {
			return d, e
		}
		if p.RoundID != rd.ID || p.Status != "CONFIRMED" || pd.Version != b.PlanVersion {
			return d, model.ErrGovernanceConflict
		}
		if b.DataRevision != caseBody.DataRevision {
			return d, model.ErrGovernanceConflict
		}
		if e = checkSourceVersions(tx, b.SourceRevisionVector); e != nil {
			return d, e
		}
		if !slices.Contains([]string{"IMPROVED", "NOT_IMPROVED", "WORSENED", "INSUFFICIENT_DATA", "NOT_COMPARABLE"}, b.Conclusion) || b.AnalysisSnapshotID == "" || b.FactsHash == "" {
			return d, invalid("评价结论与已完成固定分析快照必填")
		}
		if b.Followup == "" || b.FollowupOwnerUserID == "" {
			return d, invalid("评价须明确后续安排与负责人")
		}
		if c.Operation == "create" || c.Operation == "corrections" {
			b.Status = "DRAFT"
			b.ConfirmedBy = ""
			b.ConfirmedAt = 0
			b.CorrectsID = d.CorrectsID
			if d.CorrectsID != "" && b.CorrectionReason == "" {
				return d, invalid("评价更正须说明原因")
			}
		} else if c.Operation == "confirm" {
			if b.Status != "DRAFT" {
				return d, invalid("正式评价不可覆盖")
			}
			b.Status = "CONFIRMED"
			b.ConfirmedBy = a.Username
			b.ConfirmedAt = s.Now().UnixMilli()
		} else {
			return d, invalid("评价动作不支持")
		}
		b.RoundID = rd.ID
		d.Status = b.Status
		return put(tx, d, b, c.ExpectedVersion)
	}
	return d, invalid("治理资源不支持此动作")
}
func (s *Service) measure(tx ports.AlarmGovernanceTx, a Actor, c Command, d, rd model.GovernanceDocument, caseBody model.GovernanceCase, q commandFields) (model.GovernanceDocument, error) {
	var b model.ImprovementMeasure
	var e error
	if c.Operation == "create" || c.Operation == "update" || c.Operation == "corrections" {
		b, e = decode[model.ImprovementMeasure](c.Body)
	} else {
		b, e = model.GovernanceBody[model.ImprovementMeasure](d)
	}
	if e != nil {
		return d, e
	}
	var input model.ImprovementMeasure
	_ = json.Unmarshal(c.Body, &input)
	if c.Operation == "create" || c.Operation == "corrections" {
		b.Status = "PLANNED"
		b.Implementation = ""
		b.Acceptance = ""
		b.AcceptedBy = ""
		b.AcceptedAt = 0
		b.ImplementedBy = ""
		b.ImplementedAt = 0
		b.CorrectsID = d.CorrectsID
		if d.CorrectsID != "" && b.CorrectionReason == "" {
			return d, invalid("措施更正须说明原因")
		}
	} else {
		switch c.Operation {
		case "update":
			old, _ := model.GovernanceBody[model.ImprovementMeasure](d)
			if old.Status != "PLANNED" {
				return d, invalid("实施后的措施不可覆盖，请追加更正")
			}
			b.Status = old.Status
		case "start":
			if b.Status != "PLANNED" {
				return d, invalid("仅计划措施可开始")
			}
			b.Status = "IN_PROGRESS"
		case "implement":
			if b.Status != "IN_PROGRESS" && b.Status != "PLANNED" {
				return d, invalid("措施已实施或结束")
			}
			if strings.TrimSpace(input.Implementation) == "" {
				return d, invalid("实施记录必填")
			}
			b.Status = "IMPLEMENTED"
			b.Implementation = input.Implementation
			b.ImplementedBy = a.Username
			b.ImplementedAt = s.Now().UnixMilli()
		case "verify":
			if b.Status != "IMPLEMENTED" {
				return d, invalid("验收前须完成实施")
			}
			if b.RequiresAcceptance && b.ImplementedBy == a.Username {
				return d, invalid("必要专业验收须由实施人之外的授权人员完成")
			}
			if input.Acceptance == "" {
				return d, invalid("验收记录与专业依据必填")
			}
			b.Acceptance = input.Acceptance
			b.AcceptedBy = a.Username
			b.AcceptedAt = s.Now().UnixMilli()
			b.Status = "VERIFIED"
		case "cancel":
			if b.Status == "VERIFIED" || b.Status == "CANCELLED" {
				return d, invalid("措施已结束")
			}
			if input.CancellationReason == "" || input.Followup == "" {
				return d, invalid("取消须说明理由与后续安排")
			}
			b.Status = "CANCELLED"
			b.CancellationReason = input.CancellationReason
			b.Followup = input.Followup
		default:
			return d, invalid("措施动作不支持")
		}
	}
	if strings.TrimSpace(b.Content) == "" || b.OwnerUserID == "" || b.DueAt <= 0 || b.Basis == "" {
		return d, invalid("措施内容、负责人、期限与依据必填")
	}
	b.RoundID = rd.ID
	d.Status = b.Status
	d.OwnerUserID = b.OwnerUserID
	out, e := put(tx, d, b, c.ExpectedVersion)
	if e != nil {
		return out, e
	}
	if caseBody.Status == "PENDING_INVESTIGATION" || caseBody.Status == "INVESTIGATING" {
		cd, cb, e := read[model.GovernanceCase](tx, model.GovernanceCaseKind, rd.CaseID)
		if e != nil {
			return out, e
		}
		cb.Status = "IMPROVING"
		cd.Status = cb.Status
		if _, e = put(tx, cd, cb, cd.Version); e != nil {
			return out, e
		}
	}
	return out, nil
}
