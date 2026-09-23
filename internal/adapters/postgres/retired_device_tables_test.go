package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"github.com/jackc/pgx/v5" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func verifyRetiredDeviceTablesUntouched(t *testing.T, r *Repository) { /* 定义 verifyRetiredDeviceTablesUntouched 函数。 */
	t.Helper()                                                                                        /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                                                                       /* 更新 ctx 的值。 */
	for _, table := range []string{"device_shadow", "device_shadow_change", "device_twin_topology"} { /* 循环处理当前数据。 */
		var count int                                                                                                                                                                          /* 声明 count。 */
		if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&count); err != nil || count != 0 { /* 判断条件并选择处理分支。 */
			t.Fatalf("fresh schema creates retired table %s: %d %v", table, count, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		// An arbitrary old schema must remain untouched; no runtime reads it anymore.
		ident := pgx.Identifier{table}.Sanitize()                                                                        /* 更新 ident 的值。 */
		if _, err := r.pool.Exec(ctx, "CREATE TABLE "+ident+" (id text PRIMARY KEY, body jsonb NOT NULL)"); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := r.pool.Exec(ctx, "INSERT INTO "+ident+" VALUES ('retained', '{\"version\":7}')"); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := r.Migrate(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, table := range []string{"device_shadow", "device_shadow_change", "device_twin_topology"} { /* 循环处理当前数据。 */
		var version int                                                                                                                                                             /* 声明 version。 */
		if err := r.pool.QueryRow(ctx, "SELECT (body->>'version')::int FROM "+pgx.Identifier{table}.Sanitize()+" WHERE id='retained'").Scan(&version); err != nil || version != 7 { /* 判断条件并选择处理分支。 */
			t.Fatalf("retired data changed in %s: %d %v", table, version, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
