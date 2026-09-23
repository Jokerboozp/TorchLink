package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"os"      /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"github.com/jackc/pgx/v5"         /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5/pgxpool" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Set IOT_TEST_POSTGRES_DSN to run the migration against a disposable PostgreSQL database.
func TestMigrateLegacyAIAnalysisTenantOwnership(t *testing.T) { /* 定义 TestMigrateLegacyAIAnalysisTenantOwnership 函数。 */
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN") /* 更新 dsn 的值。 */
	if dsn == "" {                            /* 判断条件并选择处理分支。 */
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx := context.Background()         /* 更新 ctx 的值。 */
	admin, err := pgxpool.New(ctx, dsn) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer admin.Close() /* 安排函数结束时执行清理。 */

	schemaName := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())  /* 更新 schemaName 的值。 */
	identifier := pgx.Identifier{schemaName}.Sanitize()                    /* 更新 identifier 的值。 */
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE") }() /* 安排函数结束时执行清理。 */

	config, err := pgxpool.ParseConfig(dsn) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	config.ConnConfig.RuntimeParams["search_path"] = schemaName /* 执行当前语句并推进处理流程。 */
	pool, err := pgxpool.NewWithConfig(ctx, config)             /* 更新 err 的值。 */
	if err != nil {                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer pool.Close() /* 安排函数结束时执行清理。 */

	legacy := `
CREATE TABLE alarm_record (
  tenant_id text NOT NULL, id text NOT NULL, rule_id text NOT NULL, device_id text NOT NULL,
  status text NOT NULL, level text NOT NULL, source text NOT NULL,
  last_triggered_at bigint NOT NULL, body jsonb NOT NULL,
  PRIMARY KEY (tenant_id, id)
);
CREATE TABLE alarm_ai_analysis (
  alarm_id text PRIMARY KEY, body jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body) VALUES
  ('tenant_a','alarm_a','rule','device_a','ACTIVE','HIGH','iot',1,'{}'),
  ('tenant_b','alarm_b','rule','device_b','ACTIVE','HIGH','iot',1,'{}'),
  ('tenant_a','alarm_ambiguous','rule','device_c','ACTIVE','HIGH','iot',1,'{}'),
  ('tenant_b','alarm_ambiguous','rule','device_d','ACTIVE','HIGH','iot',1,'{}');
INSERT INTO alarm_ai_analysis(alarm_id,body) VALUES
  ('alarm_a','{"alarmId":"alarm_a"}'),
  ('alarm_b','{"alarmId":"alarm_b"}'),
  ('alarm_ambiguous','{"alarmId":"alarm_ambiguous"}'),
  ('alarm_orphan','{"alarmId":"alarm_orphan"}');`
	if _, err = pool.Exec(ctx, legacy); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = (&Repository{pool: pool}).Migrate(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	for alarmID, wantTenant := range map[string]string{ /* 循环处理当前数据。 */
		"alarm_a":         "tenant_a",            /* 执行当前语句并推进处理流程。 */
		"alarm_b":         "tenant_b",            /* 执行当前语句并推进处理流程。 */
		"alarm_ambiguous": "__legacy_orphaned__", /* 执行当前语句并推进处理流程。 */
		"alarm_orphan":    "__legacy_orphaned__", /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		var got string                                                                                                             /* 声明 got。 */
		if err = pool.QueryRow(ctx, `SELECT tenant_id FROM alarm_ai_analysis WHERE alarm_id=$1`, alarmID).Scan(&got); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if got != wantTenant { /* 判断条件并选择处理分支。 */
			t.Fatalf("alarm %s migrated to tenant %q, want %q", alarmID, got, wantTenant) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = pool.Exec(ctx, `INSERT INTO alarm_ai_analysis(tenant_id,alarm_id,body) VALUES('tenant_b','alarm_a','{}')`); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("composite tenant/alarm primary key was not installed: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
