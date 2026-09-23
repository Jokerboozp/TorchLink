package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) LoadAccessState(ctx context.Context, tenant string) (model.AccessState, error) { /* 定义 LoadAccessState 函数。 */
	var state model.AccessState                                                                                                      /* 声明 state。 */
	var body []byte                                                                                                                  /* 声明 body。 */
	err := r.pool.QueryRow(ctx, `SELECT body,revision FROM platform_access WHERE tenant_id=$1`, tenant).Scan(&body, &state.Revision) /* 更新 err 的值。 */
	if errors.Is(err, pgx.ErrNoRows) {                                                                                               /* 判断条件并选择处理分支。 */
		return state, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return state, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	revision := state.Revision         /* 更新 revision 的值。 */
	err = json.Unmarshal(body, &state) /* 更新 err 的值。 */
	state.Revision = revision          /* 更新 state.Revision 的值。 */
	return state, err                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAccessState(ctx context.Context, tenant string, state model.AccessState) (bool, error) { /* 定义 SaveAccessState 函数。 */
	body, err := json.Marshal(state) /* 更新 err 的值。 */
	if err != nil {                  /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err := r.pool.Exec(ctx, `INSERT INTO platform_access(tenant_id,revision,body) SELECT $1,1,$2::jsonb WHERE $3::bigint=0 ON CONFLICT(tenant_id) DO NOTHING`, tenant, body, state.Revision) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if result.RowsAffected() == 1 { /* 判断条件并选择处理分支。 */
		return true, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err = r.pool.Exec(ctx, `UPDATE platform_access SET revision=revision+1,body=$2::jsonb WHERE tenant_id=$1 AND revision=$3`, tenant, body, state.Revision) /* 更新 err 的值。 */
	return result.RowsAffected() == 1, err                                                                                                                           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
