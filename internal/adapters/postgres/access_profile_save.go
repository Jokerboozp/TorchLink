package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

func (r *Repository) saveGuardedAccessProfile(ctx context.Context, v model.DeviceAccessProfile, body []byte) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(728194601)`); err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRow(ctx, `SELECT 1 FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, v.TenantID, v.ProductID).Scan(&exists); err != nil {
		return err
	}
	var protocol, version string
	if err = tx.QueryRow(ctx, `SELECT protocol_id,version FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2`, v.TenantID, v.ProductID).Scan(&protocol, &version); errors.Is(err, pgx.ErrNoRows) {
		return model.ErrBindingChanged
	} else if err != nil {
		return err
	}
	if protocol != v.ProtocolID || version != v.ProtocolVersion {
		return model.ErrBindingChanged
	}
	var previous []byte
	safeDisable := false
	if err = tx.QueryRow(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, v.TenantID, v.ID).Scan(&previous); err == nil {
		var old model.DeviceAccessProfile
		if err = json.Unmarshal(previous, &old); err != nil {
			return err
		}
		if old.ProductID != v.ProductID || old.DeviceID != v.DeviceID {
			return model.ErrOnboardingChanged
		}
		safeDisable = model.AccessProfileDisable(old, v)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var conflict bool
	if v.DeviceID == "" {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND product_id=$2)`, v.TenantID, v.ProductID).Scan(&conflict); err != nil {
			return err
		}
		if conflict && !safeDisable {
			return model.ErrOnboardingChanged
		}
	}
	if v.Enabled && v.Mode == "listener" && v.ConnectionMode != "dial" {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE enabled AND NOT(tenant_id=$1 AND id=$2) AND body->>'mode'='listener' AND COALESCE(body->>'connectionMode','')!='dial' AND body->>'network'=$3 AND (body->>'port')::int=$4)`, v.TenantID, v.ID, v.Network, v.Port).Scan(&conflict); err != nil {
			return err
		}
		if conflict {
			return model.ErrBindingChanged
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO device_access_profile(tenant_id,id,device_id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,id) DO UPDATE SET enabled=excluded.enabled,body=excluded.body,updated_at=now()`, v.TenantID, v.ID, v.DeviceID, v.ProductID, v.Enabled, body); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
