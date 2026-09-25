package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
)

// DeleteProtocolRelease checks active bindings and removes only the selected version.
func (r *Repository) DeleteProtocolRelease(ctx context.Context, tenant, protocolID, version string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var selected string
	err = tx.QueryRow(ctx, `SELECT version FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3 FOR UPDATE`, tenant, protocolID, version).Scan(&selected)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return err
	}
	var inUse bool
	checks := []string{
		`SELECT EXISTS(SELECT 1 FROM product_protocol_binding WHERE tenant_id=$1 AND ((protocol_id=$2 AND version=$3) OR (body->>'previousVersion'=$3 AND (body->>'previousProtocolId'=$2 OR (COALESCE(body->>'previousProtocolId','')='' AND protocol_id=$2)))))`,
		`SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND body->>'protocolId'=$2 AND body->>'protocolVersion'=$3)`,
	}
	for _, query := range checks {
		if err = tx.QueryRow(ctx, query, tenant, protocolID, version).Scan(&inUse); err != nil {
			return err
		}
		if inUse {
			return model.ErrResourceInUse
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM protocol_release WHERE tenant_id=$1 AND protocol_id=$2 AND body->>'pointTableVersion'=$3 UNION ALL SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND body->>'protocolId'=$2 AND body->>'pointTableVersion'=$3)`, tenant, protocolID, version).Scan(&inUse); err != nil {
		return err
	}
	if !inUse {
		if _, err = tx.Exec(ctx, `DELETE FROM point_table_release WHERE tenant_id=$1 AND protocol_id=$2 AND version=$3`, tenant, protocolID, version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteResource keeps the reference check and database cleanup in one transaction.
// Identifiers in the SQL below are fixed application constants, never user input.
func (r *Repository) DeleteResource(ctx context.Context, tenant, kind, id string) error {
	lookup := map[string]struct{ table, column string }{
		"device": {"device_registry", "id"}, "product": {"iot_product", "id"},
		"profile": {"device_access_profile", "id"}, "protocol": {"protocol_definition", "id"},
		"camera": {"video_camera_mapping", "camera_id"}, "alarm": {"alarm_record", "id"},
		"knowledge": {"ai_knowledge_doc", "id"},
	}
	target, ok := lookup[kind]
	if !ok {
		return model.ErrNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err = tx.QueryRow(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE tenant_id=$1 AND %s=$2)", target.table, target.column), tenant, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return model.ErrNotFound
	}
	checks := map[string][]string{
		"device": {
			"SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=$2)",
			"SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND device_id=$2)",
			"SELECT EXISTS(SELECT 1 FROM video_camera_mapping WHERE tenant_id=$1 AND (device_id=$2 OR related_device_ids ? $2))",
			"SELECT EXISTS(SELECT 1 FROM video_camera_relation WHERE tenant_id=$1 AND relation_type='device' AND target_id=$2)",
			"SELECT EXISTS(SELECT 1 FROM alarm_record WHERE tenant_id=$1 AND device_id=$2 AND status IN ('ACTIVE','ACKED'))",
		},
		"product": {
			"SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND product_id=$2)",
			"SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND (product_id=$2 OR body->'childProducts' @> jsonb_build_array(jsonb_build_object('productId',$2::text))))",
			"SELECT EXISTS(SELECT 1 FROM ai_knowledge_doc WHERE tenant_id=$1 AND product_id=$2)",
			"SELECT EXISTS(SELECT 1 FROM ai_workflow_knowledge_binding WHERE tenant_id=$1 AND body->'productIds' ? $2)",
			"SELECT EXISTS(SELECT 1 FROM alarm_rule WHERE tenant_id=$1 AND body->>'productId'=$2)",
		},
		"profile": {
			"SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND id=$2 AND enabled=true)",
			"SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND body->>'connectorProfileId'=$2)",
		},
		"protocol": {
			"SELECT EXISTS(SELECT 1 FROM product_protocol_binding WHERE tenant_id=$1 AND (protocol_id=$2 OR body->>'previousProtocolId'=$2))",
			"SELECT EXISTS(SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND body->>'protocolId'=$2)",
			"SELECT EXISTS(SELECT 1 FROM iot_product WHERE tenant_id=$1 AND protocol_package_id=$2)",
		},
		"camera": {
			"SELECT EXISTS(SELECT 1 FROM video_camera_relation WHERE tenant_id=$1 AND camera_id=$2)",
			"SELECT EXISTS(SELECT 1 FROM alarm_rule WHERE tenant_id=$1 AND body->'actions' @> jsonb_build_array(jsonb_build_object('cameraId',$2::text)))",
		},
		"alarm": {"SELECT EXISTS(SELECT 1 FROM alarm_record WHERE tenant_id=$1 AND id=$2 AND status IN ('ACTIVE','ACKED'))"},
	}
	for _, query := range checks[kind] {
		if err = tx.QueryRow(ctx, query, tenant, id).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return model.ErrResourceInUse
		}
	}
	if kind == "product" {
		if _, err = tx.Exec(ctx, "DELETE FROM product_protocol_binding WHERE tenant_id=$1 AND product_id=$2", tenant, id); err != nil {
			return err
		}
	}
	if kind == "device" {
		for _, table := range []string{"device_state", "component_alarm_state", "alarm_rule_pending"} {
			if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1 AND device_id=$2", tenant, id); err != nil {
				return err
			}
		}
	}
	if kind == "protocol" {
		for _, table := range []string{"point_table_release", "protocol_release"} {
			if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1 AND protocol_id=$2", tenant, id); err != nil {
				return err
			}
		}
	}
	if kind == "alarm" {
		if _, err = tx.Exec(ctx, "DELETE FROM alarm_ai_analysis WHERE tenant_id=$1 AND alarm_id=$2", tenant, id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM component_alarm_state WHERE tenant_id=$1 AND body->>'alarmId'=$2", tenant, id); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE tenant_id=$1 AND %s=$2", target.table, target.column), tenant, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
