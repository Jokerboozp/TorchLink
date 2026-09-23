package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type rawReservation struct { /* 定义 rawReservation 类型。 */
	Metadata model.RawMessage /* 执行当前语句并推进处理流程。 */
	Hash     string           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) ReserveRawMessage(_ context.Context, v model.RawMessage) (model.RawMessage, error) { /* 定义 ReserveRawMessage 函数。 */
	r.mu.Lock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()           /* 安排函数结束时执行清理。 */
	if r.rawReservations == nil { /* 判断条件并选择处理分支。 */
		r.rawReservations = map[string]rawReservation{} /* 更新 r.rawReservations 的值。 */
	} /* 结束当前表达式或代码块。 */
	k := key(v.TenantID, v.MessageID)        /* 更新 k 的值。 */
	if old, ok := r.rawReservations[k]; ok { /* 判断条件并选择处理分支。 */
		if old.Hash != v.PayloadHash() || old.Metadata.ProductID != v.ProductID || old.Metadata.DeviceID != v.DeviceID { /* 判断条件并选择处理分支。 */
			return v, model.ErrRawConflict /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		canonical := clone(old.Metadata)                                /* 更新 canonical 的值。 */
		canonical.Payload = append(canonical.Payload[:0], v.Payload...) /* 更新 canonical.Payload 的值。 */
		return canonical, nil                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	metadata := clone(v)                                                             /* 更新 metadata 的值。 */
	metadata.Payload = nil                                                           /* 更新 metadata.Payload 的值。 */
	r.rawReservations[k] = rawReservation{Metadata: metadata, Hash: v.PayloadHash()} /* 更新 r.rawReservations[k] 的值。 */
	return v, nil                                                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
