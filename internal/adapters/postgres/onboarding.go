package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
)

func (r *Repository) SaveOnboarding(ctx context.Context, b model.OnboardingBundle) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize onboarding across replicas, including listener port reservation.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(728194601)`); err != nil {
		return err
	}
	exec := func(sql string, args ...any) error { _, e := tx.Exec(ctx, sql, args...); return e }
	body := func(v any) []byte { data, _ := json.Marshal(v); return data }
	var status string
	if p := b.Product; p != nil {
		if err = exec(`INSERT INTO iot_product(tenant_id,id,status,protocol_package_id,body) VALUES($1,$2,$3,$4,$5)`, p.TenantID, p.ID, p.Status, p.ProtocolPackageID, body(p)); err != nil {
			return err
		}
		v := b.Release
		pkg := model.ProtocolPackage{TenantID: v.TenantID, ID: p.ProtocolPackageID, Name: v.ProtocolID, Version: v.Version, Protocol: v.ProtocolID, Transport: v.Transport, PayloadFormat: v.PayloadFormat, ParserType: v.ParserType, Status: v.Status, Config: v.Config, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt}
		if err = exec(`INSERT INTO protocol_package(tenant_id,id,status,parser_type,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, pkg.TenantID, pkg.ID, pkg.Status, pkg.ParserType, body(pkg)); err != nil {
			return err
		}
	}
	if err = tx.QueryRow(ctx, `SELECT status FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, b.Device.TenantID, b.Device.ProductID).Scan(&status); err != nil {
		return err
	}
	if status != "ENABLED" {
		return errors.New("product is disabled")
	}
	if v := b.Release; v != nil {
		definition := model.ProtocolDefinition{TenantID: v.TenantID, ID: v.ProtocolID, Name: v.ProtocolID, CreatedAt: v.CreatedAt, UpdatedAt: v.CreatedAt}
		if err = exec(`INSERT INTO protocol_definition(tenant_id,id,body) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, v.TenantID, v.ProtocolID, body(definition)); err != nil {
			return err
		}
		if err = exec(`INSERT INTO protocol_release(tenant_id,protocol_id,version,status,parser_type,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, v.TenantID, v.ProtocolID, v.Version, v.Status, v.ParserType, body(v)); err != nil {
			return err
		}
		var saved model.ProtocolRelease
		var data []byte
		if err = tx.QueryRow(ctx, `SELECT body FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3 FOR SHARE`, v.TenantID, v.ProtocolID, v.Version).Scan(&data); err != nil {
			return err
		}
		if err = json.Unmarshal(data, &saved); err != nil {
			return err
		}
		if saved.Status != "PUBLISHED" || saved.ParserType != v.ParserType {
			return errors.New("protocol release changed; test again")
		}
	}
	if v := b.PointTable; v != nil {
		if err = exec(`INSERT INTO point_table_release(tenant_id,protocol_id,version,source_sha256,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, v.TenantID, v.ProtocolID, v.Version, v.SourceSHA256, body(v)); err != nil {
			return err
		}
	}
	if v := b.Binding; v != nil {
		if err = exec(`INSERT INTO product_protocol_binding(tenant_id,product_id,protocol_id,version,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, v.TenantID, v.ProductID, v.ProtocolID, v.Version, body(v)); err != nil {
			return err
		}
	}
	if v := b.Profile; v != nil {
		var protocol, version string
		if err = tx.QueryRow(ctx, `SELECT protocol_id,version FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2 FOR SHARE`, v.TenantID, v.ProductID).Scan(&protocol, &version); err != nil {
			return err
		}
		if protocol != v.ProtocolID || version != v.ProtocolVersion {
			return errors.New("product binding changed; test again")
		}
		if b.ReuseProfile {
			var data []byte
			if err = tx.QueryRow(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND id=$2 FOR SHARE`, v.TenantID, v.ID).Scan(&data); err != nil {
				return err
			}
			var old model.DeviceAccessProfile
			if err = json.Unmarshal(data, &old); err != nil {
				return err
			}
			if !old.Enabled || old.ProductID != v.ProductID || old.Host != v.Host || old.Port != v.Port || old.Network != v.Network {
				return errors.New("listener changed; test again")
			}
		}
		if v.ConnectionMode != "dial" && v.Mode == "listener" && !b.ReuseProfile {
			var used bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE enabled AND COALESCE(body->>'connectionMode','')!='dial' AND body->>'mode'='listener' AND body->>'network'=$1 AND (body->>'port')::int=$2)`, v.Network, v.Port).Scan(&used); err != nil {
				return err
			}
			if used {
				return errors.New("listener port is already reserved")
			}
		}
		if !b.ReuseProfile {
			if err = exec(`INSERT INTO device_access_profile(tenant_id,id,device_id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5,$6)`, v.TenantID, v.ID, v.DeviceID, v.ProductID, v.Enabled, body(v)); err != nil {
				return err
			}
		}
	}
	d := b.Device
	if err = exec(`INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES($1,$2,$3,$4,$5,$6,$7)`, d.TenantID, d.ID, d.ProductID, d.Status, d.AccessKey, d.SecretHash, body(d)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
