package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) RegisterProtocolDevice(ctx context.Context, expected model.DeviceAccessProfile, id, name string) (model.ManagedDevice, bool, error) {
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
	if err = tx.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR SHARE`, current.TenantID, current.ProductID).Scan(&body); err != nil {
		return empty, false, err
	}
	var product model.Product
	if err = json.Unmarshal(body, &product); err != nil {
		return empty, false, err
	}
	d, err := model.ProtocolRegistrationDevice(expected, current, product, id, name, time.Now().UnixMilli())
	if err != nil {
		return empty, false, err
	}
	if current.EdgeNodeID != "" {
		return model.ManagedDevice{}, false, model.ErrProtocolRegistration
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
		d, err = r.scanManagedDevice(tx.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR SHARE`, d.TenantID, d.ID))
		if err != nil {
			return empty, false, err
		}
		if err = model.RegisteredProtocolDevice(d, current); err != nil {
			return empty, false, err
		}
	}
	return d, created, tx.Commit(ctx)
}
