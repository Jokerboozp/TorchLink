package alarmgovernance

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var sourceFieldPattern = regexp.MustCompile(`^(properties|event)\.([A-Za-z_][A-Za-z0-9_-]{0,127})$`)

// SourceFieldDescriptor describes a scalar from an actual processed sample.
// It carries no raw message and never supplies a field or cause conclusion.
type SourceFieldDescriptor struct {
	DeviceID        string   `json:"deviceId"`
	MessageID       string   `json:"messageId"`
	SourceFieldPath string   `json:"sourceFieldPath"`
	SourceType      string   `json:"sourceType"`
	EnumValues      []string `json:"enumValues,omitempty"`
	ObservedAt      int64    `json:"observedAt"`
}

func sourceFieldValue(msg model.StandardMessage, path string) (any, string, error) {
	parts := sourceFieldPattern.FindStringSubmatch(path)
	if parts == nil {
		return nil, "", invalid("源字段只允许properties或event下的单一实际字段")
	}
	values := msg.Properties
	if parts[1] == "event" {
		values = msg.Event
	}
	v, ok := values[parts[2]]
	if !ok {
		return nil, "", invalid("成功解析样本中不存在所选字段")
	}
	switch x := v.(type) {
	case string:
		if len(x) > 4000 {
			return nil, "", invalid("源文字字段超过取证长度限制")
		}
		return x, "STRING", nil
	case bool:
		return x, "BOOLEAN", nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			break
		}
		return x, "NUMBER", nil
	case json.Number:
		n, err := x.Float64()
		if err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
			return n, "NUMBER", nil
		}
	}
	return nil, "", invalid("源字段须为已成功解析的有限标量")
}

func clearSourceSignatures(cfg *model.GovernanceConfiguration) {
	for _, fields := range [][]model.GovernanceField{cfg.Fields, cfg.FacilityQuestions} {
		for i := range fields {
			fields[i].SourceConfirmedBy = ""
			fields[i].SourceConfirmedAt = 0
			fields[i].SourceValueHash = ""
		}
	}
}

func (s *Service) validateSourceBindings(tx ports.AlarmGovernanceTx, a Actor, cfg *model.GovernanceConfiguration, confirm bool) error {
	for _, fields := range [][]model.GovernanceField{cfg.Fields, cfg.FacilityQuestions} {
		for i := range fields {
			f := &fields[i]
			if f.SourceFieldPath == "" {
				if f.SourceDeviceID != "" || f.SourceMessageID != "" || f.SourceType != "" {
					return invalid("源绑定须同时提供路径、设备、成功样本与类型：%s", f.ID)
				}
				continue
			}
			if !sourceMenus(a, model.GovernanceVerificationKind) {
				return ErrForbidden
			}
			if len(cfg.DeviceIDs) == 0 {
				return invalid("绑定成功样本的配置须明确全部影响设备，避免共享配置暴露未授权来源")
			}
			if f.Protected || slices.Contains([]string{"fieldResult", "activityRelation", "facilityStatus"}, f.ID) {
				return invalid("现场结论保护字段不能以源值预填：%s", f.ID)
			}
			if f.SourceDeviceID == "" || f.SourceMessageID == "" || !a.covers([]string{f.SourceDeviceID}) {
				return ErrForbidden
			}
			if len(cfg.DeviceIDs) > 0 && !slices.Contains(cfg.DeviceIDs, f.SourceDeviceID) {
				return invalid("源样本设备不在配置完整影响范围：%s", f.ID)
			}
			reader, ok := tx.(ports.GovernanceHistoricalReader)
			if !ok {
				return invalid("成功解析样本读取不可用")
			}
			msg, err := reader.GetGovernanceHistoricalMessage(f.SourceMessageID)
			if err != nil {
				return err
			}
			if msg.TenantID != a.TenantID || msg.DeviceID != f.SourceDeviceID {
				return ErrForbidden
			}
			if cfg.ProductID != "" && msg.ProductID != cfg.ProductID {
				return invalid("源样本不属于配置指定产品")
			}
			value, dataType, err := sourceFieldValue(msg, f.SourceFieldPath)
			if err != nil {
				return err
			}
			if f.SourceType != dataType {
				return invalid("源字段实际类型与声明不符：%s", f.ID)
			}
			compatible := f.Control == "text" && dataType == "STRING" || f.Control == "number" && dataType == "NUMBER"
			if f.Control == "single" && (dataType == "STRING" || dataType == "BOOLEAN") {
				code := ""
				if dataType == "STRING" {
					code = value.(string)
				} else if value.(bool) {
					code = "true"
				} else {
					code = "false"
				}
				compatible = len(f.Options) <= 32 && slices.ContainsFunc(f.Options, func(o model.GovernanceOption) bool { return o.Code == code })
			}
			if !compatible {
				return invalid("源字段只可绑定同类型文字、数值或明确有限单选：%s", f.ID)
			}
			if confirm {
				f.SourceConfirmedBy = a.Username
				f.SourceConfirmedAt = s.Now().UnixMilli()
				f.SourceValueHash = model.GovernanceHash(value)
			}
		}
	}
	return nil
}

func (s *Service) SourceFields(ctx context.Context, a Actor, f ports.AlarmObservationFilter) (out []SourceFieldDescriptor, err error) {
	out = []SourceFieldDescriptor{}
	a, err = s.actor(ctx, a, "templates")
	if err != nil {
		return nil, err
	}
	if !sourceMenus(a, model.GovernanceVerificationKind) || f.DeviceID == "" || !a.covers([]string{f.DeviceID}) || f.Start <= 0 || f.End <= f.Start || f.End-f.Start > 366*dayMillis {
		return nil, ErrForbidden
	}
	f.DeviceIDs = []string{f.DeviceID}
	f.Limit = 100
	f.Cursor = ""
	err = s.Store.GovernanceRead(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			fresh, e := s.ResolveTx(tx, a)
			if e != nil || !fresh.can("templates") || !sourceMenus(fresh, model.GovernanceVerificationKind) || !fresh.covers(f.DeviceIDs) {
				return ErrForbidden
			}
			a = fresh
		}
		reader, ok := tx.(ports.GovernanceHistoricalReader)
		if !ok {
			return invalid("成功解析字段目录不可用")
		}
		messages, e := reader.ListGovernanceHistoricalMessages(f)
		if e != nil {
			return e
		}
		latest := map[string]SourceFieldDescriptor{}
		for _, msg := range messages {
			if msg.TenantID != a.TenantID || msg.DeviceID != f.DeviceID {
				return ErrForbidden
			}
			for prefix, values := range map[string]map[string]any{"properties": msg.Properties, "event": msg.Event} {
				for key := range values {
					path := prefix + "." + key
					value, kind, e := sourceFieldValue(msg, path)
					if e != nil {
						continue
					}
					v := SourceFieldDescriptor{DeviceID: msg.DeviceID, MessageID: msg.MessageID, SourceFieldPath: path, SourceType: kind, ObservedAt: msg.Timestamp}
					if kind == "BOOLEAN" {
						v.EnumValues = []string{"true", "false"}
					} else if kind == "STRING" && len(value.(string)) > 0 && len(value.(string)) <= 128 && strings.TrimSpace(value.(string)) != "" {
						v.EnumValues = []string{value.(string)}
					}
					latest[path] = v
				}
			}
		}
		for _, value := range latest {
			out = append(out, value)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].SourceFieldPath < out[j].SourceFieldPath })
		if len(out) > 160 {
			out = out[:160]
		}
		return nil
	})
	if err == nil {
		fresh, e := s.actor(ctx, a, "templates")
		if e != nil || !sourceMenus(fresh, model.GovernanceVerificationKind) || !fresh.covers(f.DeviceIDs) {
			return nil, ErrForbidden
		}
	}
	return out, err
}
