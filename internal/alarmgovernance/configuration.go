package alarmgovernance

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"strings"
)

func decodeConfiguration(raw json.RawMessage) (model.GovernanceConfiguration, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return model.GovernanceConfiguration{}, invalid("配置正文格式错误")
	}
	delete(body, "expectedVersion")
	delete(body, "idempotencyKey")
	raw, _ = json.Marshal(body)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cfg model.GovernanceConfiguration
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, invalid("配置结构不合法：%v", err)
	}
	return cfg, nil
}

func resultOptions() []model.GovernanceOption {
	return []model.GovernanceOption{{Code: "TARGET_ABNORMALITY_OBSERVED", Label: "发现目标异常"}, {Code: "NO_TARGET_ABNORMALITY_OBSERVED", Label: "未发现目标异常"}, {Code: "DISPUTED", Label: "有争议"}, {Code: "FIRE_OBSERVED", Label: "发现火情"}, {Code: "OTHER_ABNORMALITY_OBSERVED", Label: "其他异常"}, {Code: "NO_FIRE_OBSERVED", Label: "未发现火情"}, {Code: "UNABLE_TO_DETERMINE", Label: "无法确定"}}
}
func Builtins() []model.GovernanceDocument {
	fields := []model.GovernanceField{{ID: "fieldResult", Label: "现场结果", Control: "single", Required: true, Protected: true, Options: resultOptions()}, {ID: "activityRelation", Label: "当时活动", Control: "single", Required: true, Protected: true, Options: []model.GovernanceOption{{Code: "CONFIRMED_RELATED", Label: "已核实相关"}, {Code: "CONFIRMED_UNRELATED", Label: "已核实无关"}, {Code: "RELATED", Label: "存在相关线索"}, {Code: "SUSPECTED", Label: "疑似有关"}, {Code: "UNRELATED", Label: "已核实无关"}, {Code: "UNKNOWN", Label: "未知"}, {Code: "DISPUTED", Label: "有争议"}}}, {ID: "facilityStatus", Label: "关联设施状态", Control: "single", Required: true, Protected: true, Options: []model.GovernanceOption{{Code: "NORMAL", Label: "已核实正常"}, {Code: "ABNORMAL", Label: "发现异常"}, {Code: "UNKNOWN", Label: "未知"}, {Code: "DISPUTED", Label: "有争议"}}}}
	out := []model.GovernanceDocument{}
	add := func(kind, id string, b model.GovernanceConfiguration) {
		b.Status = "PUBLISHED"
		b.SchemaVersion = "1"
		b.RevisionNumber = 1
		b.Builtin = true
		b.Hash = ""
		b.Hash = model.GovernanceHash(b)
		raw, _ := json.Marshal(b)
		out = append(out, model.GovernanceDocument{ID: id, Kind: kind, ResourceID: b.ResourceID, RevisionNumber: 1, Status: b.Status, CreatedBy: "system", DeviceIDs: []string{}, Body: raw})
	}
	add(model.GovernanceTemplateKind, "template-general-v1", model.GovernanceConfiguration{ResourceID: "template-general", Name: "通用核查治理模板", Fields: fields})
	for _, scene := range []struct{ ID, Name, Activity string }{{"kitchen", "餐饮厨房", "COOKING"}, {"humidity", "高湿与洗衣", "STEAM"}, {"construction", "施工装修", "CONSTRUCTION"}, {"garage", "车库车辆", "VEHICLE"}, {"electrical", "机房配电", "ELECTRICAL_WORK"}, {"water", "泵房水系统", "WATER_MAINTENANCE"}, {"general", "通用场所", "OTHER"}} {
		activityOptions := []model.GovernanceOption{{Code: "OTHER", Label: "其他"}, {Code: "UNKNOWN", Label: "未知"}, {Code: "DISPUTED", Label: "有争议"}}
		if scene.Activity != "OTHER" {
			activityOptions = append([]model.GovernanceOption{{Code: scene.Activity, Label: scene.Name + "实际活动"}}, activityOptions...)
		}
		add(model.GovernanceSceneKind, "scene-"+scene.ID+"-v1", model.GovernanceConfiguration{ResourceID: "scene-" + scene.ID, Name: scene.Name, ActivityOptions: activityOptions, EnvironmentOptions: []model.GovernanceOption{{Code: "STEAM", Label: "蒸汽"}, {Code: "DUST", Label: "粉尘"}, {Code: "UNKNOWN", Label: "未知"}, {Code: "DISPUTED", Label: "有争议"}}, FacilityQuestions: []model.GovernanceField{{ID: "facilityStatus", Label: "设施实际状态", Control: "single", Protected: true, Options: fields[2].Options}}})
	}
	add(model.GovernanceProfileKind, "profile-report-only-v1", model.GovernanceConfiguration{ResourceID: "profile-report-only", Name: "仅上报，恢复语义未知", CycleMethod: "REPORT_ONLY", TimeBasis: "RECEIVED_AT", RecoverySemantics: "UNKNOWN", ReferenceBasis: "未提供可靠断言与恢复语义；仅统计合法源身份上报，不计算物理周期。"})
	return out
}
func validateConfiguration(kind string, b model.GovernanceConfiguration) error {
	if strings.TrimSpace(b.Name) == "" || len(b.Name) > 200 || len(b.Fields) > 80 || len(b.DeviceIDs) > 100 {
		return invalid("配置名称、字段数或设备范围不合法")
	}
	seen := map[string]bool{}
	for _, f := range append(append([]model.GovernanceField{}, b.Fields...), b.FacilityQuestions...) {
		if f.ID == "" || seen[f.ID] {
			return invalid("字段ID为空或重复：%s", f.ID)
		}
		seen[f.ID] = true
		if !slices.Contains([]string{"text", "single", "multi", "time", "number", "attachment"}, f.Control) {
			return invalid("字段控件不支持：%s", f.Control)
		}
		if f.SourceFieldPath != "" && !sourceFieldPattern.MatchString(f.SourceFieldPath) {
			return invalid("源字段只允许成功样本的单一properties或event字段")
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max || (f.Min != nil || f.Max != nil) && f.Control != "number" {
			return invalid("数值范围无效")
		}
		codes := map[string]bool{}
		for _, o := range f.Options {
			if o.Code == "" || codes[o.Code] {
				return invalid("选项代码为空或重复")
			}
			codes[o.Code] = true
		}
		if f.Control == "single" || f.Control == "multi" {
			if !codes["UNKNOWN"] && !codes["UNABLE_TO_DETERMINE"] {
				return invalid("选项必须保留未知")
			}
			if f.Protected && !codes["DISPUTED"] {
				return invalid("保护字段必须保留争议")
			}
		}
		if f.Protected && slices.Contains([]string{"fieldResult", "activityRelation", "facilityStatus"}, f.ID) {
			if f.Control != "single" || kind == model.GovernanceTemplateKind && !f.Required {
				return invalid("保护字段须保留单选及必填语义：%s", f.ID)
			}
			required := []string{}
			switch f.ID {
			case "fieldResult":
				required = []string{"OTHER_ABNORMALITY_OBSERVED", "UNABLE_TO_DETERMINE", "DISPUTED"}
				if len(b.ApplicableAlarmTypes) == 0 || slices.Contains(b.ApplicableAlarmTypes, "FIRE") {
					required = append(required, "FIRE_OBSERVED", "NO_FIRE_OBSERVED")
				}
				if len(b.ApplicableAlarmTypes) == 0 || slices.ContainsFunc(b.ApplicableAlarmTypes, func(v string) bool { return v != "FIRE" }) {
					required = append(required, "TARGET_ABNORMALITY_OBSERVED", "NO_TARGET_ABNORMALITY_OBSERVED")
				}
			case "activityRelation":
				required = []string{"CONFIRMED_RELATED", "CONFIRMED_UNRELATED", "RELATED", "SUSPECTED", "UNKNOWN", "DISPUTED"}
			case "facilityStatus":
				required = []string{"NORMAL", "ABNORMAL", "UNKNOWN", "DISPUTED"}
			}
			for _, code := range required {
				if !codes[code] {
					return invalid("保护字段缺少稳定选项：%s/%s", f.ID, code)
				}
			}
		}
	}
	for _, options := range [][]model.GovernanceOption{b.ActivityOptions, b.EnvironmentOptions} {
		codes := map[string]bool{}
		for _, option := range options {
			if option.Code == "" || strings.TrimSpace(option.Label) == "" || codes[option.Code] {
				return invalid("场景选项代码为空或重复")
			}
			codes[option.Code] = true
		}
		if len(options) > 0 && (!codes["UNKNOWN"] || !codes["DISPUTED"]) {
			return invalid("场景选项必须保留未知和争议")
		}
	}
	if kind == model.GovernanceTemplateKind {
		for _, id := range []string{"fieldResult", "activityRelation", "facilityStatus"} {
			found := false
			for _, f := range b.Fields {
				if f.ID == id && f.Protected {
					found = true
				}
			}
			if !found {
				return invalid("缺少保护字段：%s", id)
			}
		}
	}
	if kind == model.GovernanceProfileKind {
		if !slices.Contains([]string{"COMPONENT_BOOLEAN", "DIRECT_EXPLICIT_STATE", "RECORDED_RULE_LIFECYCLE", "REPORT_ONLY"}, b.CycleMethod) {
			return invalid("cycleMethod不合法")
		}
		if strings.TrimSpace(b.ReferenceBasis) == "" {
			return invalid("类型配置须提供资料依据")
		}
		if b.CycleMethod != "REPORT_ONLY" && (b.AlarmType == "" || b.OriginKind == "" || b.SignalKey == "" || b.RecoverySemantics == "" || len(b.DeviceIDs) == 0) {
			return invalid("可靠周期配置须明确报警类型、来源、信号、恢复语义和设备范围")
		}
	}
	return nil
}
func (s *Service) configuration(tx ports.AlarmGovernanceTx, a Actor, c Command, d model.GovernanceDocument) (model.GovernanceDocument, error) {
	var b model.GovernanceConfiguration
	var err error
	if c.Operation == "create" || c.Operation == "revisions" || c.Operation == "update" {
		b, err = decodeConfiguration(c.Body)
	} else {
		b, err = model.GovernanceBody[model.GovernanceConfiguration](d)
	}
	if err != nil {
		return d, err
	}
	if c.Operation == "create" || c.Operation == "revisions" || c.Operation == "update" {
		clearSourceSignatures(&b)
	}
	if c.Operation == "create" || c.Operation == "revisions" {
		if b.Builtin {
			return d, invalid("内置配置只读，请复制为租户配置")
		}
		b.Builtin = false
		b.Status = "DRAFT"
		b.Hash = ""
		b.PublishedAt = 0
		b.PublishedBy = ""
		if b.ResourceID == "" {
			b.ResourceID = uuid.NewString()
		}
		if c.Operation == "revisions" {
			old, _ := model.GovernanceBody[model.GovernanceConfiguration](d)
			if old.Builtin {
				return d, invalid("内置配置不能直接追加版本，请复制")
			}
			b.ResourceID = old.ResourceID
		}
		items, _, e := tx.List(model.GovernanceFilter{Kind: c.Kind, ResourceID: b.ResourceID, AllDevices: true, Limit: 100})
		if e != nil {
			return d, e
		}
		b.RevisionNumber = 1
		for _, v := range items {
			if v.RevisionNumber >= b.RevisionNumber {
				b.RevisionNumber = v.RevisionNumber + 1
			}
		}
		d = model.GovernanceDocument{ID: uuid.NewString(), Kind: c.Kind, CreatedBy: a.Username}
		c.ExpectedVersion = 0
	} else {
		old, _ := model.GovernanceBody[model.GovernanceConfiguration](d)
		if old.Builtin {
			return d, invalid("内置配置只读")
		}
		if c.Operation == "update" {
			if old.Status != "DRAFT" {
				return d, invalid("已发布配置正文不可修改")
			}
			b.ResourceID = old.ResourceID
			b.RevisionNumber = old.RevisionNumber
			b.Status = "DRAFT"
			b.Builtin = false
			b.Hash = ""
		} else if c.Operation == "publish" {
			if old.Status != "DRAFT" {
				return d, invalid("只有草稿可发布")
			}
			b.Status = "PUBLISHED"
			b.PublishedBy = a.Username
			b.PublishedAt = s.Now().UnixMilli()
			b.Hash = ""
			b.Hash = model.GovernanceHash(b)
		} else if c.Operation == "retire" {
			if old.Status != "PUBLISHED" {
				return d, invalid("只有已发布配置可退休")
			}
			b.Status = "RETIRED"
		} else if c.Operation == "validate" {
			if e := validateConfiguration(c.Kind, b); e != nil {
				return d, e
			}
			if e := s.validateSourceBindings(tx, a, &b, false); e != nil {
				return d, e
			}
			return d, nil
		} else {
			return d, invalid("配置动作不支持")
		}
	}
	if !a.covers(b.DeviceIDs) {
		return d, ErrForbidden
	}
	if b.ProductID != "" && !a.AllDevices {
		return d, invalid("产品共享配置要求具备完整影响范围的管理权限")
	}
	if len(b.DeviceIDs) == 0 && !a.AllDevices {
		return d, invalid("共享配置发布与编辑要求覆盖完整影响范围")
	}
	if err = validateConfiguration(c.Kind, b); err != nil {
		return d, err
	}
	if c.Operation != "retire" {
		if err = s.validateSourceBindings(tx, a, &b, c.Operation == "publish"); err != nil {
			return d, err
		}
	}
	if c.Operation == "publish" {
		b.Hash = ""
		b.Hash = model.GovernanceHash(b)
	}
	d.ResourceID = b.ResourceID
	d.RevisionNumber = b.RevisionNumber
	d.DeviceIDs = unique(b.DeviceIDs)
	d.Status = b.Status
	return put(tx, d, b, c.ExpectedVersion)
}
func (s *Service) refs(tx ports.AlarmGovernanceTx, p model.GovernancePoint, r model.GovernanceConfigurationRefs) (model.GovernanceConfigurationRefs, error) {
	if r.TemplateRevisionID == "" {
		r.TemplateRevisionID = "template-general-v1"
	}
	if r.ScenePresetRevisionID == "" {
		r.ScenePresetRevisionID = "scene-general-v1"
	}
	if r.TypeProfileRevisionID == "" {
		r.TypeProfileRevisionID = "profile-report-only-v1"
	}
	for _, v := range []struct {
		kind, id string
		hash     *string
	}{{model.GovernanceTemplateKind, r.TemplateRevisionID, &r.TemplateHash}, {model.GovernanceSceneKind, r.ScenePresetRevisionID, &r.ScenePresetHash}, {model.GovernanceProfileKind, r.TypeProfileRevisionID, &r.TypeProfileHash}} {
		_, b, e := read[model.GovernanceConfiguration](tx, v.kind, v.id)
		if e != nil {
			return r, e
		}
		if b.Status != "PUBLISHED" {
			return r, invalid("新记录只能使用已发布且未退休配置")
		}
		if len(b.DeviceIDs) > 0 && !slices.Contains(b.DeviceIDs, p.DeviceID) {
			return r, invalid("类型配置不适用当前设备")
		}
		if len(b.ApplicableAlarmTypes) > 0 && !slices.Contains(b.ApplicableAlarmTypes, p.AlarmType) {
			return r, invalid("预设不适用报警类型")
		}
		if v.kind == model.GovernanceProfileKind && (b.AlarmType != "" && b.AlarmType != p.AlarmType || b.OriginKind != "" && b.OriginKind != p.OriginKind || b.SignalKey != "" && b.SignalKey != p.SignalKey) {
			return r, invalid("类型配置与原始信号不匹配")
		}
		*v.hash = b.Hash
	}
	return r, nil
}
