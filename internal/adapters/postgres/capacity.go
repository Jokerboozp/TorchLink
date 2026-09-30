package postgres

import (
	"context"
	"iot-platform/internal/model"
	"strings"
)

// capacityDeviceInUseSQL finds references added outside testing (gateways,
// access profiles, cameras) that protect a fixture device from deletion.
const capacityDeviceInUseSQL = `SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=ANY($2) UNION ALL SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND device_id=ANY($2) UNION ALL SELECT 1 FROM video_camera_mapping WHERE tenant_id=$1 AND (device_id=ANY($2) OR related_device_ids ?| $2) UNION ALL SELECT 1 FROM video_camera_relation WHERE tenant_id=$1 AND relation_type='device' AND target_id=ANY($2))`

func (r *Repository) CapacityMessageIDs(ctx context.Context, tenant string, q model.CapacityCleanupBatch) ([]string, error) {
	var pending bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM standard_message WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (raw_message_id=ANY($4) OR device_id=ANY($5)) AND processed_at=0 UNION ALL SELECT 1 FROM raw_archive_index WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND (message_id=ANY($4) OR device_id=ANY($5)) AND parse_attempted_at=0)`, tenant, q.Product, q.Devices, q.RawIDs, q.RemoveDevices).Scan(&pending)
	if err != nil {
		return nil, err
	}
	if pending {
		return nil, model.ErrResourceInUse
	}
	// An analysis started by another user must finish before its test alarm
	// disappears, otherwise the background writer could recreate deleted data.
	if err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM alarm_analysis_job j JOIN alarm_record a ON a.tenant_id=j.tenant_id AND a.id=j.alarm_id WHERE j.tenant_id=$1 AND lower(j.status) IN ('running','pending','processing') AND a.device_id=ANY($3) AND (a.device_id=ANY($5) OR a.body->>'triggerId' IN (SELECT message_id FROM standard_message WHERE tenant_id=$1 AND product_id=$2 AND device_id=ANY($3) AND raw_message_id=ANY($4)) OR a.body->'details'->'message'->>'rawMessageId'=ANY($4)))`, tenant, q.Product, q.Devices, q.RawIDs, q.RemoveDevices).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, model.ErrResourceInUse
	}
	if err = r.pool.QueryRow(ctx, capacityDeviceInUseSQL, tenant, q.RemoveDevices).Scan(&pending); err != nil {
		return nil, err
	}
	if pending {
		return nil, model.ErrResourceInUse
	}
	for _, resource := range q.Resources {
		table := capacityResourceTable(resource.Kind)
		if table == "" {
			continue
		}
		if err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE tenant_id=$1 AND id=$2 AND body->>'capacityRunId'=$3 AND lower(status) IN ('running','pending','processing'))`, tenant, resource.ID, q.RunID).Scan(&pending); err != nil {
			return nil, err
		}
		if pending {
			return nil, model.ErrResourceInUse
		}
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
	var used bool
	if err = tx.QueryRow(ctx, capacityDeviceInUseSQL, tenant, q.RemoveDevices).Scan(&used); err != nil {
		return n, err
	}
	if used {
		return n, model.ErrResourceInUse
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
	if _, err = tx.Exec(ctx, `DELETE FROM raw_ingest_reservation WHERE tenant_id=$1 AND (message_id=ANY($2) OR (metadata->>'productId'=$3 AND metadata->>'deviceId'=ANY($4)))`, tenant, q.RawIDs, q.Product, q.RemoveDevices); err != nil {
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
		tag, err = tx.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1 AND id=$2 AND body->>'capacityRunId'=$3 AND lower(status) NOT IN ('running','pending','processing')`, tenant, resource.ID, q.RunID)
		if err != nil {
			return n, err
		}
		n.Resources += tag.RowsAffected()
	}
	return n, tx.Commit(ctx)
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
