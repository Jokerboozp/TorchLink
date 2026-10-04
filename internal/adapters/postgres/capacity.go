package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

// capacityProductSQL selects products created by the capacity tool; see
// model.IsCapacityFixture. A prefix alone never establishes ownership.
const capacityProductSQL = `protocol_package_id='iot-standard@1.0.0' AND body->>'name'='容量测试标准设备 '||id AND body->>'description' LIKE 'capacity-test 自动创建%'`

// capacityDeviceSQL selects fixture devices that were not repurposed.
const capacityDeviceSQL = `COALESCE(body->>'gatewayId','')='' AND COALESCE(NULLIF(body->>'deviceRole',''),'DIRECT')='DIRECT' AND body->>'registrationSource'='ONBOARDING' AND body->>'name'='容量测试 '||id`

// capacityDeviceInUseSQL finds references added outside testing (gateways,
// access profiles, cameras) and unfinished processing that protect devices.
const capacityDeviceInUseSQL = `SELECT EXISTS(
SELECT 1 FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=ANY($2)
UNION ALL SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND device_id=ANY($2)
UNION ALL SELECT 1 FROM video_camera_mapping WHERE tenant_id=$1 AND (device_id=ANY($2) OR related_device_ids ?| $2)
UNION ALL SELECT 1 FROM video_camera_relation WHERE tenant_id=$1 AND relation_type='device' AND target_id=ANY($2)
UNION ALL SELECT 1 FROM standard_message WHERE tenant_id=$1 AND device_id=ANY($2) AND processed_at=0
UNION ALL SELECT 1 FROM raw_archive_index WHERE tenant_id=$1 AND device_id=ANY($2) AND parse_attempted_at=0
UNION ALL SELECT 1 FROM alarm_analysis_job j JOIN alarm_record a ON a.tenant_id=j.tenant_id AND a.id=j.alarm_id WHERE j.tenant_id=$1 AND a.device_id=ANY($2) AND lower(j.status) IN ('running','pending','processing','queued'))`

// capacityRunSQL matches module tasks owned by one run ($2) or every run ($3).
const capacityRunSQL = `COALESCE(body->>'capacityRunId','')<>'' AND ($3 OR body->>'capacityRunId'=$2)`

func (r *Repository) ListCapacityFixtureProducts(ctx context.Context, tenant string) ([]model.CapacityFixtureProduct, error) {
	rows, err := r.pool.Query(ctx, `SELECT p.id,p.body->>'name',(SELECT count(*) FROM device_registry d WHERE d.tenant_id=p.tenant_id AND d.product_id=p.id),(SELECT count(*) FROM raw_archive_index x WHERE x.tenant_id=p.tenant_id AND x.product_id=p.id) FROM iot_product p WHERE p.tenant_id=$1 AND `+capacityProductSQL+` ORDER BY p.id LIMIT 1000`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CapacityFixtureProduct{}
	for rows.Next() {
		var v model.CapacityFixtureProduct
		if err = rows.Scan(&v.ProductID, &v.Name, &v.DeviceCount, &v.RawMessages); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) ListCapacityFixtureDevices(ctx context.Context, tenant, product, after string, limit int) ([]string, error) {
	var fixture bool
	err := r.pool.QueryRow(ctx, `SELECT `+capacityProductSQL+` FROM iot_product WHERE tenant_id=$1 AND id=$2`, tenant, product).Scan(&fixture)
	if errors.Is(err, pgx.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !fixture {
		return nil, model.ErrResourceInUse
	}
	rows, err := r.pool.Query(ctx, `SELECT id FROM device_registry WHERE tenant_id=$1 AND product_id=$2 AND id>$3 AND `+capacityDeviceSQL+` ORDER BY id LIMIT $4`, tenant, product, after, max(1, min(limit, 1000)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CleanupCapacityData removes fixture devices with all their data, module
// tasks owned by the run, and finally the empty product, in one transaction.
func (r *Repository) CleanupCapacityData(ctx context.Context, tenant string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	var n model.CapacityCleanupCounts
	if q.Product == "" && (len(q.Devices) > 0 || q.RemoveProduct) {
		return n, model.ErrResourceInUse
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return n, err
	}
	defer tx.Rollback(ctx)
	exists := false
	if q.Product != "" {
		var fixture bool
		err = tx.QueryRow(ctx, `SELECT `+capacityProductSQL+` FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, q.Product).Scan(&fixture)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return n, err
		case !fixture:
			return n, model.ErrResourceInUse
		default:
			exists = true
		}
	}
	if len(q.Devices) > 0 {
		var foreign bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND id=ANY($2) AND (product_id<>$3 OR NOT (`+capacityDeviceSQL+`)))`, tenant, q.Devices, q.Product).Scan(&foreign); err != nil {
			return n, err
		}
		var used bool
		if err = tx.QueryRow(ctx, capacityDeviceInUseSQL, tenant, q.Devices).Scan(&used); err != nil {
			return n, err
		}
		if foreign || used {
			return n, model.ErrResourceInUse
		}
	}
	var running bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM health_inspection_job WHERE tenant_id=$1 AND `+capacityRunSQL+` AND lower(status) IN ('running','pending','processing','queued') UNION ALL SELECT 1 FROM replay_task WHERE tenant_id=$1 AND `+capacityRunSQL+` AND lower(status) IN ('running','pending','processing','queued'))`, tenant, q.RunID, q.AllRuns).Scan(&running); err != nil {
		return n, err
	}
	if running {
		return n, model.ErrResourceInUse
	}
	// pgx prepares parameterized statements, so each one is sent separately.
	for _, sql := range []string{
		`CREATE TEMP TABLE capacity_alarms ON COMMIT DROP AS SELECT id FROM alarm_record WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`CREATE TEMP TABLE capacity_raws ON COMMIT DROP AS SELECT message_id FROM raw_archive_index WHERE tenant_id=$1 AND device_id=ANY($2)`,
	} {
		if _, err = tx.Exec(ctx, sql, tenant, q.Devices); err != nil {
			return n, err
		}
	}
	if _, err = tx.Exec(ctx, `CREATE TEMP TABLE capacity_removed(kind text,id text) ON COMMIT DROP`); err != nil {
		return n, err
	}
	for _, sql := range []string{
		`DELETE FROM alarm_analysis_job WHERE tenant_id=$1 AND alarm_id IN (SELECT id FROM capacity_alarms)`,
		`DELETE FROM alarm_ai_analysis WHERE tenant_id=$1 AND alarm_id IN (SELECT id FROM capacity_alarms)`,
		`DELETE FROM component_alarm_state WHERE tenant_id=$1 AND (device_id=ANY($2) OR body->>'alarmId' IN (SELECT id FROM capacity_alarms))`,
		`DELETE FROM alarm_rule_pending WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM device_state WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM device_state_event WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM device_command WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM device_credential_revocation WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM raw_message_log WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM raw_ingest_reservation WHERE tenant_id=$1 AND (message_id IN (SELECT message_id FROM capacity_raws) OR metadata->>'deviceId'=ANY($2))`,
	} {
		if _, err = tx.Exec(ctx, sql, capacityArgs(sql, tenant, q.Devices)...); err != nil {
			return n, err
		}
	}
	counts := []struct {
		sql    string
		target *int64
	}{
		{`DELETE FROM alarm_record WHERE tenant_id=$1 AND id IN (SELECT id FROM capacity_alarms)`, &n.Alarms},
		{`DELETE FROM standard_message WHERE tenant_id=$1 AND device_id=ANY($2)`, &n.Standard},
		{`DELETE FROM raw_archive_index WHERE tenant_id=$1 AND device_id=ANY($2)`, &n.Raw},
		{`DELETE FROM device_registry WHERE tenant_id=$1 AND id=ANY($2)`, &n.Devices},
	}
	for _, c := range counts {
		tag, err := tx.Exec(ctx, c.sql, capacityArgs(c.sql, tenant, q.Devices)...)
		if err != nil {
			return n, err
		}
		*c.target = tag.RowsAffected()
	}
	if q.RunID != "" || q.AllRuns {
		for _, table := range []struct{ name, kind string }{{"health_inspection_job", "inspection"}, {"replay_task", "replay"}, {"alarm_analysis_job", "alarm-analysis"}} {
			if _, err = tx.Exec(ctx, `WITH removed AS (DELETE FROM `+table.name+` WHERE tenant_id=$1 AND `+capacityRunSQL+` AND lower(status) NOT IN ('running','pending','processing','queued') RETURNING id,body) INSERT INTO capacity_removed SELECT '`+table.kind+`',id FROM removed UNION ALL SELECT 'audit','inspection_'||(body->'report'->>'generatedAt') FROM removed WHERE '`+table.kind+`'='inspection'`, tenant, q.RunID, q.AllRuns); err != nil {
				return n, err
			}
		}
		if _, err = tx.Exec(ctx, `DELETE FROM alarm_ai_analysis WHERE tenant_id=$1 AND `+capacityRunSQL, tenant, q.RunID, q.AllRuns); err != nil {
			return n, err
		}
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM capacity_removed WHERE kind<>'audit'`).Scan(&n.Resources); err != nil {
			return n, err
		}
	}
	if len(q.Devices) > 0 {
		refs, e := pruneAccessDevices(ctx, tx, tenant, q.Devices)
		if e != nil {
			return n, e
		}
		n.AccessReferences = refs
	}
	productRemoved := false
	if q.RemoveProduct && exists {
		var remaining, referenced bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND product_id=$2)`, tenant, q.Product).Scan(&remaining); err != nil {
			return n, err
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alarm_rule WHERE tenant_id=$1 AND product_id=$2 AND COALESCE(body->>'alarmType','')<>'CAPACITY_TEST' UNION ALL SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND (product_id=$2 OR body->'childProducts' @> jsonb_build_array(jsonb_build_object('productId',$2::text))) UNION ALL SELECT 1 FROM ai_knowledge_doc WHERE tenant_id=$1 AND product_id=$2)`, tenant, q.Product).Scan(&referenced); err != nil {
			return n, err
		}
		switch {
		case remaining:
			n.Warnings = append(n.Warnings, "测试产品仍有保留运行使用的设备，产品已保留")
		case referenced:
			n.Warnings = append(n.Warnings, "测试产品仍被规则、接入配置或知识文档引用，产品已保留")
		default:
			tag, err := tx.Exec(ctx, `WITH removed AS (DELETE FROM alarm_rule WHERE tenant_id=$1 AND product_id=$2 AND body->>'alarmType'='CAPACITY_TEST' RETURNING id) INSERT INTO capacity_removed SELECT 'rule',id FROM removed`, tenant, q.Product)
			if err != nil {
				return n, err
			}
			n.Rules = tag.RowsAffected()
			if _, err = tx.Exec(ctx, `DELETE FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2`, tenant, q.Product); err != nil {
				return n, err
			}
			if tag, err = tx.Exec(ctx, `DELETE FROM iot_product WHERE tenant_id=$1 AND id=$2`, tenant, q.Product); err != nil {
				return n, err
			}
			n.Products = tag.RowsAffected()
			productRemoved = true
		}
	}
	tag, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE tenant_id=$1 AND ((target_type='device' AND target_id=ANY($2)) OR details->>'deviceId'=ANY($2) OR (target_type='alarm' AND target_id IN (SELECT id FROM capacity_alarms)) OR (target_type='raw-message' AND target_id IN (SELECT message_id FROM capacity_raws)) OR (target_type='rule' AND target_id IN (SELECT id FROM capacity_removed WHERE kind='rule')) OR (target_type='product' AND target_id=$3 AND $4) OR (target_type='replay' AND target_id IN (SELECT id FROM capacity_removed WHERE kind='replay')) OR (target_type IN ('inspection','health-inspection','device-health') AND target_id IN (SELECT id FROM capacity_removed WHERE kind IN ('inspection','audit'))))`, tenant, q.Devices, q.Product, productRemoved)
	if err != nil {
		return n, err
	}
	n.Audits = tag.RowsAffected()
	return n, tx.Commit(ctx)
}

// capacityArgs passes the device list only to statements that reference it;
// PostgreSQL rejects unused prepared parameters.
func capacityArgs(sql, tenant string, devices []string) []any {
	if strings.Contains(sql, "$2") {
		return []any{tenant, devices}
	}
	return []any{tenant}
}
