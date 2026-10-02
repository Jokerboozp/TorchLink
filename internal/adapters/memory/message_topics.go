package memory

import (
	"context"
	"encoding/json"
	"sort"

	"iot-platform/internal/model"
)

func (r *Repository) ListMessageTopicTenants(_ context.Context) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tenants := make([]string, 0, len(r.messageTopicConfigs))
	for tenant := range r.messageTopicConfigs {
		tenants = append(tenants, tenant)
	}
	sort.Strings(tenants)
	return tenants, nil
}

func (r *Repository) LoadMessageTopicConfig(_ context.Context, tenant string) (model.MessageTopicConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var config model.MessageTopicConfig
	if body := r.messageTopicConfigs[tenant]; len(body) > 0 {
		if err := json.Unmarshal(body, &config); err != nil {
			return config, err
		}
	}
	return config, nil
}

func (r *Repository) SaveMessageTopicConfig(_ context.Context, tenant string, config model.MessageTopicConfig) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var previous model.MessageTopicConfig
	if body := r.messageTopicConfigs[tenant]; len(body) > 0 {
		if err := json.Unmarshal(body, &previous); err != nil {
			return false, err
		}
	}
	if previous.Revision != config.Revision {
		return false, nil
	}
	config.Revision++
	body, err := json.Marshal(config)
	if err != nil {
		return false, err
	}
	if r.messageTopicConfigs == nil {
		r.messageTopicConfigs = map[string][]byte{}
	}
	r.messageTopicConfigs[tenant] = body
	return true, nil
}
