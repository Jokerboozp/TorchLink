package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
)

func (r *Repository) GetTwinTopology(ctx context.Context, tenant string) (model.TwinTopology, error) {
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT COALESCE((SELECT body FROM device_twin_topology WHERE tenant_id=$1),jsonb_build_object('tenantId',$1::text,'version',0,'relations',jsonb_build_array()))`, tenant).Scan(&b)
	var v model.TwinTopology
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) UpdateTwinTopology(ctx context.Context, u model.TwinUpdate) (model.TwinTopology, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.TwinTopology{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,796))`, u.TenantID); err != nil {
		return model.TwinTopology{}, err
	}
	var b []byte
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT body FROM device_twin_topology WHERE tenant_id=$1),jsonb_build_object('tenantId',$1::text,'version',0,'relations',jsonb_build_array()))`, u.TenantID).Scan(&b); err != nil {
		return model.TwinTopology{}, err
	}
	var old model.TwinTopology
	if err = json.Unmarshal(b, &old); err != nil {
		return old, err
	}
	next, err := model.ApplyTwin(old, u)
	if err != nil {
		return old, err
	}
	ids := map[string]bool{}
	for _, relation := range u.Add {
		ids[relation.Source] = true
		ids[relation.Target] = true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	// Locks participating devices through commit, preventing concurrent deletion
	// from making a newly added relation point at a non-existent tenant resource.
	rows, err := tx.Query(ctx, `SELECT id FROM device_registry WHERE tenant_id=$1 AND id=ANY($2::text[]) ORDER BY id FOR SHARE`, u.TenantID, ordered)
	if err != nil {
		return old, err
	}
	count := 0
	for rows.Next() {
		count++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return old, err
	}
	if count != len(ids) {
		return old, errors.New("twin relation device is absent from current tenant")
	}
	b, err = json.Marshal(next)
	if err != nil {
		return old, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO device_twin_topology(tenant_id,body) VALUES($1,$2) ON CONFLICT(tenant_id) DO UPDATE SET body=excluded.body`, u.TenantID, b); err != nil {
		return old, err
	}
	return next, tx.Commit(ctx)
}
func (r *Repository) GetTwinNodes(ctx context.Context, tenant string, ids []string) ([]model.TwinNode, error) {
	if len(ids) > 200 {
		return nil, errors.New("twin node query exceeds 200")
	}
	rows, err := r.pool.Query(ctx, `SELECT d.id,COALESCE(d.body->>'name',''),d.product_id,d.status,s.body FROM device_registry d LEFT JOIN device_state s ON s.tenant_id=d.tenant_id AND s.device_id=d.id WHERE d.tenant_id=$1 AND d.id=ANY($2::text[]) ORDER BY d.id`, tenant, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TwinNode{}
	for rows.Next() {
		var n model.TwinNode
		var b []byte
		if err := rows.Scan(&n.ID, &n.Name, &n.ProductID, &n.Status, &b); err != nil {
			return nil, err
		}
		var s model.DeviceState
		if len(b) > 0 {
			if err := json.Unmarshal(b, &s); err != nil {
				return nil, err
			}
		}
		n.ConnectionStatus = s.ConnectionStatus
		n.BusinessStatus = s.BusinessStatus
		n.LastSeenAt = s.LastSeenAt
		if n.ConnectionStatus == "" {
			n.ConnectionStatus = "UNKNOWN"
		}
		if n.BusinessStatus == "" {
			n.BusinessStatus = "UNKNOWN"
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
