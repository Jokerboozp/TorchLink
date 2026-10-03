package messagetopics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"iot-platform/internal/model"
)

// MessageTopicIdentity is resolved from the platform's current user and role
// policy. Version covers all policy inputs and invalidates prior credentials.
type MessageTopicIdentity struct {
	Permissions map[string]bool
	DeviceScope string
	DeviceIDs   []string
	Version     string
}

func (s *Service) SetAccessResolver(resolve func(context.Context, string, string) (MessageTopicIdentity, error)) {
	s.mu.Lock()
	s.resolver = resolve
	s.mu.Unlock()
}

// SourceAllowed is also used when issuing credentials. Message-level scope and
// knowledge restrictions are checked again for each publication.
func SourceAllowed(sourceID string, identity MessageTopicIdentity) bool {
	source, ok := topicByID(sourceID)
	if !ok || !source.Editable || (identity.DeviceScope != "all" && identity.DeviceScope != "selected") {
		return false
	}
	allowed := func(menu string) bool { return identity.Permissions["*"] || identity.Permissions["menu:"+menu] }
	if !allowed("devices") {
		return false
	}
	switch {
	case strings.HasSuffix(sourceID, ".video-alarm"):
		return allowed("alarms") && allowed("cameras") && identity.DeviceScope == "all"
	case strings.Contains(sourceID, ".alarm-") || strings.HasSuffix(sourceID, ".ui-action"):
		return allowed("alarms")
	default:
		return true
	}
}

// RouteSource returns the immutable source metadata for a live route. Deleting
// a built-in route does not delete that data source for other custom routes.
func RouteSource(cfg model.MessageTopicConfig, routeID string) (Topic, bool) {
	for _, route := range cfg.Topics {
		if route.ID == routeID {
			if route.Protocol != "" && route.SourceID == "" {
				return Topic{ID: route.ID, Name: route.Name, Protocol: route.Protocol, Direction: "outbound", DefaultTopic: route.Topic, Editable: true}, route.Protocol == "mqtt" || route.Protocol == "kafka"
			}
			source, ok := topicByID(route.SourceID)
			return source, ok && source.Editable
		}
	}
	source, ok := topicByID(routeID)
	return source, ok && source.Editable && !slices.Contains(cfg.Deleted, routeID)
}

func routeEnabled(cfg model.MessageTopicConfig, routeID string) bool {
	for _, route := range cfg.Topics {
		if route.ID == routeID {
			return route.Enabled
		}
	}
	if _, ok := RouteSource(cfg, routeID); !ok {
		return false
	}
	override, ok := cfg.Overrides[routeID]
	return !ok || override.Enabled
}

// Destination never reuses a shared topic, including after credential rotation.
// The full tenant encoding and route digest avoid delimiter and ID collisions.
func Destination(tenant, routeID string, credential model.MessageTopicCredential) string {
	sum := sha256.Sum256([]byte(routeID))
	route := hex.EncodeToString(sum[:])
	if credential.Protocol == "mqtt" {
		return MQTTPrefix(tenant) + "managed/" + credential.ID + "/" + route
	}
	return KafkaPrefix(tenant) + "managed." + credential.ID + "." + route
}

var (
	managedID   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
	managedSlug = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)
)

func cleanText(v string, max int, required bool) bool {
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) > max || (required && strings.TrimSpace(v) == "") {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func uniqueValues(values []string, maxCount, maxLength int) bool {
	if len(values) > maxCount {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !cleanText(value, maxLength, true) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validateManaged(tenant string, cfg model.MessageTopicConfig) error {
	invalid := func(reason string) error { return fmt.Errorf("%w：%s", ErrInvalidConfig, reason) }
	if len(cfg.Topics) > 100 || len(cfg.Accounts) > 100 || len(cfg.Credentials) > 1000 || !uniqueValues(cfg.Deleted, len(topics), 80) {
		return invalid("主题、账号或凭据数量超过上限")
	}
	for _, id := range cfg.Deleted {
		source, ok := topicByID(id)
		if !ok || !source.Editable {
			return invalid("系统主题不可删除")
		}
	}
	seenIDs, slugs := map[string]bool{}, map[string]bool{}
	for _, route := range cfg.Topics {
		_, reserved := topicByID(route.ID)
		source, ok := topicByID(route.SourceID)
		shared := route.Protocol != "" && route.SourceID == ""
		if !managedID.MatchString(route.ID) || reserved || seenIDs[route.ID] || (!shared && (!ok || !source.Editable || !managedSlug.MatchString(route.Topic) || route.Protocol != "")) || !cleanText(route.Name, 100, true) || !cleanText(route.Description, 500, false) {
			return invalid("自定义主题标识、名称、数据源或说明无效")
		}
		slug := source.Protocol + ":" + route.Topic
		if shared {
			slug = route.Protocol + ":" + SharedDestination(tenant, route)
		}
		if slugs[slug] {
			return invalid("同一协议的主题标识不能重复")
		}
		seenIDs[route.ID], slugs[slug] = true, true
	}
	accounts := make(map[string]model.MessageTopicAccount, len(cfg.Accounts))
	for _, account := range cfg.Accounts {
		if !managedID.MatchString(account.ID) || !cleanText(account.Name, 100, true) || !cleanText(account.Username, 128, true) || !cleanText(account.SecretHash, 256, false) || account.CreatedAt < 0 || account.ExpiresAt < 0 || !uniqueValues(account.TopicIDs, 115, 80) || !uniqueValues(account.PublishTopicIDs, 100, 80) || !uniqueValues(account.DeviceIDs, 10000, 256) {
			return invalid("对接账号资料、授权数量或有效期无效")
		}
		if _, exists := accounts[account.ID]; exists {
			return invalid("对接账号标识重复")
		}
		if account.DeviceScope != "all" && account.DeviceScope != "selected" {
			return invalid("对接账号设备范围无效")
		}
		if account.DeviceScope == "all" && len(account.DeviceIDs) > 0 {
			return invalid("全部设备范围不能同时指定设备")
		}
		for _, id := range account.TopicIDs {
			if _, ok := RouteSource(cfg, id); !ok {
				return invalid("授权引用了不存在或已删除的主题")
			}
		}
		for _, id := range account.PublishTopicIDs {
			if route, ok := sharedRoute(cfg, id); !ok || route.Query != nil {
				return invalid("发布授权只能引用非查询共享主题")
			}
		}
		accounts[account.ID] = account
	}
	credentialIDs := map[string]bool{}
	liveAccountProtocols := map[string]bool{}
	for _, credential := range cfg.Credentials {
		if !managedID.MatchString(credential.ID) || credentialIDs[credential.ID] || !managedID.MatchString(credential.AccountID) || (credential.Protocol != "mqtt" && credential.Protocol != "kafka") || !cleanText(credential.Username, 256, true) || !cleanText(credential.GroupID, 256, false) || !cleanText(credential.AccessVersion, 256, true) || credential.CreatedAt < 0 || credential.ExpiresAt <= 0 || !uniqueValues(credential.Topics, 115, 1024) || !uniqueValues(credential.PublishTopics, 100, 1024) || len(credential.Topics)+len(credential.PublishTopics) == 0 {
			return invalid("主题凭据资料、授权范围或有效期无效")
		}
		credentialIDs[credential.ID] = true
		if credential.Status != "active" && credential.Status != "provisioning" && credential.Status != "revoking" && credential.Status != "revoked" {
			return invalid("主题凭据状态无效")
		}
		prefix := MQTTPrefix(tenant) + "managed/" + credential.ID + "/"
		if credential.Protocol == "kafka" {
			prefix = KafkaPrefix(tenant) + "managed." + credential.ID + "."
		}
		for _, target := range credential.Topics {
			if validateSharedDestination(tenant, credential.Protocol, target) == nil {
				continue
			}
			if !strings.HasPrefix(target, prefix) || len(target) != len(prefix)+64 {
				return invalid("凭据主题必须属于当前租户和凭据")
			}
			if _, err := hex.DecodeString(target[len(prefix):]); err != nil {
				return invalid("凭据主题标识无效")
			}
			if credential.Protocol == "kafka" && len(target) > maxKafkaLength {
				return invalid("Kafka 主题总长不能超过 249 字节")
			}
		}
		for _, target := range credential.PublishTopics {
			if validateSharedDestination(tenant, credential.Protocol, target) != nil {
				return invalid("发布凭据主题必须属于当前租户共享主题")
			}
		}
		// Revocation records intentionally survive account/route deletion until
		// the broker confirms the old identity cannot access its destinations.
		if credential.Status == "revoking" || credential.Status == "revoked" {
			continue
		}
		key := credential.AccountID + ":" + credential.Protocol
		if liveAccountProtocols[key] {
			return invalid("同一账号的同一协议只能有一个有效或待签发凭据")
		}
		liveAccountProtocols[key] = true
		account, ok := accounts[credential.AccountID]
		if !ok {
			return invalid("凭据关联的账号不存在")
		}
		allowed := map[string]bool{}
		for _, routeID := range account.TopicIDs {
			source, ok := RouteSource(cfg, routeID)
			if ok && source.Protocol == credential.Protocol {
				if route, shared := sharedRoute(cfg, routeID); shared {
					allowed[SharedDestination(tenant, route)] = true
				} else {
					allowed[Destination(tenant, routeID, credential)] = true
				}
			}
		}
		for _, target := range credential.Topics {
			if !allowed[target] {
				return invalid("凭据主题超出账号授权")
			}
		}
		writable := map[string]bool{}
		for _, id := range account.PublishTopicIDs {
			if route, ok := sharedRoute(cfg, id); ok && route.Protocol == credential.Protocol {
				writable[SharedDestination(tenant, route)] = true
			}
		}
		for _, target := range credential.PublishTopics {
			if !writable[target] {
				return invalid("发布凭据主题超出账号授权")
			}
		}
	}
	return validateShared(tenant, cfg)
}

// Destinations keeps the existing source route while additionally publishing to
// isolated, currently authorized credentials. Internal messages never fan out.
func (s *Service) Destinations(ctx context.Context, protocol, source string, payload []byte) ([]string, error) {
	target, enabled, err := s.Resolve(ctx, protocol, source, payload)
	if err != nil {
		return nil, err
	}
	targets, err := s.ManagedDestinations(ctx, protocol, source, payload)
	if err != nil {
		return nil, err
	}
	if enabled {
		targets = append([]string{target}, targets...)
	}
	return targets, nil
}

// ManagedDestinations deliberately bypasses the routing cache: revocations on
// another API replica must stop producing to an old credential immediately.
func (s *Service) ManagedDestinations(ctx context.Context, protocol, source string, payload []byte) ([]string, error) {
	sourceInfo, external := externalTopic(protocol, source)
	if !external {
		return nil, nil
	}
	var fields struct {
		TenantID           string           `json:"tenantId"`
		DeviceID           string           `json:"deviceId"`
		AlarmID            string           `json:"alarmId"`
		Source             string           `json:"source"`
		KnowledgeScope     string           `json:"knowledgeScope"`
		KnowledgeDocuments []string         `json:"knowledgeDocuments"`
		Action             model.RuleAction `json:"action"`
	}
	if err := json.Unmarshal(payload, &fields); err != nil || validTenant(fields.TenantID) != nil {
		return nil, errors.New("对外消息缺少有效 JSON 身份")
	}
	if sourceInfo.ID == "mqtt.parsed" {
		parts := strings.Split(source, "/")
		if len(parts) != 7 || parts[3] != strings.Trim(strings.ReplaceAll(fields.TenantID, "/", "_"), " ") {
			return nil, errors.New("对外 MQTT 主题与消息租户不一致")
		}
	} else if sourceInfo.ID == "mqtt.ui-action" && source != "/iot/ui-action/"+fields.TenantID {
		return nil, errors.New("对外 MQTT 主题与消息租户不一致")
	}
	s.mu.Lock()
	resolver, now := s.resolver, s.now().Unix()
	s.mu.Unlock()
	if resolver == nil {
		return nil, nil
	}
	cfg, err := s.Load(ctx, fields.TenantID)
	if err != nil {
		return nil, err
	}
	if len(cfg.Credentials) == 0 {
		return nil, nil
	}
	if strings.HasSuffix(sourceInfo.ID, ".alarm-ai-analysis") {
		if fields.AlarmID == "" {
			return nil, nil
		}
		alarm, err := s.repo.GetAlarm(ctx, fields.TenantID, fields.AlarmID)
		if err != nil || alarm.TenantID != fields.TenantID || alarm.ID != fields.AlarmID {
			return nil, nil
		}
		fields.DeviceID, fields.Source = alarm.DeviceID, alarm.Source
	}
	accounts := map[string]model.MessageTopicAccount{}
	for _, account := range cfg.Accounts {
		accounts[account.ID] = account
	}
	identities := map[string]MessageTopicIdentity{}
	resolved := map[string]bool{}
	var targets []string
	for _, credential := range cfg.Credentials {
		if credential.Protocol != protocol || credential.Status != "active" || credential.ExpiresAt <= now {
			continue
		}
		account, exists := accounts[credential.AccountID]
		if !exists || !account.Enabled || (account.ExpiresAt > 0 && account.ExpiresAt <= now) {
			continue
		}
		if !resolved[account.Username] {
			resolved[account.Username] = true
			if identity, err := resolver(ctx, fields.TenantID, account.Username); err == nil {
				identities[account.Username] = identity
			}
		}
		identity, exists := identities[account.Username]
		if !exists || identity.Version == "" || identity.Version != credential.AccessVersion || !SourceAllowed(sourceInfo.ID, identity) {
			continue
		}
		allows := func(menu string) bool { return identity.Permissions["*"] || identity.Permissions["menu:"+menu] }
		if (fields.KnowledgeScope != "" || len(fields.KnowledgeDocuments) > 0) && !allows("knowledge") {
			continue
		}
		if fields.Action.CameraID != "" && !allows("cameras") {
			continue
		}
		if sourceInfo.ID == "kafka.video-alarm" || fields.Source == "video" {
			if identity.DeviceScope != "all" || account.DeviceScope != "all" || !allows("cameras") {
				continue
			}
		} else if fields.DeviceID == "" || !deviceInScope(identity.DeviceScope, identity.DeviceIDs, fields.DeviceID) || !deviceInScope(account.DeviceScope, account.DeviceIDs, fields.DeviceID) {
			continue
		}
		for _, routeID := range account.TopicIDs {
			routeSource, exists := RouteSource(cfg, routeID)
			if !exists || routeSource.ID != sourceInfo.ID || !routeEnabled(cfg, routeID) {
				continue
			}
			target := Destination(fields.TenantID, routeID, credential)
			if slices.Contains(credential.Topics, target) && !slices.Contains(targets, target) {
				targets = append(targets, target)
			}
		}
	}
	return targets, nil
}

func deviceInScope(scope string, devices []string, device string) bool {
	return scope == "all" || (scope == "selected" && slices.Contains(devices, device))
}
