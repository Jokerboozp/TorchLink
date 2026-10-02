package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
)

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
