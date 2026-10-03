package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
)

func (r *Repository) ListMessageTopicDevices(ctx context.Context, tenant string, deviceIDs []string, limit int) ([]model.MessageTopicDeviceRecord, error) {
	if limit < 1 || limit > 10001 {
		return nil, errors.New("主题设备查询数量须在 1 至 10001 之间")
	}
	rows, err := r.pool.Query(ctx, `SELECT d.body,s.body FROM device_registry d LEFT JOIN device_state s ON s.tenant_id=d.tenant_id AND s.device_id=d.id WHERE d.tenant_id=$1 AND ($2::text[] IS NULL OR d.id=ANY($2::text[])) ORDER BY d.id LIMIT $3`, tenant, deviceIDs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.MessageTopicDeviceRecord, 0)
	for rows.Next() {
		var deviceBody, stateBody []byte
		if err := rows.Scan(&deviceBody, &stateBody); err != nil {
			return nil, err
		}
		var record model.MessageTopicDeviceRecord
		if err := json.Unmarshal(deviceBody, &record.Device); err != nil {
			return nil, err
		}
		if len(stateBody) > 0 {
			record.State = &model.DeviceState{}
			if err := json.Unmarshal(stateBody, record.State); err != nil {
				return nil, err
			}
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (r *Repository) ListMessageTopicTenants(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT tenant_id FROM message_topic_configs ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tenants := []string{}
	for rows.Next() {
		var tenant string
		if err := rows.Scan(&tenant); err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}
	return tenants, rows.Err()
}

func (r *Repository) LoadMessageTopicConfig(ctx context.Context, tenant string) (model.MessageTopicConfig, error) {
	var config model.MessageTopicConfig
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body,revision FROM message_topic_configs WHERE tenant_id=$1`, tenant).Scan(&body, &config.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	revision := config.Revision
	err = json.Unmarshal(body, &config)
	config.Revision = revision
	return config, err
}

func (r *Repository) SaveMessageTopicConfig(ctx context.Context, tenant string, config model.MessageTopicConfig) (bool, error) {
	body, err := json.Marshal(config)
	if err != nil {
		return false, err
	}
	result, err := r.pool.Exec(ctx, `INSERT INTO message_topic_configs(tenant_id,revision,body) SELECT $1,1,$2::jsonb WHERE $3::bigint=0 ON CONFLICT(tenant_id) DO NOTHING`, tenant, body, config.Revision)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 1 {
		return true, nil
	}
	result, err = r.pool.Exec(ctx, `UPDATE message_topic_configs SET revision=revision+1,body=$2::jsonb WHERE tenant_id=$1 AND revision=$3`, tenant, body, config.Revision)
	return result.RowsAffected() == 1, err
}
