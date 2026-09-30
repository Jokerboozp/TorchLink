package alarmgovernance

import (
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"math"
	"slices"
	"strings"
)

const dayMillis int64 = 86400000

func versionRange(point model.GovernancePoint, kind string, start, end *int64) []model.GovernanceSourceVersion {
	key := point.Key(kind)
	if start == nil || *start <= 0 {
		return []model.GovernanceSourceVersion{{DependencyKey: key, BucketStart: -1}}
	}
	last := *start
	if end != nil && *end > *start {
		last = *end - 1
	}
	out := []model.GovernanceSourceVersion{}
	for b := *start / dayMillis * dayMillis; b <= last/dayMillis*dayMillis; b += dayMillis {
		out = append(out, model.GovernanceSourceVersion{DependencyKey: key, BucketStart: b})
	}
	return out
}
func (s *Service) verification(tx ports.AlarmGovernanceTx, a Actor, c Command, d model.GovernanceDocument) (model.GovernanceDocument, error) {
	var b model.FieldVerification
	var e error
	var previous model.FieldVerification
	if c.ID != "" {
		previous, e = model.GovernanceBody[model.FieldVerification](d)
		if e != nil {
			return d, e
		}
	}
	if c.Operation == "confirm" {
		b, e = model.GovernanceBody[model.FieldVerification](d)
	} else {
		b, e = decode[model.FieldVerification](c.Body)
	}
	if e != nil {
		return d, e
	}
	if c.Operation == "create" || c.Operation == "corrections" {
		if c.Operation == "corrections" {
			old, _ := model.GovernanceBody[model.FieldVerification](d)
			if b.CorrectionReason == "" {
				return d, invalid("更正须说明原因")
			}
			if b.GovernancePoint != old.GovernancePoint {
				return d, invalid("更正不能改变原核实点位或信号")
			}
			b.CorrectsID = d.ID
		}
		d = model.GovernanceDocument{ID: uuid.NewString(), Kind: c.Kind, CreatedBy: a.Username, DeviceIDs: []string{b.DeviceID}}
		b.Status = "DRAFT"
		b.ConfirmedAt = 0
		b.ConfirmedBy = ""
		b.Complete = false
		b.RecordedAt = s.Now().UnixMilli()
		c.ExpectedVersion = 0
		b.GovernanceConfigurationRefs, e = s.refs(tx, b.GovernancePoint, b.GovernanceConfigurationRefs)
		if e != nil {
			return d, e
		}
	} else {
		old, _ := model.GovernanceBody[model.FieldVerification](d)
		if old.Status != "DRAFT" {
			return d, invalid("正式核实不可覆盖，请追加更正")
		}
		if c.Operation == "update" {
			if old.GovernancePoint != b.GovernancePoint {
				return d, invalid("草稿点位与信号不可改变")
			}
			b.GovernanceConfigurationRefs = old.GovernanceConfigurationRefs
			b.RecordedAt = old.RecordedAt
			b.Status = "DRAFT"
			b.CorrectsID = old.CorrectsID
			b.CorrectionReason = old.CorrectionReason
		} else if c.Operation == "confirm" {
			if b.VerificationMethod == "UNVERIFIED" {
				return d, invalid("未核实不能确认为完整核实，请保留草稿")
			}
			if b.VerifiedAt == nil || *b.VerifiedAt <= 0 || *b.VerifiedAt > s.Now().UnixMilli() {
				return d, invalid("实际核实时间缺失或晚于登记时间")
			}
			if b.CheckScope == "" {
				return d, invalid("确认须填写实际检查范围")
			}
			for _, ref := range []struct{ kind, id string }{{model.GovernanceTemplateKind, b.TemplateRevisionID}, {model.GovernanceSceneKind, b.ScenePresetRevisionID}, {model.GovernanceProfileKind, b.TypeProfileRevisionID}} {
				_, cfg, e := read[model.GovernanceConfiguration](tx, ref.kind, ref.id)
				if e != nil {
					return d, e
				}
				cfg.Fields = append(cfg.Fields, cfg.FacilityQuestions...)
				if e = validateFieldValues(cfg, b); e != nil {
					return d, e
				}
				for _, field := range cfg.Fields {
					if field.Control != "attachment" || b.FieldValues[field.ID] == nil {
						continue
					}
					values, _ := b.FieldValues[field.ID].([]any)
					ids := make([]string, 0, len(values))
					for _, value := range values {
						ids = append(ids, value.(string))
					}
					if e = checkAttachments(tx, a, b.DeviceID, ids); e != nil {
						return d, e
					}
				}
			}
			b.Status = "CONFIRMED"
			b.ConfirmedBy = a.Username
			b.ConfirmedAt = s.Now().UnixMilli()
			b.Complete = b.VerificationMethod != "DOCUMENT_REVIEW" && b.FieldResult != "UNABLE_TO_DETERMINE" && b.FieldResult != "DISPUTED" && b.ActivityRelation != "UNKNOWN" && b.ActivityRelation != "DISPUTED"
			if x, ok := b.FieldValues["facilityStatus"]; !ok || x == "UNKNOWN" || x == "DISPUTED" {
				b.Complete = false
			}
		} else {
			return d, invalid("核实动作不支持")
		}
	}
	if e = pointValid(b.GovernancePoint); e != nil {
		return d, e
	}
	if len(b.ObservationIDs) == 0 || len(b.ObservationIDs) > 200 {
		return d, invalid("核实必须绑定具体观察事件")
	}
	if e = validateObservations(tx, b.GovernancePoint, b.ObservationIDs); e != nil {
		return d, e
	}
	if !slices.Contains([]string{"ON_SITE", "PHONE", "DOCUMENT_REVIEW", "UNVERIFIED"}, b.VerificationMethod) {
		return d, invalid("核实方式无效")
	}
	allowedResults := []string{"OTHER_ABNORMALITY_OBSERVED", "UNABLE_TO_DETERMINE", "DISPUTED"}
	if strings.EqualFold(b.AlarmType, "FIRE") {
		allowedResults = append(allowedResults, "FIRE_OBSERVED", "NO_FIRE_OBSERVED")
	} else {
		allowedResults = append(allowedResults, "TARGET_ABNORMALITY_OBSERVED", "NO_TARGET_ABNORMALITY_OBSERVED")
	}
	if !slices.Contains(allowedResults, b.FieldResult) {
		return d, invalid("现场结果必须明确选择，允许无法确定")
	}
	if !slices.Contains([]string{"CONFIRMED_RELATED", "CONFIRMED_UNRELATED", "RELATED", "UNRELATED", "SUSPECTED", "UNKNOWN", "DISPUTED"}, b.ActivityRelation) {
		return d, invalid("活动关系须明确选择，允许UNKNOWN")
	}
	for _, id := range b.ActivityIDs {
		ad, e := tx.Get(model.GovernanceActivityKind, id)
		if e != nil {
			return d, e
		}
		if e = s.scope(ad, a); e != nil {
			return d, e
		}
		if !slices.Contains(ad.DeviceIDs, b.DeviceID) {
			return d, invalid("活动不含核实点位")
		}
	}
	if e = checkAttachments(tx, a, b.DeviceID, b.AttachmentIDs); e != nil {
		return d, e
	}
	d.CorrectsID = b.CorrectsID
	d.Status = b.Status
	d.OccurredAt = 0
	if b.VerifiedAt != nil {
		d.OccurredAt = *b.VerifiedAt
	}
	out, e := put(tx, d, b, c.ExpectedVersion)
	if e != nil {
		return out, e
	}
	keys := versionRange(b.GovernancePoint, "VERIFICATION", b.VerifiedAt, b.VerifiedAt)
	if c.ID != "" {
		keys = append(keys, versionRange(previous.GovernancePoint, "VERIFICATION", previous.VerifiedAt, previous.VerifiedAt)...)
	}
	e = tx.BumpSourceVersions(keys)
	return out, e
}
func validateFieldValues(template model.GovernanceConfiguration, b model.FieldVerification) error {
	for _, f := range template.Fields {
		var value any
		switch f.ID {
		case "fieldResult":
			value = b.FieldResult
		case "activityRelation":
			value = b.ActivityRelation
		default:
			value = b.FieldValues[f.ID]
		}
		if value == nil || value == "" {
			if f.Required {
				return invalid("缺少必填字段：%s", f.Label)
			}
			continue
		}
		if f.Control == "single" {
			v, ok := value.(string)
			if !ok {
				return invalid("字段须为单选：%s", f.Label)
			}
			valid := false
			for _, o := range f.Options {
				if o.Code == v {
					valid = true
				}
			}
			if !valid {
				return invalid("字段选项不合法：%s", f.Label)
			}
		}
		if f.Control == "number" {
			v, ok := value.(float64)
			if !ok || math.IsNaN(v) || math.IsInf(v, 0) || f.Min != nil && v < *f.Min || f.Max != nil && v > *f.Max {
				return invalid("数值字段不合法：%s", f.Label)
			}
		}
		if f.Control == "text" {
			v, ok := value.(string)
			if !ok || len(v) > 4000 || f.Required && strings.TrimSpace(v) == "" {
				return invalid("文字字段不合法：%s", f.Label)
			}
		}
		if f.Control == "time" {
			v, ok := value.(float64)
			if !ok || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
				return invalid("时间字段须为有效毫秒时间：%s", f.Label)
			}
		}
		if f.Control == "multi" || f.Control == "attachment" {
			values, ok := value.([]any)
			if !ok || len(values) > 80 || f.Required && len(values) == 0 {
				return invalid("多值字段不合法：%s", f.Label)
			}
			seen := map[string]bool{}
			for _, item := range values {
				v, ok := item.(string)
				if !ok || v == "" || seen[v] {
					return invalid("多值字段值为空或重复：%s", f.Label)
				}
				seen[v] = true
				if f.Control == "multi" && !slices.ContainsFunc(f.Options, func(o model.GovernanceOption) bool { return o.Code == v }) {
					return invalid("字段选项不合法：%s", f.Label)
				}
			}
		}
	}
	return nil
}
func checkAttachments(tx ports.AlarmGovernanceTx, a Actor, device string, ids []string) error {
	if len(ids) > 30 {
		return invalid("附件数量超过限制")
	}
	for _, id := range ids {
		d, e := tx.Get(model.GovernanceAttachmentKind, id)
		if e != nil {
			return e
		}
		if !a.covers(d.DeviceIDs) || !slices.Contains(d.DeviceIDs, device) {
			return ErrForbidden
		}
	}
	return nil
}
func (s *Service) activity(tx ports.AlarmGovernanceTx, a Actor, c Command, d model.GovernanceDocument) (model.GovernanceDocument, error) {
	if c.Kind == model.GovernanceCoverageKind {
		b, e := decode[model.ActivityCoverage](c.Body)
		if e != nil {
			return d, e
		}
		if b.StartAt <= 0 || b.EndAt <= b.StartAt || b.EndAt-b.StartAt > 366*dayMillis || !slices.Contains([]string{"FULL_DECLARED", "PARTIAL", "ALARM_ONLY", "UNKNOWN"}, b.Coverage) || b.Basis == "" || b.ActivityType == "" {
			return d, invalid("覆盖须明确类型、半开区间、声明口径和依据")
		}
		b.DeviceIDs = unique(b.DeviceIDs)
		if len(b.DeviceIDs) == 0 || !a.covers(b.DeviceIDs) {
			return d, ErrForbidden
		}
		keys := []model.GovernanceSourceVersion{}
		b.RevisionNumber = 1
		if c.Operation == "revisions" {
			old, _ := model.GovernanceBody[model.ActivityCoverage](d)
			if b.CorrectionReason == "" {
				return d, invalid("覆盖更正须说明原因")
			}
			b.CoverageID = old.CoverageID
			b.CorrectsID = d.ID
			items, _, e := tx.List(model.GovernanceFilter{Kind: c.Kind, ResourceID: old.CoverageID, AllDevices: true, Limit: 100})
			if e != nil {
				return d, e
			}
			for _, x := range items {
				b.RevisionNumber = max(b.RevisionNumber, x.RevisionNumber+1)
			}
			for _, id := range old.DeviceIDs {
				keys = append(keys, versionRange(model.GovernancePoint{DeviceID: id}, "COVERAGE", &old.StartAt, &old.EndAt)...)
			}
		} else if c.Operation != "create" {
			return d, invalid("覆盖只能追加版本")
		}
		if b.CoverageID == "" {
			b.CoverageID = uuid.NewString()
		}
		b.DeclaredBy = a.Username
		d = model.GovernanceDocument{ID: uuid.NewString(), Kind: c.Kind, ResourceID: b.CoverageID, RevisionNumber: b.RevisionNumber, CreatedBy: a.Username, DeviceIDs: b.DeviceIDs, OccurredAt: b.StartAt, CorrectsID: b.CorrectsID, Status: b.Coverage}
		out, e := put(tx, d, b, 0)
		if e != nil {
			return out, e
		}
		for _, id := range b.DeviceIDs {
			keys = append(keys, versionRange(model.GovernancePoint{DeviceID: id}, "COVERAGE", &b.StartAt, &b.EndAt)...)
		}
		return out, tx.BumpSourceVersions(keys)
	}
	var b model.FieldActivityRevision
	var e error
	old := model.FieldActivityRevision{}
	if c.ID != "" {
		old, e = model.GovernanceBody[model.FieldActivityRevision](d)
		if e != nil {
			return d, e
		}
	}
	if c.Operation == "confirm" {
		b = old
		b.Status = "CONFIRMED"
	} else {
		b, e = decode[model.FieldActivityRevision](c.Body)
		if e != nil {
			return d, e
		}
		b.Status = "DRAFT"
	}
	if c.Operation != "create" && c.Operation != "revisions" && c.Operation != "confirm" {
		return d, invalid("活动动作不支持")
	}
	if len(b.DeviceIDs) == 0 || !a.covers(b.DeviceIDs) || b.ActivityType == "" || b.Unit == "" || strings.TrimSpace(b.Location) == "" {
		return d, invalid("活动必须明确位置、成员点位、类型和单位")
	}
	if b.StartAt != nil && b.EndAt != nil && (*b.EndAt <= *b.StartAt || *b.EndAt-*b.StartAt > 366*dayMillis) {
		return d, invalid("活动起止区间无效")
	}
	if !slices.Contains([]string{"TRUSTED", "UNVERIFIED", "ESTIMATED", "MISSING", "CONFLICTED"}, b.TimeQuality) {
		return d, invalid("须明确时间质量")
	}
	if c.Operation == "confirm" && (!b.Actual || b.StartAt == nil || *b.StartAt <= 0 || *b.StartAt > s.Now().UnixMilli() || b.EndAt != nil && *b.EndAt > s.Now().UnixMilli()) {
		return d, invalid("计划或起点未知的活动不能确认为实际活动")
	}
	keys := []model.GovernanceSourceVersion{}
	b.RevisionNumber = 1
	if c.ID != "" {
		b.ActivityID = old.ActivityID
		b.CorrectsID = d.ID
		if c.Operation == "revisions" && b.CorrectionReason == "" {
			return d, invalid("活动更正须说明原因")
		}
		items, _, e := tx.List(model.GovernanceFilter{Kind: c.Kind, ResourceID: b.ActivityID, AllDevices: true, Limit: 100})
		if e != nil {
			return d, e
		}
		for _, x := range items {
			b.RevisionNumber = max(b.RevisionNumber, x.RevisionNumber+1)
		}
		for _, id := range old.DeviceIDs {
			keys = append(keys, versionRange(model.GovernancePoint{DeviceID: id}, "ACTIVITY", old.StartAt, old.EndAt)...)
		}
	} else if b.ActivityID != "" {
		return d, invalid("新活动ID由服务端生成")
	}
	if b.ActivityID == "" {
		b.ActivityID = uuid.NewString()
	}
	if b.TemplateRevisionID == "" {
		b.TemplateRevisionID = "template-general-v1"
	}
	if b.ScenePresetRevisionID == "" {
		b.ScenePresetRevisionID = "scene-general-v1"
	}
	for _, ref := range []struct {
		kind, id string
		hash     *string
	}{{model.GovernanceTemplateKind, b.TemplateRevisionID, &b.TemplateHash}, {model.GovernanceSceneKind, b.ScenePresetRevisionID, &b.ScenePresetHash}} {
		_, cfg, e := read[model.GovernanceConfiguration](tx, ref.kind, ref.id)
		if e != nil {
			return d, e
		}
		if cfg.Status != "PUBLISHED" {
			return d, invalid("新活动版本只能采用已发布且未退休表单")
		}
		if len(cfg.DeviceIDs) > 0 {
			for _, id := range b.DeviceIDs {
				if !slices.Contains(cfg.DeviceIDs, id) {
					return d, invalid("活动表单未覆盖全部成员设备")
				}
			}
		}
		*ref.hash = cfg.Hash
	}
	d = model.GovernanceDocument{ID: uuid.NewString(), Kind: c.Kind, ResourceID: b.ActivityID, RevisionNumber: b.RevisionNumber, CreatedBy: a.Username, DeviceIDs: unique(b.DeviceIDs), Status: b.Status, CorrectsID: b.CorrectsID}
	if b.StartAt != nil {
		d.OccurredAt = *b.StartAt
	}
	out, e := put(tx, d, b, 0)
	if e != nil {
		return out, e
	}
	for _, id := range b.DeviceIDs {
		keys = append(keys, versionRange(model.GovernancePoint{DeviceID: id}, "ACTIVITY", b.StartAt, b.EndAt)...)
	}
	return out, tx.BumpSourceVersions(keys)
}
