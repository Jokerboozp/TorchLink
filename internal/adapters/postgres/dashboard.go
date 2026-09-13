package postgres

import (
	"context"
	"iot-platform/internal/model"
)

// One statement gives a consistent snapshot and keeps full records in storage.
func (r *Repository) DashboardCounts(ctx context.Context, tenant string, start, end int64) ([]model.DashboardCount, error) {
	rows, err := r.pool.Query(ctx, `
 WITH registered AS (
  SELECT d.product_id, CASE WHEN s.business_status='ALARM' THEN 'ONLINE' ELSE coalesce(nullif(s.business_status,''),'NEVER_SEEN') END AS state
  FROM device_registry d LEFT JOIN device_state s ON s.tenant_id=d.tenant_id AND s.device_id=d.id
  WHERE d.tenant_id=$1
 )
 SELECT 'state', state, '', count(*) FROM registered GROUP BY state
 UNION ALL
 SELECT 'product', d.product_id, coalesce(nullif(p.body->>'name',''),d.product_id), count(*)
 FROM registered d LEFT JOIN iot_product p ON p.tenant_id=$1 AND p.id=d.product_id
 GROUP BY d.product_id, p.body->>'name'
 UNION ALL
 SELECT 'level', level, '', count(*) FROM alarm_record WHERE tenant_id=$1 AND status='ACTIVE' GROUP BY level
 UNION ALL
 SELECT 'day', (((body->>'firstTriggeredAt')::bigint-$2::bigint)/86400000)::text, '', count(*)
 FROM alarm_record WHERE tenant_id=$1 AND (body->>'firstTriggeredAt')::bigint BETWEEN $2::bigint AND $3::bigint
 GROUP BY 2`, tenant, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DashboardCount{}
	for rows.Next() {
		var v model.DashboardCount
		if err := rows.Scan(&v.Kind, &v.Key, &v.Name, &v.Count); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
