package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) RegisterProtocolDevice(ctx context.Context, expected model.DeviceAccessProfile, id, name string) (model.ManagedDevice, bool, error) { /* 定义 RegisterProtocolDevice 函数。 */
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
	if err = tx.QueryRow(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=$2 FOR SHARE`, current.TenantID, current.ProductID).Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var product model.Product                             /* 声明 product。 */
	if err = json.Unmarshal(body, &product); err != nil { /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	d, err := model.ProtocolRegistrationDevice(expected, current, product, id, name, time.Now().UnixMilli()) /* 更新 err 的值。 */
	if err != nil {                                                                                          /* 判断条件并选择处理分支。 */
		return empty, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if current.EdgeNodeID != "" { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
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
		d, err = r.scanManagedDevice(tx.QueryRow(ctx, `SELECT body,secret_hash FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR SHARE`, d.TenantID, d.ID)) /* 更新 err 的值。 */
		if err != nil {                                                                                                                                         /* 判断条件并选择处理分支。 */
			return empty, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = model.RegisteredProtocolDevice(d, current); err != nil { /* 判断条件并选择处理分支。 */
			return empty, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return d, created, tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
