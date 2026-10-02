package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

type capacityQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const capacityProductCandidatesSQL = `((protocol_package_id='iot-standard@1.0.0' AND body->>'name'='容量测试标准设备 '||id AND body->>'description' LIKE 'capacity-test 自动创建%') OR (upper(status)='DISABLED' AND body->'metadata'->>'source'='CAP' AND body->>'name' LIKE '容量%' AND (protocol_package_id='iot-standard@1.0.0' OR (protocol_package_id LIKE 'cap-%' AND protocol_package_id LIKE '%gb26875%'))))`

func capacityFixtureDeviceSQL(source string) string {
	base := `COALESCE(body->>'gatewayId','')='' AND COALESCE(NULLIF(body->>'deviceRole',''),'DIRECT')='DIRECT' AND `
	if source == "legacy-cap-gb26875" {
		return base + `(body->>'registrationSource'='PROTOCOL_AUTO' AND body->>'autoRegistered'='true' AND id LIKE 'gb26875\_%' ESCAPE '\' AND body->>'name'='GB26875 设备 '||substring(id from 9))`
	}
	return base + `(body->>'registrationSource'='ONBOARDING' AND body->>'name' IN ('容量测试 '||id,'压测设备 '||id))`
}

const capacityFixtureBlockedSQL = `SELECT EXISTS(
SELECT 1 FROM device_registry d WHERE d.tenant_id=$1 AND d.body->>'gatewayId' IN (SELECT id FROM device_registry WHERE tenant_id=$1 AND product_id=$2)
UNION ALL SELECT 1 FROM device_access_profile x WHERE x.tenant_id=$1 AND (x.device_id IN (SELECT id FROM device_registry WHERE tenant_id=$1 AND product_id=$2) OR (x.product_id=$2 AND (NOT $3 OR x.enabled OR x.device_id<>'')) OR x.body->'childProducts' @> jsonb_build_array(jsonb_build_object('productId',$2::text)))
UNION ALL SELECT 1 FROM device_access_profile x JOIN device_registry d ON d.tenant_id=x.tenant_id AND d.body->>'connectorProfileId'=x.id WHERE x.tenant_id=$1 AND x.product_id=$2 AND d.product_id<>$2
UNION ALL SELECT 1 FROM video_camera_mapping c JOIN device_registry d ON d.tenant_id=c.tenant_id AND (c.device_id=d.id OR c.related_device_ids ? d.id) WHERE d.tenant_id=$1 AND d.product_id=$2
UNION ALL SELECT 1 FROM video_camera_relation c JOIN device_registry d ON d.tenant_id=c.tenant_id AND c.relation_type='device' AND c.target_id=d.id WHERE d.tenant_id=$1 AND d.product_id=$2
UNION ALL SELECT 1 FROM ai_knowledge_doc WHERE tenant_id=$1 AND product_id=$2
UNION ALL SELECT 1 FROM ai_workflow_knowledge_binding WHERE tenant_id=$1 AND body->'productIds' ? $2
UNION ALL SELECT 1 FROM alarm_rule WHERE tenant_id=$1 AND product_id=$2 AND COALESCE(body->>'alarmType','')<>'CAPACITY_TEST'
UNION ALL SELECT 1 FROM standard_message WHERE tenant_id=$1 AND product_id=$2 AND processed_at=0
UNION ALL SELECT 1 FROM raw_archive_index WHERE tenant_id=$1 AND product_id=$2 AND parse_attempted_at=0
UNION ALL SELECT 1 FROM health_inspection_job WHERE tenant_id=$1 AND lower(status) IN ('running','pending','processing','queued')
UNION ALL SELECT 1 FROM replay_task WHERE tenant_id=$1 AND lower(status) IN ('running','pending','processing','queued') AND (COALESCE(body->>'productId','')=$2 OR COALESCE(body->>'deviceId','') IN (SELECT id FROM device_registry WHERE tenant_id=$1 AND product_id=$2) OR (COALESCE(body->>'productId','')='' AND COALESCE(body->>'deviceId','')=''))
UNION ALL SELECT 1 FROM alarm_analysis_job j JOIN alarm_record a ON a.tenant_id=j.tenant_id AND a.id=j.alarm_id JOIN device_registry d ON d.tenant_id=a.tenant_id AND d.id=a.device_id WHERE d.tenant_id=$1 AND d.product_id=$2 AND lower(j.status) IN ('running','pending','processing','queued'))`

func capacityFixture(ctx context.Context, db capacityQuerier, tenant string, body []byte, preview bool) (model.CapacityFixtureProduct, error) {
	var p model.Product
	if err := json.Unmarshal(body, &p); err != nil {
		return model.CapacityFixtureProduct{}, err
	}
	v := model.CapacityFixtureProduct{ProductID: p.ID, Name: p.Name, Source: model.CapacityFixtureSource(p)}
	if v.Source == "" {
		return v, model.ErrResourceInUse
	}
	var membership string
	var invalid int64
	membershipSQL := "''"
	if preview {
		membershipSQL = `md5(COALESCE(string_agg(encode(convert_to(id,'UTF8'),'hex')||':'||updated_at::text,E'\n' ORDER BY id),''))`
	}
	err := db.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE NOT COALESCE((`+capacityFixtureDeviceSQL(v.Source)+`),false)),`+membershipSQL+` FROM device_registry WHERE tenant_id=$1 AND product_id=$2`, tenant, p.ID).Scan(&v.DeviceCount, &invalid, &membership)
	if err != nil {
		return v, err
	}
	if preview {
		h := sha256.New()
		_, _ = h.Write(body)
		_, _ = h.Write([]byte("\x00" + membership))
		v.Fingerprint = hex.EncodeToString(h.Sum(nil))
		if err = db.QueryRow(ctx, `SELECT count(*) FROM raw_archive_index WHERE tenant_id=$1 AND product_id=$2`, tenant, p.ID).Scan(&v.RawMessages); err != nil {
			return v, err
		}
	}
	if invalid > 0 {
		v.BlockedReason = "产品包含已改作其他用途的设备"
	}
	var used bool
	if err = db.QueryRow(ctx, capacityFixtureBlockedSQL, tenant, p.ID, strings.HasPrefix(v.Source, "legacy-cap")).Scan(&used); err != nil {
		return v, err
	}
	if used {
		v.BlockedReason = "设备、产品配置或后台任务仍有有效引用"
	}
	if v.Source == "legacy-cap-gb26875" {
		id, _, ok := strings.Cut(p.ProtocolPackageID, "@")
		if !ok {
			v.BlockedReason = "历史协议包缺少可核对的独立版本"
			return v, nil
		}
		var private bool
		// The artifact directory belongs to the protocol, not one version. Any
		// additional version or alias is an unknown consumer and must retain it.
		err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM protocol_package WHERE tenant_id=$1 AND id=$2 AND body->>'protocol'=$4 AND parser_type IN ('gb26875','go_protocol_parser')) AND NOT EXISTS(SELECT 1 FROM iot_product WHERE tenant_id=$1 AND id<>$3 AND starts_with(protocol_package_id,$4||'@') UNION ALL SELECT 1 FROM product_protocol_binding WHERE tenant_id=$1 AND product_id<>$3 AND (protocol_id=$4 OR body->>'previousProtocolId'=$4) UNION ALL SELECT 1 FROM device_access_profile WHERE tenant_id=$1 AND product_id<>$3 AND body->>'protocolId'=$4 UNION ALL SELECT 1 FROM protocol_package WHERE tenant_id=$1 AND id<>$2 AND (body->>'protocol'=$4 OR starts_with(id,$4||'@')))`, tenant, p.ProtocolPackageID, p.ID, id).Scan(&private)
		if err != nil {
			return v, err
		}
		if !private {
			v.BlockedReason = "历史协议包不存在或被其他配置共享"
		} else {
			v.ProtocolID = id
		}
	}
	return v, nil
}

func (r *Repository) ListCapacityFixtureProducts(ctx context.Context, tenant, after string, limit int) ([]model.CapacityFixtureProduct, error) {
	rows, err := r.pool.Query(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id>$2 AND `+capacityProductCandidatesSQL+` ORDER BY id LIMIT $3`, tenant, after, max(1, min(limit, 100)))
	if err != nil {
		return nil, err
	}
	var bodies [][]byte
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			rows.Close()
			return nil, err
		}
		bodies = append(bodies, body)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]model.CapacityFixtureProduct, 0, len(bodies))
	for _, body := range bodies {
		v, err := capacityFixture(ctx, r.pool, tenant, body, true)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *Repository) ListCapacityFixtureDevices(ctx context.Context, tenant, product, after string, limit int) ([]string, error) {
	p, err := r.GetProduct(ctx, tenant, product)
	if err != nil {
		return nil, err
	}
	source := model.CapacityFixtureSource(p)
	if source == "" {
		return nil, model.ErrResourceInUse
	}
	rows, err := r.pool.Query(ctx, `SELECT id FROM device_registry WHERE tenant_id=$1 AND product_id=$2 AND id>$3 AND `+capacityFixtureDeviceSQL(source)+` ORDER BY id LIMIT $4`, tenant, product, after, max(1, min(limit, 1000)))
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

func (r *Repository) PrepareCapacityFixture(ctx context.Context, tenant, product, fingerprint string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var body []byte
	if err = tx.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, product).Scan(&body); errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	} else if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM device_registry WHERE tenant_id=$1 AND product_id=$2 ORDER BY id FOR UPDATE`, tenant, product)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	v, err := capacityFixture(ctx, tx, tenant, body, true)
	if err != nil {
		return err
	}
	if v.BlockedReason != "" || fingerprint == "" || fingerprint != v.Fingerprint {
		return model.ErrResourceInUse
	}
	_, err = tx.Exec(ctx, `UPDATE iot_product SET status='DISABLED',body=jsonb_set(jsonb_set(body,'{status}','"DISABLED"'::jsonb),'{updatedAt}',to_jsonb((extract(epoch FROM now())*1000)::bigint)),updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, product)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func checkCapacityHistorical(ctx context.Context, db capacityQuerier, tenant string, q model.CapacityCleanupBatch) error {
	for _, id := range q.RemoveDevices {
		if !slices.Contains(q.Devices, id) {
			return model.ErrResourceInUse
		}
	}
	var outside bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM device_registry WHERE tenant_id=$1 AND id=ANY($2) AND product_id<>$3)`, tenant, q.RemoveDevices, q.Product).Scan(&outside); err != nil {
		return err
	}
	if outside {
		return model.ErrResourceInUse
	}
	if !q.Historical {
		return nil
	}
	var body []byte
	if err := db.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2 AND upper(status)='DISABLED'`, tenant, q.Product).Scan(&body); errors.Is(err, pgx.ErrNoRows) {
		return model.ErrResourceInUse
	} else if err != nil {
		return err
	}
	v, err := capacityFixture(ctx, db, tenant, body, false)
	if err != nil {
		return err
	}
	if v.BlockedReason != "" {
		return model.ErrResourceInUse
	}
	return nil
}
