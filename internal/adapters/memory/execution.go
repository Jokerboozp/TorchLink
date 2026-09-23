package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) AcquireExecutionLease(ctx context.Context, tenant, resource, owner, endpoint string, ttl time.Duration) (model.ExecutionLease, bool, error) { /* 定义 AcquireExecutionLease 函数。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return model.ExecutionLease{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if tenant == "" || resource == "" || owner == "" || ttl < time.Second || ttl > time.Minute { /* 判断条件并选择处理分支。 */
		return model.ExecutionLease{}, false, errors.New("invalid execution lease") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.Lock()          /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()  /* 安排函数结束时执行清理。 */
	if r.leases == nil { /* 判断条件并选择处理分支。 */
		r.leases = map[string]model.ExecutionLease{} /* 更新 r.leases 的值。 */
	} /* 结束当前表达式或代码块。 */
	k := key(tenant, resource)                           /* 更新 k 的值。 */
	v, exists := r.leases[k]                             /* 更新 exists 的值。 */
	now := time.Now().UnixMilli()                        /* 更新 now 的值。 */
	if exists && v.ExpiresAt > now && v.Owner != owner { /* 判断条件并选择处理分支。 */
		return v, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !exists { /* 判断条件并选择处理分支。 */
		v = model.ExecutionLease{TenantID: tenant, Resource: resource, Token: 1} /* 更新 v 的值。 */
	} else if v.ExpiresAt <= now { /* 结束当前表达式或代码块。 */
		v.Token++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	v.Owner, v.Endpoint, v.ExpiresAt = owner, endpoint, now+ttl.Milliseconds() /* 更新 v.ExpiresAt 的值。 */
	r.leases[k] = v                                                            /* 更新 r.leases[k] 的值。 */
	return v, true, nil                                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetExecutionLease(_ context.Context, tenant, resource string) (model.ExecutionLease, error) { /* 定义 GetExecutionLease 函数。 */
	r.mu.RLock()                                      /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                              /* 安排函数结束时执行清理。 */
	v, ok := r.leases[key(tenant, resource)]          /* 更新 ok 的值。 */
	if !ok || v.ExpiresAt <= time.Now().UnixMilli() { /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ReleaseExecutionLease(_ context.Context, lease model.ExecutionLease) error { /* 定义 ReleaseExecutionLease 函数。 */
	r.mu.Lock()                                                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                         /* 安排函数结束时执行清理。 */
	k := key(lease.TenantID, lease.Resource)                    /* 更新 k 的值。 */
	v, ok := r.leases[k]                                        /* 更新 ok 的值。 */
	if ok && v.Owner == lease.Owner && v.Token == lease.Token { /* 判断条件并选择处理分支。 */
		v.ExpiresAt = 0 /* 更新 v.ExpiresAt 的值。 */
		r.leases[k] = v /* 更新 r.leases[k] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
