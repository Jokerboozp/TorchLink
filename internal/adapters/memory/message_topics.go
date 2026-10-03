package memory

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"iot-platform/internal/model"
)

func (r *Repository) ListMessageTopicDevices(ctx context.Context, tenant string, deviceIDs []string, limit int) ([]model.MessageTopicDeviceRecord, error) {
	if limit < 1 || limit > 10001 {
		return nil, errors.New("主题设备查询数量须在 1 至 10001 之间")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := idSet(deviceIDs)
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0)
	for _, device := range r.devices {
		if device.TenantID == tenant && (deviceIDs == nil || wanted[device.ID]) {
			ids = append(ids, device.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	result := make([]model.MessageTopicDeviceRecord, 0, len(ids))
	for _, id := range ids {
		record := model.MessageTopicDeviceRecord{Device: cloneManaged(r.devices[key(tenant, id)])}
		record.Device.SecretHash = ""
		if state, ok := r.states[key(tenant, id)]; ok {
			record.State = &state
		}
		result = append(result, record)
	}
	return result, nil
}

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
