package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Configure before Start. Counts include every listener for a device: closing
// one of several sessions must not project a false disconnect.
func (r *Listeners) SetConnectionReporter(f func(context.Context, string, string, string, bool, int64) error) { /* 定义 SetConnectionReporter 函数。 */
	r.connectionReporter = f /* 更新 r.connectionReporter 的值。 */
} /* 结束当前表达式或代码块。 */
func (r *Listeners) reportConnection(p model.DeviceAccessProfile, device string, connected bool) { /* 定义 reportConnection 函数。 */
	r.connectionMu.Lock()          /* 执行当前语句并推进处理流程。 */
	defer r.connectionMu.Unlock()  /* 安排函数结束时执行清理。 */
	if r.connectionCounts == nil { /* 判断条件并选择处理分支。 */
		r.connectionCounts = map[string]int{} /* 更新 r.connectionCounts 的值。 */
	} /* 结束当前表达式或代码块。 */
	key := p.TenantID + "\x00" + device /* 更新 key 的值。 */
	n := r.connectionCounts[key]        /* 更新 n 的值。 */
	if connected {                      /* 判断条件并选择处理分支。 */
		r.connectionCounts[key] = n + 1 /* 更新 r.connectionCounts[key] 的值。 */
		if n > 0 {                      /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		if n <= 0 { /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if n > 1 { /* 判断条件并选择处理分支。 */
			r.connectionCounts[key] = n - 1 /* 更新 r.connectionCounts[key] 的值。 */
			return                          /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		delete(r.connectionCounts, key) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if r.connectionReporter != nil { /* 判断条件并选择处理分支。 */
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)                                                           /* 更新 cancel 的值。 */
		defer cancel()                                                                                                                    /* 安排函数结束时执行清理。 */
		if e := r.connectionReporter(ctx, p.TenantID, p.ProductID, device, connected, time.Now().UnixMilli()); e != nil && r.log != nil { /* 判断条件并选择处理分支。 */
			r.log.Warn("listener connection projection failed", "device", device, "error", e) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
