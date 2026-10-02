package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"time"
)

func applyTemplateConfiguration(ctx context.Context, tx pgx.Tx, v model.ProtocolSwitch) error {
	change := v.Preparation
	var body []byte
	var product model.Product
	if err := tx.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2`, v.Product.TenantID, v.Product.ID).Scan(&body); err != nil {
		return err
	}
	if err := json.Unmarshal(body, &product); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND product_id=$2 AND COALESCE(device_id,'')='' FOR UPDATE`, v.Product.TenantID, v.Product.ID)
	if err != nil {
		return err
	}
	profiles := []model.DeviceAccessProfile{}
	for rows.Next() {
		var p model.DeviceAccessProfile
		if err = rows.Scan(&body); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(body, &p); err != nil {
			rows.Close()
			return err
		}
		profiles = append(profiles, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if !model.SameTemplateSnapshot(product, change.ExpectedProduct, profiles, change.ExpectedProfiles) {
		return model.ErrOnboardingChanged
	}
	rec, err := scanOnboardingRecord(tx.QueryRow(ctx, `SELECT `+onboardingColumns+` FROM onboarding_record WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, change.Record.TenantID, change.Record.ID))
	found := err == nil
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return err
	}
	if found != (change.ExpectedRevision > 0) || found && (rec.Revision != change.ExpectedRevision || rec.OwnerID != change.Record.OwnerID || rec.Kind != change.Record.Kind) {
		return model.ErrOnboardingChanged
	}
	keep := map[string]bool{}
	for _, p := range change.Profiles {
		if p.TenantID != v.Product.TenantID || p.ProductID != v.Product.ID || p.DeviceID != "" {
			return model.ErrOnboardingChanged
		}
		keep[p.ID] = true
		var conflict bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND id=$2 AND (product_id<>$3 OR COALESCE(device_id,'')<>''))`, p.TenantID, p.ID, p.ProductID).Scan(&conflict); err != nil {
			return err
		}
		if conflict {
			return model.ErrOnboardingChanged
		}
		if p.Enabled && p.Mode == "listener" && p.ConnectionMode != "dial" {
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE enabled AND NOT(tenant_id=$1 AND product_id=$2) AND body->>'mode'='listener' AND COALESCE(body->>'connectionMode','')!='dial' AND body->>'network'=$3 AND (body->>'port')::int=$4)`, p.TenantID, p.ProductID, p.Network, p.Port).Scan(&conflict); err != nil {
				return err
			}
			if conflict {
				return model.ErrBindingChanged
			}
		}
	}
	for _, p := range profiles {
		if !keep[p.ID] {
			var used bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND body->>'connectorProfileId'=$2)`, p.TenantID, p.ID).Scan(&used); err != nil {
				return err
			}
			if used {
				return model.ErrOnboardingChanged
			}
			if _, err = tx.Exec(ctx, `DELETE FROM device_access_profile WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.ID); err != nil {
				return err
			}
		}
	}
	for _, p := range change.Profiles {
		body, err = json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO device_access_profile(tenant_id,id,device_id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,id) DO UPDATE SET device_id=excluded.device_id,product_id=excluded.product_id,enabled=excluded.enabled,body=excluded.body,updated_at=now()`, p.TenantID, p.ID, p.DeviceID, p.ProductID, p.Enabled, body); err != nil {
			return err
		}
	}
	next := change.Record
	now := time.Now().UnixMilli()
	if found {
		_, err = tx.Exec(ctx, `UPDATE onboarding_record SET status=$3,revision=revision+1,updated_at=$4,body=$5 WHERE tenant_id=$1 AND id=$2`, next.TenantID, next.ID, next.Status, now, next.Body)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO onboarding_record (`+onboardingColumns+`) VALUES($1,$2,$3,$4,$5,1,$6,$6,$7)`, next.TenantID, next.ID, next.OwnerID, next.Kind, next.Status, now, next.Body)
	}
	return err
}
