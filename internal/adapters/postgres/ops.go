package postgres

import (
	"context"
	"encoding/json"

	"iot-platform/internal/model"
)

func (r *Repository) ListOpsItems(ctx context.Context, tenantID, username, kind string, limit int) ([]model.OpsUserItem, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `SELECT id,name,body,created_at,updated_at FROM ops_user_item WHERE tenant_id=$1 AND username=$2 AND kind=$3 ORDER BY updated_at DESC LIMIT $4`, tenantID, username, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.OpsUserItem{}
	for rows.Next() {
		item := model.OpsUserItem{TenantID: tenantID, Username: username, Kind: kind}
		var body []byte
		if err := rows.Scan(&item.ID, &item.Name, &body, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &item.Body); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) SaveOpsItem(ctx context.Context, item model.OpsUserItem) error {
	body, err := json.Marshal(item.Body)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO ops_user_item(tenant_id,username,kind,id,name,body,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8)
ON CONFLICT(tenant_id,username,kind,id) DO UPDATE SET name=EXCLUDED.name,body=EXCLUDED.body,updated_at=EXCLUDED.updated_at`,
		item.TenantID, item.Username, item.Kind, item.ID, item.Name, body, item.CreatedAt, item.UpdatedAt)
	return err
}

func (r *Repository) DeleteOpsItem(ctx context.Context, tenantID, username, kind, id string) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM ops_user_item WHERE tenant_id=$1 AND username=$2 AND kind=$3 AND id=$4`, tenantID, username, kind, id)
	return result.RowsAffected() == 1, err
}

func (r *Repository) TrimOpsItems(ctx context.Context, tenantID, username, kind string, keep int) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM ops_user_item WHERE tenant_id=$1 AND username=$2 AND kind=$3 AND id NOT IN (
SELECT id FROM ops_user_item WHERE tenant_id=$1 AND username=$2 AND kind=$3 ORDER BY updated_at DESC LIMIT $4)`, tenantID, username, kind, keep)
	return err
}
