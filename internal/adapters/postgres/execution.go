package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) AcquireExecutionLease(ctx context.Context, tenant, resource, owner, endpoint string, ttl time.Duration) (model.ExecutionLease, bool, error) { /* 定义 AcquireExecutionLease 函数。 */
	var v model.ExecutionLease                                                                   /* 声明 v。 */
	if tenant == "" || resource == "" || owner == "" || ttl < time.Second || ttl > time.Minute { /* 判断条件并选择处理分支。 */
		return v, false, errors.New("invalid execution lease") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	err := r.pool.QueryRow(ctx, `INSERT INTO execution_lease(tenant_id,resource,owner,endpoint,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+$5*interval '1 millisecond') ON CONFLICT(tenant_id,resource) DO UPDATE SET owner=excluded.owner,endpoint=excluded.endpoint,expires_at=excluded.expires_at,token=CASE WHEN execution_lease.expires_at<=clock_timestamp() THEN execution_lease.token+1 ELSE execution_lease.token END WHERE execution_lease.owner=excluded.owner OR execution_lease.expires_at<=clock_timestamp() RETURNING tenant_id,resource,owner,endpoint,token,(extract(epoch FROM expires_at)*1000)::bigint`, tenant, resource, owner, endpoint, ttl.Milliseconds()).Scan(&v.TenantID, &v.Resource, &v.Owner, &v.Endpoint, &v.Token, &v.ExpiresAt) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		return v, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, err == nil, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetExecutionLease(ctx context.Context, tenant, resource string) (model.ExecutionLease, error) { /* 定义 GetExecutionLease 函数。 */
	var v model.ExecutionLease                                                                                                                                                                                                                                                                                           /* 声明 v。 */
	err := r.pool.QueryRow(ctx, `SELECT tenant_id,resource,owner,endpoint,token,(extract(epoch FROM expires_at)*1000)::bigint FROM execution_lease WHERE tenant_id=$1 AND resource=$2 AND expires_at>clock_timestamp()`, tenant, resource).Scan(&v.TenantID, &v.Resource, &v.Owner, &v.Endpoint, &v.Token, &v.ExpiresAt) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                                                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		err = ErrNotFound /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ReleaseExecutionLease(ctx context.Context, v model.ExecutionLease) error { /* 定义 ReleaseExecutionLease 函数。 */
	_, err := r.pool.Exec(ctx, `UPDATE execution_lease SET expires_at=to_timestamp(0) WHERE tenant_id=$1 AND resource=$2 AND owner=$3 AND token=$4`, v.TenantID, v.Resource, v.Owner, v.Token) /* 更新 err 的值。 */
	return err                                                                                                                                                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
