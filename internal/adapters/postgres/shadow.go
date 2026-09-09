package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

func (r *Repository) GetDeviceShadow(ctx context.Context, tenant, device string, names ...string) (model.DeviceShadow, error) {
	name, err := model.ShadowName(names...)
	if err != nil {
		return model.DeviceShadow{}, err
	}
	s := model.DeviceShadow{Name: name, TenantID: tenant, DeviceID: device, Desired: map[string]any{}, Reported: map[string]any{}}
	var body []byte
	err = r.pool.QueryRow(ctx, `SELECT body FROM device_shadow WHERE tenant_id=$1 AND device_id=$2 AND name=$3`, tenant, device, name).Scan(&body)
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
	err = tx.QueryRow(ctx, `SELECT body FROM device_shadow WHERE tenant_id=$1 AND device_id=$2 AND name=$3 FOR UPDATE`, u.TenantID, u.DeviceID, u.Name).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else if err == nil {
		err = json.Unmarshal(body, &s)
	}
	if err != nil {
		return s, err
	}
	if u.Name != "" && s.TenantID == "" {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM device_shadow WHERE tenant_id=$1 AND device_id=$2 AND name<>''`, u.TenantID, u.DeviceID).Scan(&count); err != nil {
			return s, err
		}
		if count >= 16 {
			return s, model.ErrShadowCount
		}
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
		if _, err = tx.Exec(ctx, `INSERT INTO device_shadow(tenant_id,device_id,name,body) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,device_id,name) DO UPDATE SET body=excluded.body`, u.TenantID, u.DeviceID, u.Name, body); err != nil {
			return s, err
		}
		if u.Desired != nil {
			change := model.ShadowChange{Version: s.DesiredVersion, Timestamp: u.Timestamp, Actor: u.Actor, Desired: s.Desired}
			body, err = json.Marshal(change)
			if err != nil {
				return s, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO device_shadow_change(tenant_id,device_id,name,version,body) VALUES($1,$2,$3,$4,$5)`, u.TenantID, u.DeviceID, u.Name, s.DesiredVersion, body); err != nil {
				return s, err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM device_shadow_change WHERE tenant_id=$1 AND device_id=$2 AND name=$3 AND version<=$4`, u.TenantID, u.DeviceID, u.Name, s.DesiredVersion-1000); err != nil {
				return s, err
			}
		}
	}
	return s, tx.Commit(ctx)
}
func (r *Repository) ListShadowChanges(ctx context.Context, tenant, device string, limit, offset int, names ...string) ([]model.ShadowChange, error) {
	name, err := model.ShadowName(names...)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_shadow_change WHERE tenant_id=$1 AND device_id=$2 AND name=$3 ORDER BY version DESC LIMIT $4 OFFSET $5`, tenant, device, name, limit, offset)
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

func (r *Repository) ListDeviceShadowNames(ctx context.Context, tenant, device string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT name FROM device_shadow WHERE tenant_id=$1 AND device_id=$2 AND name<>'' ORDER BY name`, tenant, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
