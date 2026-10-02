package messagetopics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	cacheTTL       = 2 * time.Second
	maxCacheSize   = 256
	maxMQTTLength  = 1024
	maxKafkaLength = 249
)

var (
	ErrInvalidConfig = errors.New("消息主题配置无效")
	kafkaSuffix      = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)
	variablePattern  = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9]*)\}`)
)

type cacheEntry struct {
	config  model.MessageTopicConfig
	expires time.Time
}

type Service struct {
	repo       ports.Repository
	mu         sync.Mutex
	cache      map[string]cacheEntry
	generation uint64
	now        func() time.Time
	resolver   func(context.Context, string, string) (MessageTopicIdentity, error)
}

func New(repo ports.Repository) *Service {
	return &Service{repo: repo, cache: make(map[string]cacheEntry), now: time.Now}
}

func (s *Service) Load(ctx context.Context, tenant string) (model.MessageTopicConfig, error) {
	if err := validTenant(tenant); err != nil {
		return model.MessageTopicConfig{}, err
	}
	cfg, err := s.repo.LoadMessageTopicConfig(ctx, tenant)
	if err != nil {
		return model.MessageTopicConfig{}, err
	}
	if err := validate(tenant, cfg); err != nil {
		return model.MessageTopicConfig{}, err
	}
	return cloneConfig(cfg), nil
}

// Validate can be used by an API to distinguish invalid user input from a
// storage failure. Save always repeats validation before writing.
func Validate(tenant string, cfg model.MessageTopicConfig) error {
	if err := validTenant(tenant); err != nil {
		return err
	}
	return validate(tenant, cfg)
}

// Save uses the caller's revision as a compare-and-swap expectation. Invalid
// policies never reach storage; a successful change invalidates local readers.
func (s *Service) Save(ctx context.Context, tenant string, cfg model.MessageTopicConfig) (bool, error) {
	if err := Validate(tenant, cfg); err != nil {
		return false, err
	}
	ok, err := s.repo.SaveMessageTopicConfig(ctx, tenant, cloneConfig(cfg))
	if err != nil {
		return false, err
	}
	// A conflict also means this process may be serving an older configuration.
	s.mu.Lock()
	delete(s.cache, tenant)
	s.generation++
	s.mu.Unlock()
	return ok, nil
}

func validTenant(tenant string) error {
	if strings.TrimSpace(tenant) == "" || !utf8.ValidString(tenant) {
		return fmt.Errorf("%w：缺少有效租户", ErrInvalidConfig)
	}
	return nil
}

func cloneConfig(cfg model.MessageTopicConfig) model.MessageTopicConfig {
	out := cfg
	out.Overrides = make(map[string]model.MessageTopicOverride, len(cfg.Overrides))
	for id, override := range cfg.Overrides {
		out.Overrides[id] = override
	}
	out.Topics = slices.Clone(cfg.Topics)
	out.Deleted = slices.Clone(cfg.Deleted)
	out.Accounts = slices.Clone(cfg.Accounts)
	for i := range out.Accounts {
		out.Accounts[i].TopicIDs = slices.Clone(out.Accounts[i].TopicIDs)
		out.Accounts[i].DeviceIDs = slices.Clone(out.Accounts[i].DeviceIDs)
	}
	out.Credentials = slices.Clone(cfg.Credentials)
	for i := range out.Credentials {
		out.Credentials[i].Topics = slices.Clone(out.Credentials[i].Topics)
	}
	return out
}

func validate(tenant string, cfg model.MessageTopicConfig) error {
	if cfg.Revision < 0 || len(cfg.Overrides) > len(topics) {
		return fmt.Errorf("%w：版本或覆盖数量无效", ErrInvalidConfig)
	}
	for id, override := range cfg.Overrides {
		topic, ok := topicByID(id)
		if !ok || !topic.Editable {
			return fmt.Errorf("%w：主题 %s 不可编辑", ErrInvalidConfig, id)
		}
		if !utf8.ValidString(override.Description) || utf8.RuneCountInString(override.Description) > 500 {
			return fmt.Errorf("%w：%s 的说明不得超过 500 字", ErrInvalidConfig, id)
		}
		if override.Topic == "" || override.Topic == topic.DefaultTopic {
			continue
		}
		if err := validateDestination(tenant, topic, override.Topic); err != nil {
			return fmt.Errorf("%w：%s：%s", ErrInvalidConfig, id, err)
		}
	}
	return validateManaged(tenant, cfg)
}

func validateDestination(tenant string, topic Topic, destination string) error {
	if topic.Protocol == "kafka" {
		prefix := KafkaPrefix(tenant)
		if strings.HasPrefix(destination, prefix+"managed.") {
			return errors.New("账号凭据专属主题地址不能用于普通发布路由")
		}
		if len(destination) > maxKafkaLength || !strings.HasPrefix(destination, prefix) || !kafkaSuffix.MatchString(strings.TrimPrefix(destination, prefix)) {
			return errors.New("Kafka 主题须使用本租户专属前缀，后缀仅允许字母、数字、点、下划线和短横线，总长不超过 249 字节")
		}
		return nil
	}
	prefix := MQTTPrefix(tenant)
	if strings.HasPrefix(destination, prefix+"managed/") {
		return errors.New("账号凭据专属主题地址不能用于普通发布路由")
	}
	if len(destination) > maxMQTTLength || !strings.HasPrefix(destination, prefix) || len(destination) == len(prefix) || !utf8.ValidString(destination) {
		return errors.New("MQTT 主题须使用本租户专属前缀并包含非空后缀，总长不超过 1024 字节")
	}
	for _, match := range variablePattern.FindAllStringSubmatch(destination, -1) {
		if !slices.Contains(topic.Variables, match[1]) {
			return fmt.Errorf("不支持变量 {%s}", match[1])
		}
	}
	literal := variablePattern.ReplaceAllString(destination, "value")
	if strings.ContainsAny(literal, "{}") || !validMQTTTopic(literal) {
		return errors.New("MQTT 主题不能包含通配符、空层级、空白、控制字符或无效变量")
	}
	return nil
}

func validMQTTTopic(topic string) bool {
	if strings.ContainsAny(topic, "+#{}") || strings.HasSuffix(topic, "/") || strings.Contains(topic, "//") {
		return false
	}
	for _, r := range topic {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return utf8.ValidString(topic)
}

func (s *Service) cached(ctx context.Context, tenant string) (model.MessageTopicConfig, error) {
	for {
		s.mu.Lock()
		now := s.now()
		if entry, ok := s.cache[tenant]; ok && now.Before(entry.expires) {
			cfg := cloneConfig(entry.config)
			s.mu.Unlock()
			return cfg, nil
		}
		generation := s.generation
		s.mu.Unlock()
		// Bound the age of the database snapshot from the beginning of the
		// read. A delayed read must not gain another full TTL after it returns.
		loadCtx, cancel := context.WithTimeout(ctx, cacheTTL)
		cfg, err := s.Load(loadCtx, tenant)
		cancel()
		if err != nil {
			// Never replace a stored policy with permissive defaults when storage
			// is unavailable, even if this tenant has never been seen here.
			return model.MessageTopicConfig{}, err
		}
		s.mu.Lock()
		expires := now.Add(cacheTTL)
		if !s.now().Before(expires) {
			s.mu.Unlock()
			return model.MessageTopicConfig{}, errors.New("读取消息主题配置超时，请重试")
		}
		if generation != s.generation {
			s.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return model.MessageTopicConfig{}, err
			}
			continue
		}
		if len(s.cache) >= maxCacheSize {
			oldestTenant := ""
			var oldest time.Time
			for id, entry := range s.cache {
				if oldestTenant == "" || entry.expires.Before(oldest) {
					oldestTenant, oldest = id, entry.expires
				}
			}
			delete(s.cache, oldestTenant)
		}
		s.cache[tenant] = cacheEntry{config: cloneConfig(cfg), expires: expires}
		s.mu.Unlock()
		return cfg, nil
	}
}

// Resolve only interprets known external source topics. Unknown, internal and
// device-transport topics pass through without parsing or consulting storage.
func (s *Service) Resolve(ctx context.Context, protocol, sourceTopic string, payload []byte) (string, bool, error) {
	topic, external := externalTopic(protocol, sourceTopic)
	if !external {
		return sourceTopic, true, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return "", false, errors.New("对外消息缺少有效 JSON 身份")
	}
	value := func(field string) (string, error) {
		var v string
		if err := json.Unmarshal(fields[field], &v); err != nil || strings.TrimSpace(v) == "" || !utf8.ValidString(v) {
			return "", fmt.Errorf("对外消息缺少有效的 %s", field)
		}
		return v, nil
	}
	tenant, err := value("tenantId")
	if err != nil {
		return "", false, err
	}
	// These legacy paths contain a tenant identity. A malformed caller cannot
	// cause another tenant's source topic to use this payload's routing policy.
	if topic.ID == "mqtt.parsed" || topic.ID == "mqtt.ui-action" {
		parts := strings.Split(sourceTopic, "/")
		want := strings.Trim(strings.ReplaceAll(tenant, "/", "_"), " ")
		if topic.ID == "mqtt.ui-action" {
			// UI actions use the tenant without model sanitization.
			if sourceTopic != "/iot/ui-action/"+tenant {
				return "", false, errors.New("对外 MQTT 主题与消息租户不一致")
			}
		} else if len(parts) != 7 || parts[3] != want {
			return "", false, errors.New("对外 MQTT 主题与消息租户不一致")
		}
	}
	cfg, err := s.cached(ctx, tenant)
	if err != nil {
		return "", false, fmt.Errorf("读取消息主题配置失败：%w", err)
	}
	if slices.Contains(cfg.Deleted, topic.ID) {
		return "", false, nil
	}
	override, exists := cfg.Overrides[topic.ID]
	if !exists {
		return sourceTopic, true, nil
	}
	if !override.Enabled {
		return "", false, nil
	}
	if override.Topic == "" || override.Topic == topic.DefaultTopic {
		return sourceTopic, true, nil
	}
	destination := override.Topic
	if protocol == "mqtt" {
		for _, match := range variablePattern.FindAllStringSubmatch(destination, -1) {
			v, err := value(match[1])
			if err != nil {
				return "", false, err
			}
			if strings.Contains(v, "/") || !validMQTTTopic(v) {
				return "", false, fmt.Errorf("变量 {%s} 不是有效的 MQTT 主题层级", match[1])
			}
			destination = strings.ReplaceAll(destination, match[0], v)
		}
		if len(destination) > maxMQTTLength || !validMQTTTopic(destination) {
			return "", false, errors.New("生成的 MQTT 主题无效或超过 1024 字节")
		}
		if strings.HasPrefix(destination, MQTTPrefix(tenant)+"managed/") {
			return "", false, errors.New("生成的 MQTT 主题不能进入账号凭据专属地址")
		}
	}
	return destination, true, nil
}
