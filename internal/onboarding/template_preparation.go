package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func DefaultVerificationRules() model.VerificationRules {
	return model.VerificationRules{Mode: "periodic", MinMessages: 2, WindowSeconds: 900, MaxGapSeconds: 900}
}

func NormalizeVerificationRules(v model.VerificationRules) (model.VerificationRules, error) {
	if v.Mode == "" {
		v = DefaultVerificationRules()
	}
	switch v.Mode {
	case "periodic", "low_frequency", "event", "child":
	default:
		return v, invalid("请选择周期、低频、事件或子设备验收方式")
	}
	if v.MinMessages == 0 {
		if v.Mode == "periodic" {
			v.MinMessages = 2
		} else {
			v.MinMessages = 1
		}
	}
	if v.WindowSeconds == 0 {
		if v.Mode == "low_frequency" {
			v.WindowSeconds = 86400
		} else {
			v.WindowSeconds = 900
		}
	}
	if v.MaxGapSeconds == 0 && v.Mode == "periodic" {
		v.MaxGapSeconds = v.WindowSeconds
	}
	if v.MinMessages < 1 || v.MinMessages > 100 || v.WindowSeconds < 1 || v.WindowSeconds > 30*86400 || v.MaxGapSeconds < 0 || v.MaxGapSeconds > v.WindowSeconds {
		return v, invalid("验收数量须为 1–100，时间窗口须在 1 秒至 30 天之间，最大间隔不能超过窗口")
	}
	validTypes := map[string]bool{"PROPERTY_REPORT": true, "EVENT_REPORT": true, "STATE_CHANGE": true, "ALARM_REPORT": true, "COMMAND_REPLY": true, "LOG_REPORT": true}
	for _, kind := range v.RequiredMessageTypes {
		if !validTypes[kind] {
			return v, invalid("验收消息类型无效")
		}
	}
	if len(v.RequiredProperties) > 100 || len(v.RequiredEvents) > 30 || len(v.RequiredMessageTypes) > 6 {
		return v, invalid("验收条件数量超出限制")
	}
	for _, key := range append(append([]string{}, v.RequiredProperties...), v.RequiredEvents...) {
		if strings.TrimSpace(key) == "" || len(key) > 128 {
			return v, invalid("验收字段和事件标识不能为空或超过 128 字节")
		}
	}
	if v.Mode == "event" && len(v.RequiredEvents) == 0 && len(v.RequiredMessageTypes) == 0 {
		return v, invalid("事件型验收请指定事件标识或消息类型")
	}
	return v, nil
}

func ProductVerificationRules(p model.Product) model.VerificationRules {
	if p.VerificationRules != nil {
		if v, err := NormalizeVerificationRules(*p.VerificationRules); err == nil {
			return v
		}
	}
	return DefaultVerificationRules()
}

func TemplateRecordID(productID string) string { return "template:" + productID }
func (s *Service) TemplateRecord(ctx context.Context, tenant, productID string) (model.OnboardingRecord, model.TemplatePreparation, error) {
	rec, err := s.Repo.GetOnboardingRecord(ctx, tenant, TemplateRecordID(productID))
	if errors.Is(err, model.ErrNotFound) {
		return model.OnboardingRecord{TenantID: tenant, ID: TemplateRecordID(productID), OwnerID: "template", Kind: "template-preparation"}, model.TemplatePreparation{Status: "DRAFT", History: []model.TemplateRevision{}}, nil
	}
	var v model.TemplatePreparation
	if err == nil {
		err = json.Unmarshal(rec.Body, &v)
	}
	return rec, v, err
}

func (s *Service) CurrentCandidate(ctx context.Context, tenant, productID string) (model.TemplateCandidate, error) {
	p, err := s.Repo.GetProduct(ctx, tenant, productID)
	if err != nil {
		return model.TemplateCandidate{}, err
	}
	plan, err := s.Plan(ctx, tenant, p)
	if err != nil {
		return model.TemplateCandidate{}, err
	}
	all, err := s.Repo.ListDeviceAccessProfiles(ctx, tenant)
	if err != nil {
		return model.TemplateCandidate{}, err
	}
	v := model.TemplateCandidate{Product: p, ProtocolID: plan.Protocol.ID, Version: plan.Protocol.Version, Profiles: []model.DeviceAccessProfile{}, VerificationRules: ProductVerificationRules(p)}
	for _, profile := range all {
		if profile.ProductID == productID && profile.DeviceID == "" {
			v.Profiles = append(v.Profiles, profile)
		}
	}
	return v, nil
}

// CandidateFingerprint includes only operational configuration. Cosmetic edits
// and runtime status updates cannot revoke a successful acceptance.
func CandidateFingerprint(v model.TemplateCandidate) string {
	p := v.Product
	metadata := map[string]any{}
	for _, key := range []string{"identity", "firmware", "deviceParameters", "connectionDefaults", "autoRegisterPolicy"} {
		if value, ok := p.Metadata[key]; ok {
			metadata[key] = value
		}
	}
	profiles := make([]model.DeviceAccessProfile, 0, len(v.Profiles))
	for _, profile := range v.Profiles {
		profile.RuntimeStatus = ""
		profile.LastError = ""
		profile.LastSuccessAt = 0
		profile.LastErrorAt = 0
		profile.CreatedAt = 0
		profile.UpdatedAt = 0
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	data, _ := json.Marshal(struct {
		ProtocolID, Version, Transport, PayloadFormat string
		Model                                         *model.ThingModel
		Metadata                                      map[string]any
		Profiles                                      []model.DeviceAccessProfile
		Rules                                         model.VerificationRules
	}{v.ProtocolID, v.Version, p.Transport, p.PayloadFormat, p.ThingModel, metadata, profiles, v.VerificationRules})
	return Hash(string(data))
}

// CandidateFingerprint also pins the device-facing URL selected by the server.
// Changing the configured public ingress cannot inherit another endpoint's proof.
func (s *Service) CandidateFingerprint(v model.TemplateCandidate) string {
	fingerprint := CandidateFingerprint(v)
	transport := strings.ToUpper(v.Product.Transport)
	if strings.Contains(transport, "TCP") || transport == "UDP" || strings.HasPrefix(transport, "MODBUS") {
		return fingerprint
	}
	address := s.PublicHTTP
	if v.ProtocolID == parser.StandardProtocolID && transport != "HTTP" {
		address = s.PublicMQTT
	}
	if address == "" {
		return fingerprint
	}
	return Hash(fingerprint + "\x00" + address)
}

func (s *Service) TemplateFingerprint(ctx context.Context, tenant, productID string) (string, error) {
	v, err := s.CurrentCandidate(ctx, tenant, productID)
	if err != nil {
		return "", err
	}
	return s.CandidateFingerprint(v), nil
}

func (s *Service) TemplateReadiness(ctx context.Context, tenant, productID string) (string, bool, error) {
	p, err := s.Repo.GetProduct(ctx, tenant, productID)
	if err != nil {
		return "", false, err
	}
	rec, v, err := s.TemplateRecord(ctx, tenant, productID)
	if err != nil {
		return "", false, err
	}
	if rec.Revision == 0 {
		return "UNVERIFIED", false, nil
	}
	fp, err := s.TemplateFingerprint(ctx, tenant, productID)
	if err != nil {
		return "", false, err
	}
	if fp != v.Fingerprint {
		return "CONFIGURATION_CHANGED", false, nil
	}
	ready := v.Status == "READY" && v.Verification != nil && v.Verification.Status == "VERIFIED" && v.Verification.Fingerprint == fp
	return v.Status, ready && p.Status == "ENABLED", nil
}

func (s *Service) SaveTemplateRecord(ctx context.Context, rec model.OnboardingRecord, v model.TemplatePreparation) (model.OnboardingRecord, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return rec, err
	}
	rec.Body = body
	rec.Status = v.Status
	return s.Repo.SaveOnboardingRecord(ctx, rec, rec.Revision)
}

func fieldEvidenceSource(source string) bool {
	switch source {
	case "standard-http", "standard-mqtt", "device-http", "modbus-tcp-collector":
		return true
	}
	return strings.HasPrefix(source, "go-protocol-")
}

// VerifyDevice derives evidence exclusively from archived field ingress and its
// persisted parser result. A UI never submits an acceptance result.
func (s *Service) VerifyDevice(ctx context.Context, tenant, deviceID string) (model.DeviceVerification, error) {
	d, err := s.Repo.GetManagedDevice(ctx, tenant, deviceID)
	if err != nil {
		return model.DeviceVerification{}, err
	}
	candidate, err := s.CurrentCandidate(ctx, tenant, d.ProductID)
	if err != nil {
		return model.DeviceVerification{}, err
	}
	p, rules := candidate.Product, candidate.VerificationRules
	now := time.Now().UnixMilli()
	v := model.DeviceVerification{Status: "WAITING", DeviceID: d.ID, ProductID: p.ID, Fingerprint: s.CandidateFingerprint(candidate), ProtocolID: candidate.ProtocolID, Version: candidate.Version, Checks: []model.VerificationCheck{}, RawMessageIDs: []string{}, CheckedAt: now, Since: max(d.CreatedAt, p.CreatedAt, now-int64(rules.WindowSeconds)*1000)}
	_, prep, err := s.TemplateRecord(ctx, tenant, p.ID)
	if err != nil {
		return v, err
	}
	v.Since = max(v.Since, prep.AppliedAt)
	v.Since = max(v.Since, d.UpdatedAt)
	if binding, e := s.Repo.GetProductProtocolBinding(ctx, tenant, p.ID); e == nil {
		v.Since = max(v.Since, binding.UpdatedAt)
	} else if !errors.Is(e, model.ErrNotFound) {
		return v, e
	}
	var profile *model.DeviceAccessProfile
	if d.ConnectorProfileID != "" {
		value, e := s.Repo.GetDeviceAccessProfile(ctx, tenant, d.ConnectorProfileID)
		if e != nil {
			return v, e
		}
		profile = &value
		v.ProfileID = value.ID
		v.ProfileFingerprint = value.ConfigurationFingerprint()
		v.Since = max(v.Since, value.UpdatedAt)
	}
	add := func(key, label string, ok bool, detail string) {
		state := "waiting"
		if ok {
			state = "passed"
		}
		v.Checks = append(v.Checks, model.VerificationCheck{Key: key, Label: label, State: state, Detail: detail})
	}
	add("configuration", "配置已保存", p.Status == "ENABLED" && d.Status == "ENABLED", "模板与设备须已启用")
	indexes, err := s.Repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: tenant, DeviceID: d.ID, Start: v.Since, Limit: 1000})
	if err != nil {
		return v, err
	}
	if s.LoadRaw == nil {
		return v, errors.New("原文读取服务未配置，不能生成现场验收")
	}
	types, props, events := map[string]bool{}, map[string]bool{}, map[string]bool{}
	times := []int64{}
	received := false
	latestParseFailed := false
	latestAt := int64(0)
	for _, idx := range indexes {
		raw, e := s.LoadRaw(ctx, idx)
		if e != nil {
			continue
		}
		if raw.TenantID != tenant || raw.ProductID != p.ID || raw.DeviceID != d.ID || !fieldEvidenceSource(raw.Source) || raw.ProtocolID != candidate.ProtocolID || raw.ProtocolVersion != candidate.Version {
			continue
		}
		if profile != nil && (raw.Metadata["profileId"] != profile.ID || raw.Metadata["profileFingerprint"] != v.ProfileFingerprint) {
			continue
		}
		if d.GatewayID != "" && (raw.GatewayID != d.GatewayID || raw.Metadata["childAddress"] != d.ChildAddress || raw.Metadata["childType"] != d.ChildType) {
			continue
		}
		received = true
		msg, e := s.Repo.GetStandardMessageByRaw(ctx, tenant, idx.MessageID)
		if idx.ReceivedAt > latestAt {
			latestAt = idx.ReceivedAt
			latestParseFailed = e != nil
		}
		if e != nil {
			continue
		}
		if msg.TenantID != tenant || msg.ProductID != p.ID || msg.DeviceID != d.ID || msg.RawMessageID != idx.MessageID {
			continue
		}
		v.RawMessageIDs = append(v.RawMessageIDs, idx.MessageID)
		times = append(times, idx.ReceivedAt)
		types[string(msg.MessageType)] = true
		for key := range msg.Properties {
			props[key] = true
		}
		for _, key := range []string{"id", "event", "eventId", "identifier", "type", "code"} {
			if value, ok := msg.Event[key].(string); ok {
				events[value] = true
			}
		}
	}
	v.MessageCount = len(times)
	add("raw", "现场原文", received, "仅使用当前协议及连接的现场原文，排除模拟和回放")
	add("parsed", "解析成功", len(times) > 0 && !latestParseFailed, fmt.Sprintf("有效解析 %d 条；最新报文须解析成功", len(times)))
	add("count", "上报数量", len(times) >= rules.MinMessages, fmt.Sprintf("最近 %d 秒至少 %d 条", rules.WindowSeconds, rules.MinMessages))
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	if rules.MaxGapSeconds > 0 {
		stable := len(times) >= rules.MinMessages
		if stable {
			stable = now-times[len(times)-1] <= int64(rules.MaxGapSeconds)*1000
			for i := len(times) - rules.MinMessages + 1; i < len(times); i++ {
				stable = stable && times[i]-times[i-1] <= int64(rules.MaxGapSeconds)*1000
			}
		}
		add("interval", "上报间隔", stable, fmt.Sprintf("最新连续 %d 条间隔和最近上报距今均不超过 %d 秒", rules.MinMessages, rules.MaxGapSeconds))
	}
	for _, kind := range rules.RequiredMessageTypes {
		add("type:"+kind, "消息类型", types[kind], kind)
	}
	for _, key := range rules.RequiredProperties {
		add("property:"+key, "必需字段", props[key], key)
	}
	for _, key := range rules.RequiredEvents {
		add("event:"+key, "指定事件", events[key], key)
	}
	if rules.Mode == "child" || d.GatewayID != "" || d.DeviceRole == "CHILD" {
		parent, pe := s.Repo.GetManagedDevice(ctx, tenant, d.GatewayID)
		valid := pe == nil && parent.Status == "ENABLED" && d.GatewayID != ""
		if valid {
			parentEvidence, e := s.verifyParentEvidence(ctx, tenant, parent.ID, v.Since)
			valid = e == nil && parentEvidence
		}
		add("parent", "主设备与子设备映射", valid, "主设备须在授权范围内且本窗口有成功解析的现场数据；子设备须有独立地址映射及原文")
	}
	all := len(v.Checks) > 0
	for _, check := range v.Checks {
		all = all && check.State == "passed"
	}
	if all {
		v.Status = "VERIFIED"
		v.VerifiedAt = now
	}
	return v, nil
}

func (s *Service) verifyParentEvidence(ctx context.Context, tenant, id string, since int64) (bool, error) {
	d, err := s.Repo.GetManagedDevice(ctx, tenant, id)
	if err != nil {
		return false, err
	}
	if d.Status != "ENABLED" || d.DeviceRole != "GATEWAY" || d.GatewayID != "" {
		return false, nil
	}
	candidate, err := s.CurrentCandidate(ctx, tenant, d.ProductID)
	if err != nil {
		return false, err
	}
	if candidate.Product.Status != "ENABLED" {
		return false, nil
	}
	since = max(since, d.UpdatedAt)
	if binding, e := s.Repo.GetProductProtocolBinding(ctx, tenant, d.ProductID); e == nil {
		since = max(since, binding.UpdatedAt)
	}
	profileFingerprint := ""
	if d.ConnectorProfileID != "" {
		p, e := s.Repo.GetDeviceAccessProfile(ctx, tenant, d.ConnectorProfileID)
		if e != nil {
			return false, e
		}
		since = max(since, p.UpdatedAt)
		profileFingerprint = p.ConfigurationFingerprint()
	}
	indexes, err := s.Repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: tenant, DeviceID: id, Start: since, Limit: 100})
	if err != nil {
		return false, err
	}
	for _, idx := range indexes {
		raw, e := s.LoadRaw(ctx, idx)
		if e == nil && raw.TenantID == tenant && raw.DeviceID == id && raw.ProductID == d.ProductID && raw.ProtocolID == candidate.ProtocolID && raw.ProtocolVersion == candidate.Version && (d.ConnectorProfileID == "" || (raw.Metadata["profileId"] == d.ConnectorProfileID && raw.Metadata["profileFingerprint"] == profileFingerprint)) && fieldEvidenceSource(raw.Source) {
			if standard, parseErr := s.Repo.GetStandardMessageByRaw(ctx, tenant, idx.MessageID); parseErr == nil && standard.TenantID == tenant && standard.DeviceID == id && standard.ProductID == d.ProductID && standard.RawMessageID == idx.MessageID {
				return true, nil
			}
		}
	}
	return false, nil
}

// Incomplete candidate drafts may be resumed, but cannot contain credentials.
// Operational validation runs when applying or creating an isolated trial.
func ValidatePreparationDraft(v model.TemplateCandidate) error {
	data, err := json.Marshal(v)
	if err != nil || len(data) > 512<<10 {
		return invalid("模板草稿过大或格式无效")
	}
	var content any
	if err = json.Unmarshal(data, &content); err != nil {
		return err
	}
	if containsCredential(content) {
		return invalid("模板草稿不得保存明文密码、令牌或密钥，请使用已有凭据引用")
	}
	return nil
}
