package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"strings"
)

// capacityDeviceInUseSQL finds references added outside testing (gateways,
// access profiles, cameras) that protect a fixture device from deletion.
const capacityDeviceInUseSQL = `SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=ANY($2) UNION ALL SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND device_id=ANY($2) UNION ALL SELECT 1 FROM video_camera_mapping WHERE tenant_id=$1 AND (device_id=ANY($2) OR related_device_ids ?| $2) UNION ALL SELECT 1 FROM video_camera_relation WHERE tenant_id=$1 AND relation_type='device' AND target_id=ANY($2))`

func (r *Repository) CapacityMessageIDs(ctx context.Context, tenant string, q model.CapacityCleanupBatch) ([]string, error) {
	if err := checkCapacityHistorical(ctx, r.pool, tenant, q); err != nil {
		return nil, err
	}
	if err := checkCapacityActive(ctx, r.pool, tenant, q); err != nil {
		return nil, err
	}
	var pending bool
	if err := r.pool.QueryRow(ctx, capacityDeviceInUseSQL, tenant, q.RemoveDevices).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, model.ErrResourceInUse
	}
	// Exclusive devices are deleted by their device predicate in ClickHouse;
	// only ledger batches need derived IDs. Do not load millions of IDs here.
	rows, err := r.pool.Query(ctx, `SELECT message_id FROM standard_message WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND raw_message_id=ANY($4)`, tenant, q.Product, q.Devices, q.RawIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) CleanupCapacityData(ctx context.Context, tenant string, q model.CapacityCleanupBatch) (model.CapacityCleanupCounts, error) {
	var n model.CapacityCleanupCounts
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return n, err
	}
	defer tx.Rollback(ctx)
	if err = checkCapacityHistorical(ctx, tx, tenant, q); err != nil {
		return n, err
	}
	if err = checkCapacityActive(ctx, tx, tenant, q); err != nil {
		return n, err
	}
	var used bool
	if err = tx.QueryRow(ctx, capacityDeviceInUseSQL, tenant, q.RemoveDevices).Scan(&used); err != nil {
		return n, err
	}
	if used {
		return n, model.ErrResourceInUse
	}
	if _, err = tx.Exec(ctx, `CREATE TEMP TABLE capacity_removed_resources(kind text,id text) ON COMMIT DROP; CREATE TEMP TABLE capacity_inspection_audits(id text) ON COMMIT DROP`); err != nil {
		return n, err
	}
	if _, err = tx.Exec(ctx, `CREATE TEMP TABLE capacity_raws ON COMMIT DROP AS SELECT message_id FROM raw_archive_index WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (message_id=ANY($4) OR device_id=ANY($5)) UNION SELECT message_id FROM raw_message_log WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (message_id=ANY($4) OR device_id=ANY($5))`, tenant, q.Product, q.Devices, q.RawIDs, q.RemoveDevices); err != nil {
		return n, err
	}
	// Remember derived IDs before deleting standards so alarm and state records
	// can be scoped to this run even when devices are shared.
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE capacity_messages ON COMMIT DROP AS SELECT message_id FROM standard_message WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (raw_message_id=ANY($4) OR device_id=ANY($5))`, tenant, q.Product, q.Devices, q.RawIDs, q.RemoveDevices)
	if err != nil {
		return n, err
	}
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE capacity_alarms ON COMMIT DROP AS SELECT id FROM alarm_record WHERE tenant_id=$1 AND device_id=ANY($2) AND (device_id=ANY($3) OR body->>'triggerId' IN (SELECT message_id FROM capacity_messages) OR body->'details'->'message'->>'rawMessageId'=ANY($4))`, tenant, q.Devices, q.RemoveDevices, q.RawIDs)
	if err != nil {
		return n, err
	}
	for _, sql := range []string{
		`DELETE FROM alarm_analysis_job WHERE tenant_id=$1 AND alarm_id IN (SELECT id FROM capacity_alarms)`,
		`DELETE FROM alarm_ai_analysis WHERE tenant_id=$1 AND alarm_id IN (SELECT id FROM capacity_alarms)`,
		`DELETE FROM component_alarm_state WHERE tenant_id=$1 AND (device_id=ANY($2) OR body->>'alarmId' IN (SELECT id FROM capacity_alarms))`,
		`DELETE FROM alarm_rule_pending WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM device_state WHERE tenant_id=$1 AND (device_id=ANY($2) OR body->>'lastMessageId' IN (SELECT message_id FROM capacity_messages))`,
		`DELETE FROM device_state_event WHERE tenant_id=$1 AND (device_id=ANY($2) OR body->'state'->>'lastMessageId' IN (SELECT message_id FROM capacity_messages))`,
		`DELETE FROM device_command WHERE tenant_id=$1 AND device_id=ANY($2)`,
		`DELETE FROM device_credential_revocation WHERE tenant_id=$1 AND device_id=ANY($2)`,
	} {
		args := []any{tenant}
		if strings.Contains(sql, "$2") {
			args = append(args, q.RemoveDevices)
		}
		if _, err = tx.Exec(ctx, sql, args...); err != nil {
			return n, err
		}
	}
	tag, err := tx.Exec(ctx, `DELETE FROM alarm_record WHERE tenant_id=$1 AND id IN (SELECT id FROM capacity_alarms)`, tenant)
	if err != nil {
		return n, err
	}
	n.Alarms = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `DELETE FROM standard_message WHERE tenant_id=$1 AND message_id IN (SELECT message_id FROM capacity_messages)`, tenant)
	if err != nil {
		return n, err
	}
	n.Standard = tag.RowsAffected()
	for _, table := range []string{"raw_message_log", "raw_archive_index"} {
		tag, err = tx.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (message_id=ANY($4) OR device_id=ANY($5))`, tenant, q.Product, q.Devices, q.RawIDs, q.RemoveDevices)
		if err != nil {
			return n, err
		}
		if table == "raw_archive_index" {
			n.Raw = tag.RowsAffected()
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM raw_ingest_reservation WHERE tenant_id=$1 AND (message_id IN (SELECT message_id FROM capacity_raws) OR (metadata->>'productId'=$2 AND metadata->>'deviceId'=ANY($3)))`, tenant, q.Product, q.RemoveDevices); err != nil {
		return n, err
	}
	tag, err = tx.Exec(ctx, `DELETE FROM device_registry WHERE tenant_id=$1 AND product_id=$2 AND id=ANY($3)`, tenant, q.Product, q.RemoveDevices)
	if err != nil {
		return n, err
	}
	n.Devices = tag.RowsAffected()
	for _, resource := range q.Resources {
		table := capacityResourceTable(resource.Kind)
		if table == "" {
			continue
		}
		if resource.Kind == "alarm-analysis" {
			if _, err = tx.Exec(ctx, `DELETE FROM alarm_ai_analysis WHERE tenant_id=$1 AND body->>'capacityRunId'=$3 AND (alarm_id,knowledge_scope) IN (SELECT alarm_id,knowledge_scope FROM alarm_analysis_job WHERE tenant_id=$1 AND id=$2 AND body->>'capacityRunId'=$3 AND lower(status) NOT IN ('running','pending','processing'))`, tenant, resource.ID, q.RunID); err != nil {
				return n, err
			}
		}
		condition := ""
		if resource.Kind == "inspection" {
			condition = ` AND NOT EXISTS(SELECT 1 FROM health_inspection_item WHERE tenant_id=$1 AND job_id=$2 AND COALESCE(body->>'productId','')<>$4)`
		}
		args := []any{tenant, resource.ID, q.RunID}
		if condition != "" {
			args = append(args, q.Product)
		}
		auditSQL := ""
		if resource.Kind == "inspection" {
			auditSQL = `, audits AS (INSERT INTO capacity_inspection_audits SELECT 'inspection_'||(body->'report'->>'generatedAt') FROM removed RETURNING id)`
		}
		tag, err = tx.Exec(ctx, `WITH removed AS (DELETE FROM `+table+` WHERE tenant_id=$1 AND id=$2 AND body->>'capacityRunId'=$3 AND lower(status) NOT IN ('running','pending','processing','queued')`+condition+` RETURNING id,body)`+auditSQL+` INSERT INTO capacity_removed_resources SELECT '`+resource.Kind+`',id FROM removed`, args...)
		if err != nil {
			return n, err
		}
		n.Resources += tag.RowsAffected()
	}
	if err = cleanupCapacityAssociated(ctx, tx, tenant, q, &n); err != nil {
		return n, err
	}
	if len(q.RemoveDevices) > 0 {
		var body []byte
		var revision int64
		err = tx.QueryRow(ctx, `SELECT body,revision FROM platform_access WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&body, &revision)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return n, err
		}
		if err == nil {
			next, refs, e := model.PruneCapacityAccessReferences(body, q.RemoveDevices)
			if e != nil {
				return n, e
			}
			if refs > 0 {
				if _, e = tx.Exec(ctx, `UPDATE platform_access SET revision=revision+1,body=jsonb_set($2::jsonb,'{revision}',to_jsonb(revision+1)) WHERE tenant_id=$1`, tenant, next); e != nil {
					return n, e
				}
				n.AccessReferences = refs
			}
		}
	}
	if _, err = tx.Exec(ctx, `CREATE TEMP TABLE capacity_rules ON COMMIT DROP AS SELECT id FROM alarm_rule WHERE tenant_id=$1 AND product_id=$2 AND body->>'alarmType'='CAPACITY_TEST' AND (id=$3 OR $4)`, tenant, q.Product, q.RemoveRule, q.RemoveProduct); err != nil {
		return n, err
	}
	if q.RemoveProduct {
		if err = finishCapacityProduct(ctx, tx, tenant, q, &n); err != nil {
			return n, err
		}
	}
	tag, err = tx.Exec(ctx, `DELETE FROM audit_log WHERE tenant_id=$1 AND ((target_type='device' AND target_id=ANY($2)) OR (target_type='alarm' AND target_id IN (SELECT id FROM capacity_alarms)) OR (target_type='raw-message' AND target_id IN (SELECT message_id FROM capacity_raws)) OR (target_type='rule' AND target_id IN (SELECT id FROM capacity_rules)) OR (target_type='product' AND target_id=$3 AND $4) OR (target_type='replay' AND target_id IN (SELECT id FROM capacity_removed_resources WHERE kind='replay')) OR (target_type IN ('inspection','health-inspection','device-health') AND (target_id IN (SELECT id FROM capacity_removed_resources WHERE kind='inspection') OR target_id IN (SELECT id FROM capacity_inspection_audits))) OR details->>'deviceId'=ANY($2) OR (details->>'productId'=$3 AND $4))`, tenant, q.RemoveDevices, q.Product, q.RemoveProduct)
	if err != nil {
		return n, err
	}
	n.Audits = tag.RowsAffected()
	return n, tx.Commit(ctx)
}

func checkCapacityActive(ctx context.Context, db capacityQuerier, tenant string, q model.CapacityCleanupBatch) error {
	var active bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM standard_message WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (raw_message_id=ANY($4) OR device_id=ANY($5)) AND processed_at=0 UNION ALL SELECT 1 FROM raw_archive_index WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (message_id=ANY($4) OR device_id=ANY($5)) AND parse_attempted_at=0 UNION ALL SELECT 1 FROM health_inspection_job WHERE tenant_id=$1 AND cardinality($5::text[])>0 AND lower(status) IN ('running','pending','processing','queued') UNION ALL SELECT 1 FROM replay_task WHERE tenant_id=$1 AND cardinality($5::text[])>0 AND lower(status) IN ('running','pending','processing','queued') AND (body->>'deviceId'=ANY($5) OR body->>'productId'=$2 OR (COALESCE(body->>'deviceId','')='' AND COALESCE(body->>'productId','')='')) UNION ALL SELECT 1 FROM alarm_analysis_job j JOIN alarm_record a ON a.tenant_id=j.tenant_id AND a.id=j.alarm_id WHERE j.tenant_id=$1 AND lower(j.status) IN ('running','pending','processing','queued') AND a.device_id=ANY($3) AND (a.device_id=ANY($5) OR a.body->>'triggerId' IN (SELECT message_id FROM standard_message WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND raw_message_id=ANY($4)) OR a.body->'details'->'message'->>'rawMessageId'=ANY($4)))`, tenant, q.Product, q.Devices, q.RawIDs, q.RemoveDevices).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return model.ErrResourceInUse
	}
	for _, resource := range q.Resources {
		if table := capacityResourceTable(resource.Kind); table != "" {
			if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE tenant_id=$1 AND id=$2 AND body->>'capacityRunId'=$3 AND lower(status) IN ('running','pending','processing','queued'))`, tenant, resource.ID, q.RunID).Scan(&active); err != nil {
				return err
			}
			if active {
				return model.ErrResourceInUse
			}
		}
	}
	return nil
}

func cleanupCapacityAssociated(ctx context.Context, tx pgx.Tx, tenant string, q model.CapacityCleanupBatch, n *model.CapacityCleanupCounts) error {
	if len(q.RemoveDevices) == 0 && !q.RemoveProduct {
		return nil
	}
	// A mixed report is immutable and kept intact. Never prune its items or
	// rewrite a summary/AI opinion to pretend it was generated for fewer devices.
	var mixed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM health_inspection_item i WHERE i.tenant_id=$1 AND i.body->>'deviceId'=ANY($2) AND EXISTS(SELECT 1 FROM health_inspection_item other WHERE other.tenant_id=i.tenant_id AND other.job_id=i.job_id AND COALESCE(other.body->>'productId','')<>$3))`, tenant, q.RemoveDevices, q.Product).Scan(&mixed); err != nil {
		return err
	}
	if mixed {
		n.Warnings = append(n.Warnings, "含非测试设备的混合巡检报告已完整保留")
	}
	tag, err := tx.Exec(ctx, `WITH removed AS (DELETE FROM health_inspection_job j WHERE tenant_id=$1 AND lower(status) NOT IN ('running','pending','processing','queued') AND EXISTS(SELECT 1 FROM health_inspection_item i WHERE i.tenant_id=j.tenant_id AND i.job_id=j.id) AND NOT EXISTS(SELECT 1 FROM health_inspection_item i WHERE i.tenant_id=j.tenant_id AND i.job_id=j.id AND NOT COALESCE((i.body->>'deviceId'=ANY($2) OR ($4 AND i.body->>'productId'=$3 AND i.body->>'deviceName' IN ('容量测试 '||(i.body->>'deviceId'),'压测设备 '||(i.body->>'deviceId'),'GB26875 设备 '||substring(i.body->>'deviceId' from 9)))),false)) RETURNING id,body), audits AS (INSERT INTO capacity_inspection_audits SELECT 'inspection_'||(body->'report'->>'generatedAt') FROM removed RETURNING id) INSERT INTO capacity_removed_resources SELECT 'inspection',id FROM removed`, tenant, q.RemoveDevices, q.Product, q.RemoveProduct)
	if err != nil {
		return err
	}
	n.Resources += tag.RowsAffected()
	tag, err = tx.Exec(ctx, `WITH removed AS (DELETE FROM replay_task WHERE tenant_id=$1 AND lower(status) NOT IN ('running','pending','processing','queued') AND (body->>'deviceId'=ANY($2) OR ($4 AND body->>'productId'=$3 AND COALESCE(body->>'deviceId','')='')) RETURNING id) INSERT INTO capacity_removed_resources SELECT 'replay',id FROM removed`, tenant, q.RemoveDevices, q.Product, q.RemoveProduct)
	if err != nil {
		return err
	}
	n.Resources += tag.RowsAffected()
	return nil
}

func finishCapacityProduct(ctx context.Context, tx pgx.Tx, tenant string, q model.CapacityCleanupBatch, n *model.CapacityCleanupCounts) error {
	var body []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, q.Product).Scan(&body); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	var p model.Product
	if err := json.Unmarshal(body, &p); err != nil {
		return err
	}
	source := model.CapacityFixtureSource(p)
	if source == "" || (q.Historical && !strings.EqualFold(p.Status, "DISABLED")) {
		return model.ErrResourceInUse
	}
	v, err := capacityFixture(ctx, tx, tenant, body, false)
	if err != nil {
		return err
	}
	if v.DeviceCount != 0 || v.BlockedReason != "" {
		return model.ErrResourceInUse
	}
	if q.Historical && strings.HasPrefix(source, "legacy-cap") {
		tag, err := tx.Exec(ctx, `DELETE FROM device_access_profile WHERE tenant_id=$1 AND product_id=$2 AND device_id='' AND enabled=false`, tenant, q.Product)
		if err != nil {
			return err
		}
		n.Profiles += tag.RowsAffected()
	}
	tag, err := tx.Exec(ctx, `DELETE FROM alarm_rule WHERE tenant_id=$1 AND id IN (SELECT id FROM capacity_rules)`, tenant)
	if err != nil {
		return err
	}
	n.Rules += tag.RowsAffected()
	if _, err = tx.Exec(ctx, `DELETE FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2`, tenant, q.Product); err != nil {
		return err
	}
	if v.ProtocolID != "" && !q.KeepProtocol {
		for _, table := range []string{"point_table_release", "protocol_release", "protocol_definition"} {
			column := "protocol_id"
			if table == "protocol_definition" {
				column = "id"
			}
			if _, err = tx.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1 AND `+column+`=$2`, tenant, v.ProtocolID); err != nil {
				return err
			}
		}
		tag, err = tx.Exec(ctx, `DELETE FROM protocol_package WHERE tenant_id=$1 AND id=$2`, tenant, p.ProtocolPackageID)
		if err != nil {
			return err
		}
		n.Protocols += tag.RowsAffected()
	}
	tag, err = tx.Exec(ctx, `DELETE FROM iot_product WHERE tenant_id=$1 AND id=$2`, tenant, q.Product)
	if err != nil {
		return err
	}
	n.Products += tag.RowsAffected()
	return nil
}

func capacityResourceTable(kind string) string {
	switch kind {
	case "inspection":
		return "health_inspection_job"
	case "alarm-analysis":
		return "alarm_analysis_job"
	case "replay":
		return "replay_task"
	}
	return ""
}
