package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) ListManagedDeviceChildren(ctx context.Context, tenant, parent string, limit, offset int) ([]model.ManagedDevice, int, error) { /* 定义 ListManagedDeviceChildren 函数。 */
	var total int                                                                                                                                                  /* 声明 total。 */
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=$2`, tenant, parent).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND body->>'gatewayId'=$2 ORDER BY id LIMIT $3 OFFSET $4`, tenant, parent, max(1, min(limit, 100)), max(0, offset)) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()               /* 安排函数结束时执行清理。 */
	items := []model.ManagedDevice{} /* 更新 items 的值。 */
	for rows.Next() {                /* 循环处理当前数据。 */
		d, e := r.scanManagedDevice(rows) /* 更新 e 的值。 */
		if e != nil {                     /* 判断条件并选择处理分支。 */
			return nil, 0, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, d) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) RegisterProtocolChild(ctx context.Context, expected model.DeviceAccessProfile, parentID string, identity model.ChildIdentity) (model.ManagedDevice, bool, error) { /* 定义 RegisterProtocolChild 函数。 */
	var empty model.ManagedDevice /* 声明 empty。 */
	tx, err := r.pool.Begin(ctx)  /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                                                                                                               /* 安排函数结束时执行清理。 */
	var body []byte                                                                                                                                                      /* 声明 body。 */
	if err = tx.QueryRow(ctx, `SELECT body FROM device_access_profile WHERE tenant_id=$1 AND id=$2 FOR SHARE`, expected.TenantID, expected.ID).Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var current model.DeviceAccessProfile                 /* 声明 current。 */
	if err = json.Unmarshal(body, &current); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parent, err := r.scanManagedDevice(tx.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, expected.TenantID, parentID)) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                           /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var parentStatus string                                                                                                                                                   /* 声明 parentStatus。 */
	if err = tx.QueryRow(ctx, `SELECT status FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR SHARE`, expected.TenantID, parent.ProductID).Scan(&parentStatus); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if parentStatus != "ENABLED" { /* 判断条件并选择处理分支。 */
		return empty, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var productID string                      /* 声明 productID。 */
	for _, v := range current.ChildProducts { /* 循环处理当前数据。 */
		if v.Type == identity.Type { /* 判断条件并选择处理分支。 */
			productID = v.ProductID /* 更新 productID 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err = tx.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR SHARE`, expected.TenantID, productID).Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var product model.Product                             /* 声明 product。 */
	if err = json.Unmarshal(body, &product); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	d, err := model.ProtocolChildDevice(expected, current, parent, product, identity, time.Now().UnixMilli()) /* 更新 err 的值。 */
	if err != nil {                                                                                           /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var status string                                                                                                                                                                                                                                                                                      /* 声明 status。 */
	if err = tx.QueryRow(ctx, `SELECT r.status FROM product_protocol_binding b JOIN protocol_release r ON r.tenant_id=b.tenant_id AND r.protocol_id=b.protocol_id AND r.version=b.version WHERE b.tenant_id=$1 AND b.product_id=$2 FOR SHARE OF b,r`, d.TenantID, d.ProductID).Scan(&status); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if status != "PUBLISHED" { /* 判断条件并选择处理分支。 */
		return empty, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	body, err = json.Marshal(d) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err := tx.Exec(ctx, `INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES($1,$2,$3,$4,$5,'',$6) ON CONFLICT(tenant_id,id) DO NOTHING`, d.TenantID, d.ID, d.ProductID, d.Status, d.AccessKey, body) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	created := result.RowsAffected() == 1 /* 更新 created 的值。 */
	if !created {                         /* 判断条件并选择处理分支。 */
		old, e := r.scanManagedDevice(tx.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, d.TenantID, d.ID)) /* 更新 e 的值。 */
		if e != nil {                                                                                                                                             /* 判断条件并选择处理分支。 */
			return empty, false, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if e = model.ExistingProtocolChild(old, d); e != nil { /* 判断条件并选择处理分支。 */
			return empty, false, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		old.UpdatedAt = d.UpdatedAt /* 更新 old.UpdatedAt 的值。 */
		if identity.Name != "" {    /* 判断条件并选择处理分支。 */
			old.Name = identity.Name /* 更新 old.Name 的值。 */
		} /* 结束当前表达式或代码块。 */
		d = old                                                                                                                           /* 更新 d 的值。 */
		body, _ = json.Marshal(d)                                                                                                         /* 更新 _ 的值。 */
		if _, err = tx.Exec(ctx, `UPDATE device_registry SET body=$3 WHERE tenant_id=$1 AND id=$2`, d.TenantID, d.ID, body); err != nil { /* 判断条件并选择处理分支。 */
			return empty, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	parent.DeviceRole = "GATEWAY"                                                                                                               /* 更新 parent.DeviceRole 的值。 */
	body, _ = json.Marshal(parent)                                                                                                              /* 更新 _ 的值。 */
	if _, err = tx.Exec(ctx, `UPDATE device_registry SET body=$3 WHERE tenant_id=$1 AND id=$2`, parent.TenantID, parent.ID, body); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return d, created, tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
