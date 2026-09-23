package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// The reservation contains only immutable routing/parser metadata, not a second
// payload copy. It prevents conflicting writers before either tier archives data.
func (r *Repository) ReserveRawMessage(ctx context.Context, v model.RawMessage) (model.RawMessage, error) { /* 定义 ReserveRawMessage 函数。 */
	metadata := v                       /* 更新 metadata 的值。 */
	metadata.Payload = nil              /* 更新 metadata.Payload 的值。 */
	body, err := json.Marshal(metadata) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		return v, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tag, err := r.pool.Exec(ctx, `INSERT INTO raw_ingest_reservation(tenant_id,message_id,payload_hash,metadata) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, v.TenantID, v.MessageID, v.PayloadHash(), body) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                           /* 判断条件并选择处理分支。 */
		return v, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if tag.RowsAffected() == 1 { /* 判断条件并选择处理分支。 */
		return v, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var hash string                                                                                                                                                                          /* 声明 hash。 */
	if err = r.pool.QueryRow(ctx, `SELECT payload_hash,metadata FROM raw_ingest_reservation WHERE tenant_id=$1 AND message_id=$2`, v.TenantID, v.MessageID).Scan(&hash, &body); err != nil { /* 判断条件并选择处理分支。 */
		return v, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(body, &metadata); err != nil { /* 判断条件并选择处理分支。 */
		return v, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if hash != v.PayloadHash() || metadata.ProductID != v.ProductID || metadata.DeviceID != v.DeviceID { /* 判断条件并选择处理分支。 */
		return v, model.ErrRawConflict /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	metadata.Payload = v.Payload /* 更新 metadata.Payload 的值。 */
	return metadata, nil         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
