package messagetopics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

// MaxPayload bounds one published message, including snapshot results.
const MaxPayload = 256 << 10

// Destination is stable across keys and credential rotations. Topic can be a
// tenant-owned complete address or a readable suffix entered in the UI.
func Destination(tenant string, route model.MessageTopicRoute) string {
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

func validateDestination(tenant, protocol, target string) error {
	if protocol == "kafka" {
		if len(target) > maxKafkaLength || !strings.HasPrefix(target, KafkaPrefix(tenant)) || !kafkaSuffix.MatchString(strings.TrimPrefix(target, KafkaPrefix(tenant))) {
			return errors.New("Kafka 主题须使用租户前缀、有效名称且不超过249字节")
		}
		return nil
	}
	if protocol != "mqtt" || !strings.HasPrefix(target, MQTTPrefix(tenant)) || target == MQTTPrefix(tenant) || len(target) > maxMQTTLength || !validMQTTTopic(target) {
		return errors.New("MQTT 主题须使用租户前缀，不能包含通配符或空层级")
	}
	return nil
}

func findRoute(cfg model.MessageTopicConfig, id string) (model.MessageTopicRoute, bool) {
	for _, route := range cfg.Topics {
		if route.ID == id {
			return route, true
		}
	}
	return model.MessageTopicRoute{}, false
}

// AccumulateQueryExposure only widens a topic's data boundary. Call before
// saving a query, including on a disabled topic.
func AccumulateQueryExposure(cfg *model.MessageTopicConfig, topicID string, query model.MessageTopicQuery) error {
	if err := ValidateQuery(query); err != nil {
		return fmt.Errorf("%w：%v", ErrInvalidConfig, err)
	}
	if cfg == nil {
		return fmt.Errorf("%w：缺少主题配置", ErrInvalidConfig)
	}
	index := slices.IndexFunc(cfg.Topics, func(route model.MessageTopicRoute) bool { return route.ID == topicID })
	if index < 0 {
		return fmt.Errorf("%w：查询主题不存在", ErrInvalidConfig)
	}
	route := &cfg.Topics[index]
	sources := querySourceIDs(route.Protocol, query.Dataset)
	if len(sources) == 0 {
		return fmt.Errorf("%w：查询主题协议无效", ErrInvalidConfig)
	}
	for _, source := range sources {
		j := slices.IndexFunc(route.Exposure, func(e model.MessageTopicExposure) bool { return e.SourceID == source })
		if j < 0 {
			route.Exposure = append(route.Exposure, model.MessageTopicExposure{SourceID: source, DeviceScope: query.DeviceScope, DeviceIDs: slices.Clone(query.DeviceIDs)})
			continue
		}
		exposure := &route.Exposure[j]
		switch {
		case exposure.DeviceScope == "all":
		case query.DeviceScope == "all":
			exposure.DeviceScope, exposure.DeviceIDs = "all", nil
		default:
			for _, id := range query.DeviceIDs {
				if !slices.Contains(exposure.DeviceIDs, id) {
					exposure.DeviceIDs = append(exposure.DeviceIDs, id)
				}
			}
			slices.Sort(exposure.DeviceIDs)
		}
	}
	return nil
}

// RetireTopic removes a topic while keeping its address reserved: the broker
// still holds its history, so a new topic must not inherit those messages.
func RetireTopic(tenant string, cfg *model.MessageTopicConfig, id string) bool {
	index := slices.IndexFunc(cfg.Topics, func(route model.MessageTopicRoute) bool { return route.ID == id })
	if index < 0 {
		return false
	}
	route := cfg.Topics[index]
	cfg.RetiredTopics = append(cfg.RetiredTopics, route.Protocol+":"+Destination(tenant, route))
	cfg.Topics = slices.Delete(cfg.Topics, index, index+1)
	return true
}

// sanitize drops records of the former routing model. Their broker identities
// are kept as revocations so old credentials are still removed from brokers.
func sanitize(tenant string, cfg *model.MessageTopicConfig) {
	cfg.Topics = slices.DeleteFunc(cfg.Topics, func(route model.MessageTopicRoute) bool {
		if route.Query != nil && (route.Protocol == "mqtt" || route.Protocol == "kafka") {
			return false
		}
		if route.Protocol == "mqtt" || route.Protocol == "kafka" {
			retired := route.Protocol + ":" + Destination(tenant, route)
			if validateDestination(tenant, route.Protocol, Destination(tenant, route)) == nil && !slices.Contains(cfg.RetiredTopics, retired) {
				cfg.RetiredTopics = append(cfg.RetiredTopics, retired)
			}
		}
		return true
	})
	for i := range cfg.Credentials {
		if cfg.Credentials[i].KeyID == "" && cfg.Credentials[i].Status != "revoked" {
			cfg.Credentials[i].Status = "revoking"
		}
	}
}

func validate(tenant string, cfg model.MessageTopicConfig) error {
	bad := func(s string) error { return fmt.Errorf("%w：%s", ErrInvalidConfig, s) }
	if cfg.Revision < 0 || len(cfg.Topics) > 100 || len(cfg.Credentials) > 1000 || !uniqueValues(cfg.RetiredTopics, 10000, 2048) {
		return bad("主题、凭据或已删除主题数量超过上限")
	}
	for _, retired := range cfg.RetiredTopics {
		protocol, target, ok := strings.Cut(retired, ":")
		if !ok || validateDestination(tenant, protocol, target) != nil {
			return bad("已删除主题地址无效")
		}
	}
	ids, destinations := map[string]bool{}, map[string]bool{}
	for _, route := range cfg.Topics {
		if !managedID.MatchString(route.ID) || ids[route.ID] || !cleanText(route.Name, 100, true) || !cleanText(route.Description, 500, false) {
			return bad("主题标识、名称或说明无效")
		}
		ids[route.ID] = true
		if route.Query == nil {
			return bad("主题必须配置数据查询")
		}
		target := Destination(tenant, route)
		if err := validateDestination(tenant, route.Protocol, target); err != nil {
			return bad(err.Error())
		}
		if destinations[route.Protocol+":"+target] {
			return bad("同一协议的主题地址不能重复")
		}
		destinations[route.Protocol+":"+target] = true
		if slices.Contains(cfg.RetiredTopics, route.Protocol+":"+target) {
			return bad("已删除的主题地址不能复用，请更换主题名称")
		}
		if !uniqueValues(route.KeyIDs, 100, 80) {
			return bad("订阅密钥列表无效")
		}
		if len(route.Exposure) > len(topics) {
			return bad("主题历史数据源数量无效")
		}
		seen := map[string]bool{}
		for _, exposure := range route.Exposure {
			source, exists := topicByID(exposure.SourceID)
			if !exists || !source.Editable || source.Protocol != route.Protocol || seen[exposure.SourceID] || !validScope(exposure.DeviceScope, exposure.DeviceIDs) {
				return bad("主题历史数据范围无效")
			}
			seen[exposure.SourceID] = true
		}
		if err := ValidateQuery(*route.Query); err != nil {
			return bad(err.Error())
		}
		for _, source := range querySourceIDs(route.Protocol, route.Query.Dataset) {
			if !slices.ContainsFunc(route.Exposure, func(e model.MessageTopicExposure) bool {
				return e.SourceID == source && scopeCovers(e.DeviceScope, e.DeviceIDs, route.Query.DeviceScope, route.Query.DeviceIDs)
			}) {
				return bad("查询数据范围尚未计入主题历史授权范围")
			}
		}
	}
	credentialIDs, live := map[string]bool{}, map[string]bool{}
	for _, c := range cfg.Credentials {
		if !managedID.MatchString(c.ID) || credentialIDs[c.ID] || (c.Protocol != "mqtt" && c.Protocol != "kafka") || !cleanText(c.Username, 256, true) || !cleanText(c.GroupID, 256, false) || c.CreatedAt < 0 || c.ExpiresAt <= 0 || !uniqueValues(c.Topics, 100, 1024) {
			return bad("主题凭据资料、授权范围或有效期无效")
		}
		credentialIDs[c.ID] = true
		switch c.Status {
		case "revoking", "revoked":
			// Revocation records outlive deleted topics and keys until the
			// broker confirms the identity no longer has access.
			continue
		case "active", "provisioning":
		default:
			return bad("主题凭据状态无效")
		}
		if !managedID.MatchString(c.KeyID) || !cleanText(c.AccessVersion, 256, true) || len(c.Topics) == 0 {
			return bad("主题凭据缺少密钥或授权范围")
		}
		if live[c.KeyID+":"+c.Protocol] {
			return bad("同一密钥的同一协议只能有一个有效凭据")
		}
		live[c.KeyID+":"+c.Protocol] = true
		for _, target := range c.Topics {
			route, ok := routeByDestination(tenant, cfg, c.Protocol, target)
			if !ok || !slices.Contains(route.KeyIDs, c.KeyID) {
				return bad("凭据主题超出密钥授权")
			}
		}
	}
	return nil
}

func routeByDestination(tenant string, cfg model.MessageTopicConfig, protocol, target string) (model.MessageTopicRoute, bool) {
	for _, route := range cfg.Topics {
		if route.Protocol == protocol && Destination(tenant, route) == target {
			return route, true
		}
	}
	return model.MessageTopicRoute{}, false
}

// CredentialTopics lists the exact destinations a key may currently read.
func CredentialTopics(tenant string, cfg model.MessageTopicConfig, keyID, protocol string, identity MessageTopicIdentity) []string {
	var out []string
	for _, route := range cfg.Topics {
		if route.Protocol == protocol && slices.Contains(route.KeyIDs, keyID) && RouteAllowed(route, identity) {
			out = append(out, Destination(tenant, route))
		}
	}
	return out
}

// Publication is one rendered message for one destination.
type Publication struct {
	Topic   string
	Payload []byte
}

// Publications keeps the platform's source publication and adds every
// realtime query topic that matches it. Query fan-out is best effort: a
// policy or projection failure is logged and never fails or retries the source.
func (s *Service) Publications(ctx context.Context, protocol, source string, payload []byte) []Publication {
	result := []Publication{{Topic: source, Payload: payload}}
	sourceInfo, external := externalTopic(protocol, source)
	if !external {
		return result
	}
	var identity struct {
		TenantID string `json:"tenantId"`
	}
	if err := json.Unmarshal(payload, &identity); err != nil || validTenant(identity.TenantID) != nil {
		return result
	}
	// The parsed MQTT path carries the tenant; a mismatched payload must not
	// select another tenant's topics.
	if sourceInfo.ID == "mqtt.parsed" {
		parts := strings.Split(source, "/")
		if len(parts) != 7 || parts[3] != strings.Trim(strings.ReplaceAll(identity.TenantID, "/", "_"), " ") {
			return result
		}
	}
	matches := func(route model.MessageTopicRoute) bool {
		return route.Enabled && route.Protocol == protocol && route.Query.Mode == "realtime" && slices.Contains(querySourceIDs(protocol, route.Query.Dataset), sourceInfo.ID)
	}
	// The cached copy only skips tenants without a matching topic; readiness
	// and revocation decisions always read the current stored policy.
	if cached, err := s.cached(ctx, identity.TenantID); err == nil && !slices.ContainsFunc(cached.Topics, matches) {
		return result
	}
	cfg, err := s.Load(ctx, identity.TenantID)
	if err != nil {
		slog.WarnContext(ctx, "message topic policy unavailable", "tenant", identity.TenantID, "reason", err)
		return result
	}
	for _, route := range cfg.Topics {
		if !matches(route) || !s.topicReady(ctx, identity.TenantID, cfg, route) {
			continue
		}
		out, matched, err := PreviewQuery(*route.Query, payload)
		if err != nil {
			slog.WarnContext(ctx, "message topic query skipped", "tenant", identity.TenantID, "topicId", route.ID, "reason", err)
			continue
		}
		if matched {
			result = append(result, Publication{Topic: Destination(identity.TenantID, route), Payload: out})
		}
	}
	return result
}

// topicReady pauses a topic while any issued credential could read it with a
// stale policy: pending revocations, provisioning races and changed users.
func (s *Service) topicReady(ctx context.Context, tenant string, cfg model.MessageTopicConfig, route model.MessageTopicRoute) bool {
	s.mu.Lock()
	resolver, now := s.resolver, s.now().Unix()
	s.mu.Unlock()
	target := Destination(tenant, route)
	for _, credential := range cfg.Credentials {
		if credential.Protocol != route.Protocol || !slices.Contains(credential.Topics, target) || credential.Status == "revoked" {
			continue
		}
		if credential.Provisioning || credential.Status != "active" || credential.ExpiresAt <= now || resolver == nil {
			return false
		}
		identity, err := resolver(ctx, tenant, credential.KeyID)
		if err != nil || identity.Version == "" || identity.Version != credential.AccessVersion {
			return false
		}
		if !slices.Contains(route.KeyIDs, credential.KeyID) || !RouteAllowed(route, identity) {
			return false
		}
	}
	return true
}

// pathValue reads a dotted path with numeric array indexes.
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

var fieldPath = regexp.MustCompile(`^[\pL\pN_@:-]+(?:\.[\pL\pN_@:-]+)*$`)

func validFieldPath(path string) bool {
	return len(path) <= 512 && len(strings.Split(path, ".")) <= 32 && fieldPath.MatchString(path)
}
