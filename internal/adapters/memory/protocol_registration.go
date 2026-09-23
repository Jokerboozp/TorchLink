package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) RegisterProtocolDevice(ctx context.Context, expected model.DeviceAccessProfile, id, name string) (model.ManagedDevice, bool, error) { /* 定义 RegisterProtocolDevice 函数。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.Lock()                                                                                                                                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                                                                                                                   /* 安排函数结束时执行清理。 */
	current := r.accessProfiles[key(expected.TenantID, expected.ID)]                                                                                      /* 更新 current 的值。 */
	d, err := model.ProtocolRegistrationDevice(expected, current, r.products[key(current.TenantID, current.ProductID)], id, name, time.Now().UnixMilli()) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                       /* 判断条件并选择处理分支。 */
		return d, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if current.EdgeNodeID != "" { /* 判断条件并选择处理分支。 */
		return d, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if existing, ok := r.devices[key(current.TenantID, id)]; ok { /* 判断条件并选择处理分支。 */
		return cloneManaged(existing), false, model.RegisteredProtocolDevice(existing, current) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, existing := range r.devices { /* 循环处理当前数据。 */
		if existing.AccessKey == d.AccessKey { /* 判断条件并选择处理分支。 */
			return model.ManagedDevice{}, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	r.devices[key(d.TenantID, d.ID)] = cloneManaged(d) /* 执行当前语句并推进处理流程。 */
	return cloneManaged(d), true, nil                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
