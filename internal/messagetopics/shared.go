package messagetopics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"iot-platform/internal/model"
)

const MaxRulePayload = 256 << 10

// SharedDestination is stable across accounts and credential rotations. Topic
// can be a tenant-owned complete address or a readable suffix entered in the UI.
func SharedDestination(tenant string, route model.MessageTopicRoute) string {
	prefix := KafkaPrefix(tenant)
	if route.Protocol == "mqtt" {
		prefix = MQTTPrefix(tenant)
	}
	if strings.HasPrefix(route.Topic, prefix) || (route.Protocol == "mqtt" && strings.HasPrefix(route.Topic, "/iot/external/")) || (route.Protocol == "kafka" && strings.HasPrefix(route.Topic, "iot.external.")) {
		return route.Topic
	}
	if route.Protocol == "mqtt" {
		return prefix + strings.TrimPrefix(route.Topic, "/")
	}
	return prefix + route.Topic
}

func sharedRoute(cfg model.MessageTopicConfig, id string) (model.MessageTopicRoute, bool) {
	for _, route := range cfg.Topics {
		if route.ID == id && route.Protocol != "" && route.SourceID == "" {
			return route, true
		}
	}
	return model.MessageTopicRoute{}, false
}

func permission(identity MessageTopicIdentity, menu string) bool {
	return identity.Permissions["*"] || identity.Permissions["menu:"+menu]
}

// RouteAllowed checks the complete historical exposure of a shared topic. Old
// per-credential routes retain their per-message filtering in ManagedDestinations.
func RouteAllowed(cfg model.MessageTopicConfig, id string, identity MessageTopicIdentity, account model.MessageTopicAccount) bool {
	if !account.Enabled || !routeEnabled(cfg, id) {
		return false
	}
	route, shared := sharedRoute(cfg, id)
	if !shared {
		source, ok := RouteSource(cfg, id)
		return ok && SourceAllowed(source.ID, identity) && (!strings.HasSuffix(source.ID, ".video-alarm") || account.DeviceScope == "all")
	}
	if !permission(identity, "messageTopics") {
		return false
	}
	for _, exposure := range route.Exposure {
		if !SourceAllowed(exposure.SourceID, identity) || !scopeCovers(identity.DeviceScope, identity.DeviceIDs, exposure.DeviceScope, exposure.DeviceIDs) || !scopeCovers(account.DeviceScope, account.DeviceIDs, exposure.DeviceScope, exposure.DeviceIDs) {
			return false
		}
		// Full source payloads can contain these fields, and historical messages
		// remain readable after a rule changes its field projection.
		if strings.HasSuffix(exposure.SourceID, ".alarm-ai-analysis") && !permission(identity, "knowledge") {
			return false
		}
		if strings.Contains(exposure.SourceID, ".alarm-") || strings.HasSuffix(exposure.SourceID, ".video-alarm") || strings.HasSuffix(exposure.SourceID, ".ui-action") {
			if !permission(identity, "cameras") {
				return false
			}
		}
	}
	return true
}

// Publish grants convey no permission to read retained or historical messages.
func RoutePublishAllowed(cfg model.MessageTopicConfig, id string, identity MessageTopicIdentity, account model.MessageTopicAccount) bool {
	route, ok := sharedRoute(cfg, id)
	return ok && route.Query == nil && route.Enabled && account.Enabled && permission(identity, "messageTopics")
}

func scopeCovers(scope string, ids []string, requiredScope string, requiredIDs []string) bool {
	if scope == "all" {
		return true
	}
	if scope != "selected" || requiredScope != "selected" {
		return false
	}
	for _, id := range requiredIDs {
		if !slices.Contains(ids, id) {
			return false
		}
	}
	return true
}

func validRuleScope(scope string, ids []string) bool {
	return (scope == "all" && len(ids) == 0) || (scope == "selected" && len(ids) > 0 && uniqueValues(ids, 10000, 256))
}

// AccumulateExposure only widens a topic's data boundary. Call before saving a
// rule, including a disabled draft, and preserve Exposure when editing topics.
func AccumulateExposure(cfg *model.MessageTopicConfig, rule model.MessageTopicRule) error {
	if cfg == nil || !validRuleScope(rule.DeviceScope, rule.DeviceIDs) {
		return fmt.Errorf("%w：规则设备范围无效", ErrInvalidConfig)
	}
	source, exists := topicByID(rule.SourceID)
	if !exists || !source.Editable {
		return fmt.Errorf("%w：规则数据源无效", ErrInvalidConfig)
	}
	for i := range cfg.Topics {
		route := &cfg.Topics[i]
		if route.ID != rule.TopicID {
			continue
		}
		if route.Protocol == "" || route.SourceID != "" || source.Protocol != route.Protocol {
			return fmt.Errorf("%w：规则数据源与主题协议不匹配", ErrInvalidConfig)
		}
		for j := range route.Exposure {
			exposure := &route.Exposure[j]
			if exposure.SourceID != rule.SourceID {
				continue
			}
			if exposure.DeviceScope == "all" {
				return nil
			}
			if rule.DeviceScope == "all" {
				exposure.DeviceScope, exposure.DeviceIDs = "all", nil
				return nil
			}
			for _, id := range rule.DeviceIDs {
				if !slices.Contains(exposure.DeviceIDs, id) {
					exposure.DeviceIDs = append(exposure.DeviceIDs, id)
				}
			}
			slices.Sort(exposure.DeviceIDs)
			return nil
		}
		route.Exposure = append(route.Exposure, model.MessageTopicExposure{SourceID: rule.SourceID, DeviceScope: rule.DeviceScope, DeviceIDs: slices.Clone(rule.DeviceIDs)})
		return nil
	}
	return fmt.Errorf("%w：规则主题不存在", ErrInvalidConfig)
}

// ReserveOverrideTargets carries forward all ordinary publication addresses,
// including disabled overrides and templates. A deleted or renamed override
// can still have retained broker history or a publication already in flight.
// Call with the stored revision before persisting a policy edit.
func ReserveOverrideTargets(cfg *model.MessageTopicConfig, previous model.MessageTopicConfig) {
	if cfg == nil {
		return
	}
	seen := make(map[model.MessageTopicReservedRoute]bool)
	history := make([]model.MessageTopicReservedRoute, 0, len(previous.RoutingHistory)+len(cfg.RoutingHistory))
	add := func(route model.MessageTopicReservedRoute) {
		if !seen[route] {
			seen[route] = true
			history = append(history, route)
		}
	}
	for _, routes := range [][]model.MessageTopicReservedRoute{previous.RoutingHistory, cfg.RoutingHistory} {
		for _, route := range routes {
			add(route)
		}
	}
	for _, overrides := range []map[string]model.MessageTopicOverride{previous.Overrides, cfg.Overrides} {
		ids := make([]string, 0, len(overrides))
		for id := range overrides {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			source, ok := topicByID(id)
			target := overrides[id].Topic
			if ok && source.Editable && target != "" && target != source.DefaultTopic {
				add(model.MessageTopicReservedRoute{SourceID: id, Topic: target})
			}
		}
	}
	cfg.RoutingHistory = history
}

// Only recognized variables become wildcards; all literal text, including
// regex punctuation in MQTT topic levels, keeps its exact meaning. Matching
// conservatively allows any expansion so a historical template cannot be
// bypassed by choosing a different device or product value later.
func reservedRoutePattern(topic string) *regexp.Regexp {
	var pattern strings.Builder
	pattern.WriteByte('^')
	offset := 0
	for _, match := range variablePattern.FindAllStringIndex(topic, -1) {
		pattern.WriteString(regexp.QuoteMeta(topic[offset:match[0]]))
		pattern.WriteString(".*")
		offset = match[1]
	}
	pattern.WriteString(regexp.QuoteMeta(topic[offset:]))
	pattern.WriteByte('$')
	return regexp.MustCompile(pattern.String())
}

func validateShared(tenant string, cfg model.MessageTopicConfig) error {
	bad := func(s string) error { return fmt.Errorf("%w：%s", ErrInvalidConfig, s) }
	if len(cfg.Rules) > 200 || len(cfg.RoutingHistory) > 10000 || !uniqueValues(cfg.RetiredTopics, 10000, 2048) {
		return bad("规则或已删除主题记录数量超限")
	}
	reserved := map[string][]*regexp.Regexp{}
	seenHistory := map[model.MessageTopicReservedRoute]bool{}
	for _, route := range cfg.RoutingHistory {
		source, exists := topicByID(route.SourceID)
		if !exists || !source.Editable || route.Topic == "" || route.Topic == source.DefaultTopic || seenHistory[route] || validateDestination(tenant, source, route.Topic) != nil {
			return bad("普通发布路由的历史地址无效")
		}
		seenHistory[route] = true
		reserved[source.Protocol] = append(reserved[source.Protocol], reservedRoutePattern(route.Topic))
	}
	for _, retired := range cfg.RetiredTopics {
		protocol, target, ok := strings.Cut(retired, ":")
		if !ok || (protocol != "mqtt" && protocol != "kafka") || validateSharedDestination(tenant, protocol, target) != nil {
			return bad("已删除主题地址无效")
		}
	}
	for id, override := range cfg.Overrides {
		source, _ := topicByID(id)
		if sharedTargetReserved(tenant, cfg, source.Protocol, override.Topic) {
			return bad("普通发布路由不能指向共享主题或已删除地址，请使用自动发布规则")
		}
	}
	for _, route := range cfg.Topics {
		if route.Protocol == "" {
			if len(route.Exposure) > 0 || route.Query != nil {
				return bad("旧规则主题不能设置共享数据范围")
			}
			continue
		}
		if route.SourceID != "" || (route.Protocol != "mqtt" && route.Protocol != "kafka") {
			return bad("主题协议无效")
		}
		target := SharedDestination(tenant, route)
		if err := validateSharedDestination(tenant, route.Protocol, target); err != nil {
			return bad(err.Error())
		}
		if slices.Contains(cfg.RetiredTopics, route.Protocol+":"+target) {
			return bad("已删除的主题地址不能复用，请更换主题名称")
		}
		for _, pattern := range reserved[route.Protocol] {
			if pattern.MatchString(target) {
				return bad("共享主题地址与普通发布路由的历史地址冲突，请更换主题名称")
			}
		}
		if len(route.Exposure) > len(topics) {
			return bad("主题历史数据源数量无效")
		}
		seen := map[string]bool{}
		for _, exposure := range route.Exposure {
			source, exists := topicByID(exposure.SourceID)
			if !exists || !source.Editable || source.Protocol != route.Protocol || seen[exposure.SourceID] || !validRuleScope(exposure.DeviceScope, exposure.DeviceIDs) {
				return bad("主题历史数据范围无效")
			}
			seen[exposure.SourceID] = true
		}
		if route.Query != nil {
			if err := ValidateQuery(*route.Query); err != nil {
				return bad(err.Error())
			}
			for _, source := range querySourceIDs(route.Protocol, route.Query.Dataset) {
				covered := false
				for _, exposure := range route.Exposure {
					if exposure.SourceID == source && scopeCovers(exposure.DeviceScope, exposure.DeviceIDs, route.Query.DeviceScope, route.Query.DeviceIDs) {
						covered = true
					}
				}
				if !covered {
					return bad("查询数据范围尚未计入主题历史授权范围")
				}
			}
		}
	}
	ids := map[string]bool{}
	for _, rule := range cfg.Rules {
		if !managedID.MatchString(rule.ID) || ids[rule.ID] || !cleanText(rule.Name, 100, true) {
			return bad("规则标识或名称无效")
		}
		ids[rule.ID] = true
		route, exists := sharedRoute(cfg, rule.TopicID)
		source, known := topicByID(rule.SourceID)
		if !exists || !known || !source.Editable || source.Protocol != route.Protocol || !validRuleScope(rule.DeviceScope, rule.DeviceIDs) {
			return bad("规则主题、数据源或设备范围无效")
		}
		if route.Query != nil {
			return bad("查询主题不能同时配置旧发送规则")
		}
		if source.ID == "kafka.video-alarm" && rule.DeviceScope != "all" {
			return bad("视频告警规则仅支持全部设备范围")
		}
		covered := false
		for _, exposure := range route.Exposure {
			if exposure.SourceID == rule.SourceID && scopeCovers(exposure.DeviceScope, exposure.DeviceIDs, rule.DeviceScope, rule.DeviceIDs) {
				covered = true
			}
		}
		if !covered {
			return bad("规则数据范围尚未计入主题历史授权范围")
		}
		if err := validateRuleFormat(rule); err != nil {
			return bad(err.Error())
		}
	}
	return nil
}

func sharedTargetReserved(tenant string, cfg model.MessageTopicConfig, protocol, target string) bool {
	if slices.Contains(cfg.RetiredTopics, protocol+":"+target) {
		return true
	}
	for _, route := range cfg.Topics {
		if route.Protocol == protocol && route.SourceID == "" && SharedDestination(tenant, route) == target {
			return true
		}
	}
	return false
}

func validateSharedDestination(tenant, protocol, target string) error {
	if protocol == "kafka" {
		if strings.HasPrefix(target, KafkaPrefix(tenant)+"managed.") {
			return errors.New("共享主题不能使用账号专属地址")
		}
		if len(target) > maxKafkaLength || !strings.HasPrefix(target, KafkaPrefix(tenant)) || !kafkaSuffix.MatchString(strings.TrimPrefix(target, KafkaPrefix(tenant))) {
			return errors.New("Kafka 主题须使用租户前缀、有效名称且不超过249字节")
		}
		return nil
	}
	if protocol != "mqtt" || !strings.HasPrefix(target, MQTTPrefix(tenant)) || target == MQTTPrefix(tenant) || len(target) > maxMQTTLength || !validMQTTTopic(target) || strings.ContainsAny(target, "{}") || strings.HasPrefix(target, MQTTPrefix(tenant)+"managed/") {
		return errors.New("MQTT 主题须使用租户前缀，不能包含通配符、变量或账号专属地址")
	}
	return nil
}

var rulePath = regexp.MustCompile(`^[\pL\pN_@:-]+(?:\.[\pL\pN_@:-]+)*$`)
var rulePlaceholder = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func validRulePath(path string) bool {
	return len(path) <= 512 && len(strings.Split(path, ".")) <= 32 && rulePath.MatchString(path)
}
func validateRuleFormat(rule model.MessageTopicRule) error {
	if len(rule.Fields) > 64 || len(rule.Template) > 64<<10 || !utf8.ValidString(rule.Template) {
		return errors.New("字段最多64项，文本模板最多64KB")
	}
	for key, path := range rule.Fields {
		if !cleanText(key, 100, true) || !validRulePath(path) {
			return errors.New("字段名称或输入路径无效")
		}
	}
	switch rule.Format {
	case "original":
		if len(rule.Fields) > 0 || rule.Template != "" {
			return errors.New("原始内容格式不能同时设置字段或模板")
		}
	case "json":
		if len(rule.Fields) == 0 || rule.Template != "" {
			return errors.New("JSON 格式需要字段映射且不能设置文本模板")
		}
	case "text":
		if rule.Template == "" || len(rule.Fields) > 0 {
			return errors.New("文本格式需要模板且不能同时设置JSON字段")
		}
		for _, match := range rulePlaceholder.FindAllStringSubmatch(rule.Template, -1) {
			if !validRulePath(strings.TrimSpace(match[1])) {
				return errors.New("文本模板字段路径无效")
			}
		}
		remaining := rulePlaceholder.ReplaceAllString(rule.Template, "")
		if strings.Contains(remaining, "{{") || strings.Contains(remaining, "}}") {
			return errors.New("文本模板变量括号不完整")
		}
	default:
		return errors.New("请选择 original、json 或 text 内容格式")
	}
	return nil
}

func pathValue(value any, path string) (any, bool) {
	for _, part := range strings.Split(path, ".") {
		switch current := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = current[part]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(current) {
				return nil, false
			}
			value = current[index]
		default:
			return nil, false
		}
	}
	return value, true
}

// PreviewRule uses the same bounded projection as live publication. There is no
// executable template language or expression evaluation.
func PreviewRule(rule model.MessageTopicRule, payload []byte) ([]byte, error) {
	if len(payload) > MaxRulePayload {
		return nil, errors.New("规则消息不能超过256KB")
	}
	if err := validateRuleFormat(rule); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var input any
	if err := decoder.Decode(&input); err != nil {
		return nil, errors.New("规则输入必须是有效JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("规则输入只能包含一条JSON消息")
	}
	if rule.Format == "original" {
		return bytes.Clone(payload), nil
	}
	if rule.Format == "json" {
		out := map[string]any{}
		for key, path := range rule.Fields {
			value, ok := pathValue(input, path)
			if !ok {
				return nil, fmt.Errorf("输入缺少字段 %s", path)
			}
			out[key] = value
		}
		result, err := json.Marshal(out)
		if len(result) > MaxRulePayload {
			return nil, errors.New("规则输出不能超过256KB")
		}
		return result, err
	}
	var out bytes.Buffer
	offset := 0
	for _, match := range rulePlaceholder.FindAllStringSubmatchIndex(rule.Template, -1) {
		out.WriteString(rule.Template[offset:match[0]])
		path := strings.TrimSpace(rule.Template[match[2]:match[3]])
		value, ok := pathValue(input, path)
		if !ok {
			return nil, fmt.Errorf("输入缺少字段 %s", path)
		}
		if text, ok := value.(string); ok {
			out.WriteString(text)
		} else {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, errors.New("规则字段不能编码")
			}
			out.Write(encoded)
		}
		if out.Len() > MaxRulePayload {
			return nil, errors.New("规则输出不能超过256KB")
		}
		offset = match[1]
	}
	out.WriteString(rule.Template[offset:])
	if out.Len() > MaxRulePayload {
		return nil, errors.New("规则输出不能超过256KB")
	}
	return out.Bytes(), nil
}

// Publication permits separate rules to render different contents without
// altering the source event or another destination's payload.
type Publication struct {
	Topic   string
	Payload []byte
}

func (s *Service) Publications(ctx context.Context, protocol, source string, payload []byte) ([]Publication, error) {
	if _, external := externalTopic(protocol, source); !external {
		return []Publication{{Topic: source, Payload: payload}}, nil
	}
	var result []Publication
	var failures []error
	target, enabled, err := s.Resolve(ctx, protocol, source, payload)
	if err != nil {
		failures = append(failures, err)
	} else if enabled {
		result = append(result, Publication{Topic: target, Payload: payload})
	}
	managed, err := s.ManagedDestinations(ctx, protocol, source, payload)
	if err != nil {
		failures = append(failures, err)
	}
	for _, target := range managed {
		result = append(result, Publication{Topic: target, Payload: payload})
	}
	shared, err := s.sharedPublications(ctx, protocol, source, payload)
	result = append(result, shared...)
	if err != nil {
		failures = append(failures, err)
	}
	return result, errors.Join(failures...)
}

func (s *Service) sharedPublications(ctx context.Context, protocol, source string, payload []byte) ([]Publication, error) {
	sourceInfo, external := externalTopic(protocol, source)
	if !external {
		return nil, nil
	}
	var identity struct {
		TenantID string `json:"tenantId"`
		DeviceID string `json:"deviceId"`
		AlarmID  string `json:"alarmId"`
		Source   string `json:"source"`
	}
	if err := json.Unmarshal(payload, &identity); err != nil || validTenant(identity.TenantID) != nil {
		return nil, errors.New("规则消息缺少有效租户")
	}
	if sourceInfo.ID == "mqtt.parsed" {
		parts := strings.Split(source, "/")
		if len(parts) != 7 || parts[3] != strings.Trim(strings.ReplaceAll(identity.TenantID, "/", "_"), " ") {
			return nil, errors.New("规则来源与消息租户不一致")
		}
	}
	if sourceInfo.ID == "mqtt.ui-action" && source != "/iot/ui-action/"+identity.TenantID {
		return nil, errors.New("规则来源与消息租户不一致")
	}
	cfg, err := s.Load(ctx, identity.TenantID)
	if err != nil {
		return nil, err
	}
	hasQuery := false
	for _, route := range cfg.Topics {
		if route.Query != nil && route.Enabled && route.Protocol == protocol && route.Query.Mode == "realtime" && slices.Contains(querySourceIDs(protocol, route.Query.Dataset), sourceInfo.ID) {
			hasQuery = true
			break
		}
	}
	if len(cfg.Rules) == 0 && !hasQuery {
		return nil, nil
	}
	if strings.HasSuffix(sourceInfo.ID, ".alarm-ai-analysis") {
		alarm, e := s.repo.GetAlarm(ctx, identity.TenantID, identity.AlarmID)
		if e != nil || alarm.TenantID != identity.TenantID || alarm.ID != identity.AlarmID {
			return nil, errors.New("规则无法定位告警设备")
		}
		identity.DeviceID, identity.Source = alarm.DeviceID, alarm.Source
	}
	var result []Publication
	checked := map[string]bool{}
	permitted := map[string]bool{}
	for _, route := range cfg.Topics {
		query := route.Query
		if query == nil || query.Mode != "realtime" || !route.Enabled || route.Protocol != protocol || !slices.Contains(querySourceIDs(protocol, query.Dataset), sourceInfo.ID) {
			continue
		}
		if !s.sharedTopicReady(ctx, identity.TenantID, cfg, route) {
			continue
		}
		out, matched, err := PreviewQuery(*query, payload)
		if err != nil {
			slog.WarnContext(ctx, "message topic query skipped", "tenant", identity.TenantID, "topicId", route.ID, "reason", err)
			continue
		}
		if matched {
			result = append(result, Publication{Topic: SharedDestination(identity.TenantID, route), Payload: out})
		}
	}
	for _, rule := range cfg.Rules {
		if !rule.Enabled || rule.SourceID != sourceInfo.ID {
			continue
		}
		route, ok := sharedRoute(cfg, rule.TopicID)
		if !ok || !route.Enabled || route.Protocol != protocol {
			continue
		}
		if rule.DeviceScope == "selected" && (identity.DeviceID == "" || identity.Source == "video" || !slices.Contains(rule.DeviceIDs, identity.DeviceID)) {
			continue
		}
		if !checked[route.ID] {
			checked[route.ID] = true
			permitted[route.ID] = s.sharedTopicReady(ctx, identity.TenantID, cfg, route)
		}
		if !permitted[route.ID] {
			continue
		}
		out, err := PreviewRule(rule, payload)
		if err != nil {
			// Conversion is optional fan-out. Reporting it as a source publish
			// failure would retry successful destinations and stop later outputs.
			slog.WarnContext(ctx, "message topic rule skipped", "tenant", identity.TenantID, "ruleId", rule.ID, "sourceId", rule.SourceID, "reason", err)
			continue
		}
		result = append(result, Publication{Topic: SharedDestination(identity.TenantID, route), Payload: out})
	}
	return result, nil
}

func (s *Service) sharedTopicReady(ctx context.Context, tenant string, cfg model.MessageTopicConfig, route model.MessageTopicRoute) bool {
	s.mu.Lock()
	resolver, now := s.resolver, s.now().Unix()
	s.mu.Unlock()
	target := SharedDestination(tenant, route)
	for _, credential := range cfg.Credentials {
		if credential.Protocol != route.Protocol {
			continue
		}
		reads, writes := slices.Contains(credential.Topics, target), slices.Contains(credential.PublishTopics, target)
		if !reads && !writes {
			continue
		}
		// A broker provision call may finish after a concurrent revocation.
		// Keep the shared destination quiet until that late completion is cleaned up.
		if credential.Provisioning {
			return false
		}
		if credential.Status == "revoked" {
			continue
		}
		if resolver == nil || credential.Status != "active" || credential.ExpiresAt <= now {
			return false
		}
		var account model.MessageTopicAccount
		for _, candidate := range cfg.Accounts {
			if candidate.ID == credential.AccountID {
				account = candidate
				break
			}
		}
		if account.ID == "" || !account.Enabled || (account.ExpiresAt > 0 && account.ExpiresAt <= now) {
			return false
		}
		identity, err := resolver(ctx, tenant, account.Username)
		if err != nil || identity.Version == "" || identity.Version != credential.AccessVersion {
			return false
		}
		if reads && (!slices.Contains(account.TopicIDs, route.ID) || !RouteAllowed(cfg, route.ID, identity, account)) {
			return false
		}
		if writes && (!slices.Contains(account.PublishTopicIDs, route.ID) || !RoutePublishAllowed(cfg, route.ID, identity, account)) {
			return false
		}
	}
	return true
}
