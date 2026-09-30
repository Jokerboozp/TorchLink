package alarmgovernance

import (
	"encoding/json"
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"strings"
)

func pointValid(p model.GovernancePoint) error {
	if p.DeviceID == "" || p.AlarmType == "" || p.SignalKey == "" || !slices.Contains([]string{"COMPONENT_STATE", "DEVICE_DIRECT", "RULE_LIFECYCLE"}, p.OriginKind) {
		return invalid("须明确点位、原始报警类型、来源语义和signalKey")
	}
	return nil
}
func (s *Service) caseCommand(tx ports.AlarmGovernanceTx, a Actor, q Command, d model.GovernanceDocument, f commandFields) (model.GovernanceDocument, error) {
	if q.Operation == "create" {
		b, e := decode[model.GovernanceCase](q.Body)
		if e != nil {
			return d, e
		}
		if e = pointValid(b.GovernancePoint); e != nil {
			return d, e
		}
		if e = validateObservations(tx, b.GovernancePoint, b.ObservationIDs); e != nil {
			return d, e
		}
		if strings.TrimSpace(b.Title) == "" || b.OwnerUserID == "" {
			return d, invalid("事项标题与负责人必填")
		}
		if !a.covers([]string{b.DeviceID}) {
			return d, ErrForbidden
		}
		b.GovernanceConfigurationRefs, e = s.refs(tx, b.GovernancePoint, b.GovernanceConfigurationRefs)
		if e != nil {
			return d, e
		}
		b.Status = "PENDING_INVESTIGATION"
		b.DataRevision = 1
		b.CurrentRoundID = uuid.NewString()
		b.AssetInstanceID = "UNKNOWN"
		b.IdentityQuality = "UNKNOWN"
		d = model.GovernanceDocument{ID: uuid.NewString(), Kind: model.GovernanceCaseKind, CreatedBy: a.Username, DeviceIDs: []string{b.DeviceID}, Status: b.Status, OwnerUserID: b.OwnerUserID, PointKey: b.GovernancePoint.Key("")}
		d, e = put(tx, d, b, 0)
		if e != nil {
			return d, e
		}
		round := model.GovernanceRound{GovernanceConfigurationRefs: b.GovernanceConfigurationRefs, CaseID: d.ID, Number: 1, Status: "ACTIVE", StartedAt: s.Now().UnixMilli(), AssetInstanceID: b.AssetInstanceID, IdentityQuality: b.IdentityQuality, LocationSnapshot: b.Location}
		_, e = put(tx, model.GovernanceDocument{ID: b.CurrentRoundID, Kind: model.GovernanceRoundKind, CaseID: d.ID, RevisionNumber: 1, CreatedBy: a.Username, DeviceIDs: d.DeviceIDs, Status: "ACTIVE"}, round, 0)
		if e != nil {
			return d, e
		}
		if len(b.ObservationIDs) > 0 {
			_, e = put(tx, model.GovernanceDocument{ID: uuid.NewString(), Kind: model.GovernanceAlarmLinkKind, CaseID: d.ID, RoundID: b.CurrentRoundID, CreatedBy: a.Username, DeviceIDs: d.DeviceIDs}, model.GovernanceAlarmLink{RoundID: b.CurrentRoundID, ObservationIDs: unique(b.ObservationIDs), Reason: "建立事项时人工关联", ConfirmedBy: a.Username}, 0)
		}
		return d, e
	}
	b, e := model.GovernanceBody[model.GovernanceCase](d)
	if e != nil {
		return d, e
	}
	if e = validateObservations(tx, b.GovernancePoint, f.ObservationIDs); e != nil {
		return d, e
	}
	rd, r, e := read[model.GovernanceRound](tx, model.GovernanceRoundKind, b.CurrentRoundID)
	if e != nil {
		return d, e
	}
	switch q.Operation {
	case "confirm-identity":
		incoming, e := decode[model.GovernanceCase](q.Body)
		if e != nil {
			return d, e
		}
		if b.IdentityQuality != "UNKNOWN" || r.Status != "ACTIVE" || incoming.AssetInstanceID == "" || incoming.AssetInstanceID == "UNKNOWN" || incoming.IdentityBasis == "" {
			return d, invalid("仅首次未知实物允许人工确认，并须提供明确标识和现场依据；更换实物应建立关联新事项")
		}
		b.AssetInstanceID = incoming.AssetInstanceID
		b.IdentityQuality = "CONFIRMED"
		b.IdentityBasis = incoming.IdentityBasis
		b.DataRevision++
		r.AssetInstanceID = b.AssetInstanceID
		r.IdentityQuality = b.IdentityQuality
		r.IdentityBasis = b.IdentityBasis
		if _, e = put(tx, rd, r, rd.Version); e != nil {
			return d, e
		}
		if e = tx.BumpSourceVersions([]model.GovernanceSourceVersion{{DependencyKey: b.GovernancePoint.Key("IDENTITY"), BucketStart: -1}, {DependencyKey: model.GovernancePoint{DeviceID: b.DeviceID}.Key("IDENTITY"), BucketStart: -1}}); e != nil {
			return d, e
		}
	case "update":
		incoming, e := decode[model.GovernanceCase](q.Body)
		if e != nil {
			return d, e
		}
		if incoming.Title != "" {
			b.Title = incoming.Title
		}
		if incoming.Location != "" && incoming.Location != b.Location {
			return d, invalid("位置变更须在新轮次记录历史快照，不可覆盖当前事实")
		}
	case "assign":
		if f.OwnerUserID == "" {
			return d, invalid("负责人必填")
		}
		b.OwnerUserID = f.OwnerUserID
	case "start":
		if b.Status != "PENDING_INVESTIGATION" || r.Status != "ACTIVE" {
			return d, invalid("当前事项不能开始核查")
		}
		if s.ResolveTx != nil {
			owner, e := s.ResolveTx(tx, Actor{TenantID: a.TenantID, Username: b.OwnerUserID})
			if e != nil || !owner.menu() || !owner.covers(d.DeviceIDs) {
				return d, invalid("当前负责人已失效，请先重新指派")
			}
		}
		b.Status = "INVESTIGATING"
	case "start-observation":
		if b.Status != "IMPROVING" && b.Status != "INVESTIGATING" {
			return d, invalid("当前事项不能开始观察")
		}
		if e = s.measureGate(tx, r.CaseID, rd.ID); e != nil {
			return d, e
		}
		plans, _, e := tx.List(model.GovernanceFilter{Kind: model.GovernancePlanKind, RoundID: rd.ID, Status: "CONFIRMED", AllDevices: true, Limit: 100})
		if e != nil {
			return d, e
		}
		if len(plans) == 0 {
			return d, invalid("须先确认观察计划")
		}
		b.Status = "OBSERVING"
	case "complete":
		if b.Status != "OBSERVING" || r.Status != "ACTIVE" {
			return d, invalid("仅观察中的活动轮次可以完成")
		}
		if e = s.measureGate(tx, d.ID, rd.ID); e != nil {
			return d, e
		}
		if f.ReviewID == "" {
			return d, invalid("须选择已正式确认评价")
		}
		reviewDoc, review, e := read[model.ObservationReview](tx, model.GovernanceReviewKind, f.ReviewID)
		if e != nil {
			return d, e
		}
		if review.RoundID != rd.ID || review.Status != "CONFIRMED" || review.Followup == "" || review.FollowupOwnerUserID == "" {
			return d, invalid("评价必须属于本轮、已确认并明确后续安排与责任人")
		}
		if s.ResolveTx != nil {
			owner, e := s.ResolveTx(tx, Actor{TenantID: a.TenantID, Username: review.FollowupOwnerUserID})
			if e != nil || !owner.menu() || !owner.covers(d.DeviceIDs) {
				return d, invalid("后续负责人已失效，请更正评价安排")
			}
		}
		if f.DataRevision != b.DataRevision || review.DataRevision != b.DataRevision || f.AnalysisSnapshotID != review.AnalysisSnapshotID || f.FactsHash != review.FactsHash {
			return d, model.ErrGovernanceConflict
		}
		if e = checkSourceVersions(tx, review.SourceRevisionVector); e != nil {
			return d, e
		}
		resources := []model.GovernanceDocument{}
		for _, kind := range []string{model.GovernanceAlarmLinkKind, model.GovernanceVerificationLinkKind, model.GovernanceCauseKind, model.GovernanceMeasureKind, model.GovernancePlanKind, model.GovernanceReviewKind} {
			items, total, e := tx.List(model.GovernanceFilter{Kind: kind, RoundID: rd.ID, AllDevices: true, Limit: 100})
			if e != nil {
				return d, e
			}
			if total > 100 {
				return d, invalid("本轮资料超过报告单批限制，需分段确认")
			}
			resources = append(resources, items...)
		}
		business, total, e := tx.List(model.GovernanceFilter{Kind: model.GovernanceBusinessLinkKind, CaseID: d.ID, AllDevices: true, Limit: 100})
		if e != nil {
			return d, e
		}
		if total > 100 {
			return d, invalid("关联来源超过报告单批限制")
		}
		superseded := map[string]bool{}
		for _, link := range business {
			if link.CorrectsID != "" {
				superseded[link.CorrectsID] = true
			}
		}
		for _, link := range business {
			if !superseded[link.ID] {
				if !a.covers(link.DeviceIDs) {
					return d, ErrForbidden
				}
				resources = append(resources, link)
			}
		}
		report := model.GovernanceReport{CaseID: d.ID, RoundID: rd.ID, ReviewID: reviewDoc.ID, ReviewVersion: reviewDoc.Version, AnalysisSnapshotID: review.AnalysisSnapshotID, FactsHash: review.FactsHash, DataRevision: review.DataRevision, SourceRevisionVector: review.SourceRevisionVector, Conclusion: review.Conclusion, Limitations: review.Limitations, Followup: review.Followup, FollowupOwnerUserID: review.FollowupOwnerUserID, ConfirmedBy: a.Username, ConfirmedAt: s.Now().UnixMilli(), Resources: resources}
		reportDoc, e := put(tx, model.GovernanceDocument{ID: uuid.NewString(), Kind: model.GovernanceReportKind, CaseID: d.ID, RoundID: rd.ID, DeviceIDs: d.DeviceIDs, CreatedBy: a.Username, Status: "CONFIRMED"}, report, 0)
		if e != nil {
			return d, e
		}
		r.ReportID = reportDoc.ID
		r.Status = "COMPLETED"
		end := s.Now().UnixMilli()
		r.EndedAt = &end
		r.EndReason = f.Reason
		rd.Status = r.Status
		if _, e = put(tx, rd, r, rd.Version); e != nil {
			return d, e
		}
		b.Status = "COMPLETED"
	case "cancel":
		if r.Status != "ACTIVE" || f.Reason == "" {
			return d, invalid("取消须说明原因和未完成工作去向")
		}
		measures, total, e := tx.List(model.GovernanceFilter{Kind: model.GovernanceMeasureKind, RoundID: rd.ID, AllDevices: true, Limit: 100})
		if e != nil {
			return d, e
		}
		if total > 100 {
			return d, invalid("措施超过取消校验限制")
		}
		for _, m := range effectiveDocuments(measures) {
			mb, _ := model.GovernanceBody[model.ImprovementMeasure](m)
			if !slices.Contains([]string{"VERIFIED", "CANCELLED"}, mb.Status) && mb.Required {
				return d, invalid("必要措施须先明确取消理由与后续安排")
			}
		}
		r.Status = "CANCELLED"
		end := s.Now().UnixMilli()
		r.EndedAt = &end
		r.EndReason = f.Reason
		rd.Status = r.Status
		if _, e = put(tx, rd, r, rd.Version); e != nil {
			return d, e
		}
		b.Status = "CANCELLED"
	case "reopen":
		if b.Status != "COMPLETED" || r.Status != "COMPLETED" || len(f.ObservationIDs) == 0 || f.Reason == "" {
			return d, invalid("重开须人工核对新事件并说明原因")
		}
		oldLinks, total, e := tx.List(model.GovernanceFilter{Kind: model.GovernanceAlarmLinkKind, CaseID: d.ID, AllDevices: true, Limit: 100})
		if e != nil {
			return d, e
		}
		if total > 100 {
			return d, invalid("历史关联超过单批校验范围")
		}
		known := map[string]bool{}
		for _, link := range oldLinks {
			body, _ := model.GovernanceBody[model.GovernanceAlarmLink](link)
			for _, id := range body.ObservationIDs {
				known[id] = true
			}
		}
		hasNew := false
		for _, id := range f.ObservationIDs {
			if !known[id] {
				hasNew = true
			}
		}
		if !hasNew {
			return d, invalid("所选事件已经在历史轮次关联，重开须人工核对新增事件")
		}
		var req model.GovernanceCase
		_ = json.Unmarshal(q.Body, &req)
		refs := b.GovernanceConfigurationRefs
		if req.TemplateRevisionID != "" {
			refs.TemplateRevisionID = req.TemplateRevisionID
		}
		if req.ScenePresetRevisionID != "" {
			refs.ScenePresetRevisionID = req.ScenePresetRevisionID
		}
		if req.TypeProfileRevisionID != "" {
			refs.TypeProfileRevisionID = req.TypeProfileRevisionID
		}
		refs, e = s.refs(tx, b.GovernancePoint, refs)
		if e != nil {
			return d, e
		}
		newID := uuid.NewString()
		newRound := model.GovernanceRound{GovernanceConfigurationRefs: refs, CaseID: d.ID, Number: r.Number + 1, PreviousRoundID: rd.ID, Status: "ACTIVE", StartedAt: s.Now().UnixMilli(), AssetInstanceID: r.AssetInstanceID, IdentityQuality: r.IdentityQuality, LocationSnapshot: r.LocationSnapshot}
		if _, e = put(tx, model.GovernanceDocument{ID: newID, Kind: model.GovernanceRoundKind, CaseID: d.ID, CreatedBy: a.Username, DeviceIDs: d.DeviceIDs, RevisionNumber: newRound.Number, Status: "ACTIVE"}, newRound, 0); e != nil {
			return d, e
		}
		if _, e = put(tx, model.GovernanceDocument{ID: uuid.NewString(), Kind: model.GovernanceAlarmLinkKind, CaseID: d.ID, RoundID: newID, CreatedBy: a.Username, DeviceIDs: d.DeviceIDs}, model.GovernanceAlarmLink{RoundID: newID, ObservationIDs: unique(f.ObservationIDs), Reason: f.Reason, ConfirmedBy: a.Username}, 0); e != nil {
			return d, e
		}
		b.CurrentRoundID = newID
		b.Status = "PENDING_INVESTIGATION"
		b.DataRevision++
	default:
		return d, invalid("事项动作不支持")
	}
	d.Status = b.Status
	d.OwnerUserID = b.OwnerUserID
	return put(tx, d, b, d.Version)
}
func (s *Service) measureGate(tx ports.AlarmGovernanceTx, caseID, roundID string) error {
	items, total, e := tx.List(model.GovernanceFilter{Kind: model.GovernanceMeasureKind, RoundID: roundID, AllDevices: true, Limit: 100})
	if e != nil {
		return e
	}
	if total > 100 {
		return invalid("措施超过校验限制")
	}
	for _, d := range effectiveDocuments(items) {
		b, _ := model.GovernanceBody[model.ImprovementMeasure](d)
		if b.Required && (b.Status != "IMPLEMENTED" && b.Status != "VERIFIED" && b.Status != "CANCELLED" || b.RequiresAcceptance && b.Status != "VERIFIED" && b.Status != "CANCELLED") {
			return invalid("必要措施尚未实施或验收")
		}
		if b.Required && b.Status == "CANCELLED" && (b.CancellationReason == "" || b.Followup == "") {
			return invalid("取消措施缺少理由或后续安排")
		}
	}
	return nil
}
func effectiveDocuments(items []model.GovernanceDocument) []model.GovernanceDocument {
	superseded := map[string]bool{}
	for _, d := range items {
		if d.CorrectsID != "" {
			superseded[d.CorrectsID] = true
		}
	}
	current := make([]model.GovernanceDocument, 0, len(items))
	for _, d := range items {
		if !superseded[d.ID] {
			current = append(current, d)
		}
	}
	return current
}
func checkSourceVersions(tx ports.AlarmGovernanceTx, expected []model.GovernanceSourceVersion) error {
	if len(expected) == 0 {
		return invalid("正式确认缺少来源版本向量")
	}
	current, e := tx.SourceVersions(expected)
	if e != nil {
		return e
	}
	for _, v := range current {
		found := false
		for _, x := range expected {
			if x.DependencyKey == v.DependencyKey && x.BucketStart == v.BucketStart {
				found = true
				if x.Generation != v.Generation {
					return model.ErrGovernanceConflict
				}
			}
		}
		if !found {
			return model.ErrGovernanceConflict
		}
	}
	return nil
}
