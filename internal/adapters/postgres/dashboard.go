package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// One statement gives a consistent snapshot and keeps full records in storage.
func (r *Repository) DashboardCounts(ctx context.Context, tenant string, start, end int64) ([]model.DashboardCount, error) { /* 定义 DashboardCounts 函数。 */
	return r.dashboardCounts(ctx, tenant, start, end, false, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) DashboardCountsForDevices(ctx context.Context, tenant string, start, end int64, ids []string) ([]model.DashboardCount, error) { /* 定义 DashboardCountsForDevices 函数。 */
	return r.dashboardCounts(ctx, tenant, start, end, true, ids) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) dashboardCounts(ctx context.Context, tenant string, start, end int64, scoped bool, ids []string) ([]model.DashboardCount, error) { /* 定义 dashboardCounts 函数。 */
	rows, err := r.pool.Query(ctx, `
 WITH registered AS (
  SELECT d.product_id, CASE WHEN s.business_status='ALARM' THEN 'ONLINE' ELSE coalesce(nullif(s.business_status,''),'NEVER_SEEN') END AS state
  FROM device_registry d LEFT JOIN device_state s ON s.tenant_id=d.tenant_id AND s.device_id=d.id
  WHERE d.tenant_id=$1 AND (NOT $4::boolean OR d.id=ANY($5::text[]))
 )
 SELECT 'state', state, '', count(*) FROM registered GROUP BY state
 UNION ALL
 SELECT 'product', d.product_id, coalesce(nullif(p.body->>'name',''),d.product_id), count(*)
 FROM registered d LEFT JOIN iot_product p ON p.tenant_id=$1 AND p.id=d.product_id
 GROUP BY d.product_id, p.body->>'name'
 UNION ALL
 SELECT 'level', level, '', count(*) FROM alarm_record WHERE tenant_id=$1 AND status='ACTIVE' AND (NOT $4::boolean OR device_id=ANY($5::text[])) GROUP BY level
 UNION ALL
 SELECT 'day', (((body->>'firstTriggeredAt')::bigint-$2::bigint)/86400000)::text, '', count(*)
 FROM alarm_record WHERE tenant_id=$1 AND (body->>'firstTriggeredAt')::bigint BETWEEN $2::bigint AND $3::bigint AND (NOT $4::boolean OR device_id=ANY($5::text[]))
 GROUP BY 2`, tenant, start, end, scoped, ids)
	if err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()              /* 安排函数结束时执行清理。 */
	out := []model.DashboardCount{} /* 更新 out 的值。 */
	for rows.Next() {               /* 循环处理当前数据。 */
		var v model.DashboardCount                                            /* 声明 v。 */
		if err := rows.Scan(&v.Kind, &v.Key, &v.Name, &v.Count); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
