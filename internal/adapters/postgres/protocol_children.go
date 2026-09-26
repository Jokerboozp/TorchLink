package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) ListManagedDeviceChildren(ctx context.Context, tenant, parent string, limit, offset int) ([]model.ManagedDevice, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=$2`, tenant, parent).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=$2 ORDER BY id LIMIT $3 OFFSET $4`, tenant, parent, max(1, min(limit, 100)), max(0, offset))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []model.ManagedDevice{}
	for rows.Next() {
		d, e := r.scanManagedDevice(rows)
		if e != nil {
			return nil, 0, e
		}
		items = append(items, d)
	}
	return items, total, rows.Err()
}

func (r *Repository) RegisterProtocolChild(ctx context.Context, expected model.DeviceAccessProfile, parentID string, identity model.ChildIdentity) (model.ManagedDevice, bool, error) {
	var empty model.ManagedDevice
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback(ctx)
	var body []byte
	if err = tx.QueryRow(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND id=$2 FOR SHARE`, expected.TenantID, expected.ID).Scan(&body); err != nil {
		return empty, false, err
	}
	var current model.DeviceAccessProfile
	if err = json.Unmarshal(body, &current); err != nil {
		return empty, false, err
	}
	parent, err := r.scanManagedDevice(tx.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, expected.TenantID, parentID))
	if err != nil {
		return empty, false, err
	}
	var parentStatus string
	if err = tx.QueryRow(ctx, `SELECT status FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR SHARE`, expected.TenantID, parent.ProductID).Scan(&parentStatus); err != nil {
		return empty, false, err
	}
	if parentStatus != "ENABLED" {
		return empty, false, model.ErrProtocolRegistration
	}
	var productID string
	for _, v := range current.ChildProducts {
		if v.Type == identity.Type {
			productID = v.ProductID
		}
	}
	if err = tx.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR SHARE`, expected.TenantID, productID).Scan(&body); err != nil {
		return empty, false, err
	}
	var product model.Product
	if err = json.Unmarshal(body, &product); err != nil {
		return empty, false, err
	}
	d, err := model.ProtocolChildDevice(expected, current, parent, product, identity, time.Now().UnixMilli())
	if err != nil {
		return empty, false, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT r.status FROM product_protocol_binding b JOIN protocol_release r ON r.tenant_id=b.tenant_id AND r.protocol_id=b.protocol_id AND r.version=b.version WHERE b.tenant_id=$1 AND b.product_id=$2 FOR SHARE OF b,r`, d.TenantID, d.ProductID).Scan(&status); err != nil {
		return empty, false, err
	}
	if status != "PUBLISHED" {
		return empty, false, model.ErrProtocolRegistration
	}
	body, err = json.Marshal(d)
	if err != nil {
		return empty, false, err
	}
	result, err := tx.Exec(ctx, `INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES($1,$2,$3,$4,$5,'',$6) ON CONFLICT(tenant_id,id) DO NOTHING`, d.TenantID, d.ID, d.ProductID, d.Status, d.AccessKey, body)
	if err != nil {
		return empty, false, err
	}
	created := result.RowsAffected() == 1
	if !created {
		old, e := r.scanManagedDevice(tx.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, d.TenantID, d.ID))
		if e != nil {
			return empty, false, e
		}
		if e = model.ExistingProtocolChild(old, d); e != nil {
			return empty, false, e
		}
		old.UpdatedAt = d.UpdatedAt
		if identity.Name != "" {
			old.Name = identity.Name
		}
		d = old
		body, _ = json.Marshal(d)
		if _, err = tx.Exec(ctx, `UPDATE device_registry SET body=$3 WHERE tenant_id=$1 AND id=$2`, d.TenantID, d.ID, body); err != nil {
			return empty, false, err
		}
	}
	parent.DeviceRole = "GATEWAY"
	body, _ = json.Marshal(parent)
	if _, err = tx.Exec(ctx, `UPDATE device_registry SET body=$3 WHERE tenant_id=$1 AND id=$2`, parent.TenantID, parent.ID, body); err != nil {
		return empty, false, err
	}
	return d, created, tx.Commit(ctx)
}
