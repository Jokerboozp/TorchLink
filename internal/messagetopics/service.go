package messagetopics

import (
	"context"
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
	sanitize(tenant, &cfg)
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
	out.Topics = slices.Clone(cfg.Topics)
	for i := range out.Topics {
		out.Topics[i].Query = cloneQuery(cfg.Topics[i].Query)
		out.Topics[i].KeyIDs = slices.Clone(cfg.Topics[i].KeyIDs)
		out.Topics[i].Exposure = slices.Clone(cfg.Topics[i].Exposure)
		for j := range out.Topics[i].Exposure {
			out.Topics[i].Exposure[j].DeviceIDs = slices.Clone(cfg.Topics[i].Exposure[j].DeviceIDs)
		}
	}
	out.RetiredTopics = slices.Clone(cfg.RetiredTopics)
	out.Credentials = slices.Clone(cfg.Credentials)
	for i := range out.Credentials {
		out.Credentials[i].Topics = slices.Clone(out.Credentials[i].Topics)
	}
	return out
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
