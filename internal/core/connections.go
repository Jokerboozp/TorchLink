package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (e *Engine) ReportConnection(ctx context.Context, tenant, product, device string, connected bool, at int64) error { /* 定义 ReportConnection 函数。 */
	unlock := e.lockDeviceState(tenant, device)              /* 更新 unlock 的值。 */
	defer unlock()                                           /* 安排函数结束时执行清理。 */
	state, err := e.Repo.GetDeviceState(ctx, tenant, device) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		state = model.DeviceState{TenantID: tenant, ProductID: product, DeviceID: device, DataStatus: "UNKNOWN", BusinessStatus: "UNKNOWN", ReportIntervalSec: 300, OfflineToleranceSec: 60} /* 更新 state 的值。 */
	} /* 结束当前表达式或代码块。 */
	state.ConnectionStatus = "DISCONNECTED" /* 更新 state.ConnectionStatus 的值。 */
	if !connected {                         /* 判断条件并选择处理分支。 */
		state.LastDisconnectAt = at /* 更新 state.LastDisconnectAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if connected { /* 判断条件并选择处理分支。 */
		state.ConnectionStatus = "CONNECTED" /* 更新 state.ConnectionStatus 的值。 */
		state.LastConnectAt = at             /* 更新 state.LastConnectAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	state.StatusSource = "LISTENER_SESSION" /* 更新 state.StatusSource 的值。 */
	return e.updateDeviceState(ctx, state)  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Bound the lock set without retaining an entry for every device ever seen.
func (e *Engine) lockDeviceState(tenant, device string) func() { /* 定义 lockDeviceState 函数。 */
	var h uint32 = 2166136261                            /* 声明 h。 */
	for _, b := range []byte(tenant + "\x00" + device) { /* 循环处理当前数据。 */
		h = (h ^ uint32(b)) * 16777619 /* 更新 h 的值。 */
	} /* 结束当前表达式或代码块。 */
	m := &e.stateLocks[h%uint32(len(e.stateLocks))] /* 更新 m 的值。 */
	m.Lock()                                        /* 执行当前语句并推进处理流程。 */
	return m.Unlock                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
