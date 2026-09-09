package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

func (r *Repository) GetDeviceShadow(ctx context.Context, tenant, device string) (model.DeviceShadow, error) {
	s := model.DeviceShadow{TenantID: tenant, DeviceID: device, Desired: map[string]any{}, Reported: map[string]any{}}
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM device_shadow WHERE tenant_id=$1 AND device_id=$2`, tenant, device).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else if err == nil {
		err = json.Unmarshal(body, &s)
	}
	s.ComputeDelta()
	return s, err
}
func (r *Repository) UpdateDeviceShadow(ctx context.Context, u model.ShadowUpdate) (model.DeviceShadow, error) {
	var s model.DeviceShadow
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return s, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,793))`, u.TenantID+"/"+u.DeviceID); err != nil {
		return s, err
	}
	var body []byte
	err = tx.QueryRow(ctx, `SELECT body FROM device_shadow WHERE tenant_id=$1 AND device_id=$2 FOR UPDATE`, u.TenantID, u.DeviceID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else if err == nil {
		err = json.Unmarshal(body, &s)
	}
	if err != nil {
		return s, err
	}
	changed, err := model.ApplyShadow(&s, u)
	if err != nil {
		return model.DeviceShadow{}, err
	}
	if changed {
		body, err = json.Marshal(s)
		if err != nil {
			return s, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO device_shadow(tenant_id,device_id,body) VALUES($1,$2,$3) ON CONFLICT(tenant_id,device_id) DO UPDATE SET body=excluded.body`, u.TenantID, u.DeviceID, body); err != nil {
			return s, err
		}
		if u.Desired != nil {
			change := model.ShadowChange{Version: s.DesiredVersion, Timestamp: u.Timestamp, Actor: u.Actor, Desired: s.Desired}
			body, err = json.Marshal(change)
			if err != nil {
				return s, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO device_shadow_change(tenant_id,device_id,version,body) VALUES($1,$2,$3,$4)`, u.TenantID, u.DeviceID, s.DesiredVersion, body); err != nil {
				return s, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM device_shadow_change WHERE tenant_id=$1 AND device_id=$2 AND version<=$3`, u.TenantID, u.DeviceID, s.DesiredVersion-1000); err != nil {
				return s, err
			}
		}
	}
	return s, tx.Commit(ctx)
}
func (r *Repository) ListShadowChanges(ctx context.Context, tenant, device string, limit, offset int) ([]model.ShadowChange, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_shadow_change WHERE tenant_id=$1 AND device_id=$2 ORDER BY version DESC LIMIT $3 OFFSET $4`, tenant, device, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ShadowChange{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var v model.ShadowChange
		if err := json.Unmarshal(body, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
