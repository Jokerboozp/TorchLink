package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"os"      /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"

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
	var legacyScope string
	if err = pool.QueryRow(ctx, `SELECT knowledge_scope FROM alarm_ai_analysis WHERE tenant_id='tenant_a' AND alarm_id='alarm_a'`).Scan(&legacyScope); err != nil {
		t.Fatal(err)
	}
	if legacyScope != "legacy-tenant-knowledge" {
		t.Fatalf("pre-scope analysis must stay restricted as tenant knowledge, got scope %q", legacyScope)
	}
	// The knowledge-free variant of the same alarm is stored beside the restricted one.
	if _, err = pool.Exec(ctx, `INSERT INTO alarm_ai_analysis(tenant_id,alarm_id,knowledge_scope,body) VALUES('tenant_a','alarm_a','','{}')`); err != nil {
		t.Fatalf("knowledge scope was not added to the analysis primary key: %v", err)
	}
	repo := &Repository{pool: pool}
	if err = repo.Migrate(ctx); err != nil {
		t.Fatalf("analysis scope migration must be repeatable: %v", err)
	}
	legacyRow, err := repo.GetAIAnalysis(ctx, "tenant_a", "alarm_a", "legacy-tenant-knowledge")
	if err != nil || legacyRow.KnowledgeScope != "legacy-tenant-knowledge" {
		t.Fatalf("legacy analysis must report its column scope: %#v err=%v", legacyRow, err)
	}
	for _, v := range []model.AIAnalysis{
		{TenantID: "tenant_b", AlarmID: "alarm_b", Summary: "base"},
		{TenantID: "tenant_b", AlarmID: "alarm_b", Summary: "scoped", KnowledgeScope: "alarm-handler", KnowledgeDocuments: []string{"doc-1"}},
	} {
		if err = repo.SaveAIAnalysis(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	base, err := repo.GetAIAnalysis(ctx, "tenant_b", "alarm_b", "")
	if err != nil || base.Summary != "base" {
		t.Fatalf("knowledge-free analysis overwritten or missing: %#v err=%v", base, err)
	}
	scoped, err := repo.GetAIAnalysis(ctx, "tenant_b", "alarm_b", "alarm-handler")
	if err != nil || scoped.Summary != "scoped" || len(scoped.KnowledgeDocuments) != 1 {
		t.Fatalf("knowledge analysis not stored separately: %#v err=%v", scoped, err)
	}
} /* 结束当前表达式或代码块。 */

func TestMigrateDeviceSystemTagsToFields(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schemaName := fmt.Sprintf("device_fields_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE") }()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r := &Repository{pool: pool}
	if err = r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	legacy := `{"id":"child","tenantId":"t","productId":"p","name":"子设备","status":"ENABLED","deviceRole":"CHILD","gatewayId":"gw","tags":{"floor":"1","connector":"TCP_CHILD","connectorProfileId":"gw-tcp","childAddress":"3","childType":"smoke","onboardingRequestHash":"h"}}`
	if _, err = pool.Exec(ctx, `INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES('t','child','p','ENABLED','k','',$1)`, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO iot_product(tenant_id,id,status,protocol_package_id,body) VALUES('t','gw','ENABLED','','{"id":"gw","tenantId":"t","category":"gateway"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO device_registry(tenant_id,id,product_id,status,access_key,secret_hash,body) VALUES('t','legacy-gw','gw','ENABLED','k2','','{"id":"legacy-gw","tenantId":"t","productId":"gw","status":"ENABLED"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO protocol_release(tenant_id,protocol_id,version,status,parser_type,body) VALUES('t','meter','1','PUBLISHED','modbus_tcp_parser_v2','{"protocolId":"meter","version":"1","status":"PUBLISHED"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO iot_product(tenant_id,id,status,protocol_package_id,body) VALUES('t','meter-product','ENABLED','meter@1','{"id":"meter-product","tenantId":"t","protocolPackageId":"meter@1"}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = r.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var tags map[string]string
	var connector string
	if err = pool.QueryRow(ctx, `SELECT body->>'connector', body->'tags' FROM device_registry WHERE id='child'`).Scan(&connector, &tags); err != nil {
		t.Fatal(err)
	}
	if connector != "TCP_CHILD" || len(tags) != 1 || tags["floor"] != "1" {
		t.Fatalf("stored body not migrated: connector=%q tags=%v", connector, tags)
	}
	d, err := r.GetManagedDevice(ctx, "t", "child")
	if err != nil || d.ConnectorProfileID != "gw-tcp" || d.ChildAddress != "3" || d.ChildType != "smoke" || d.OnboardingRequestHash != "h" {
		t.Fatalf("migrated device %+v %v", d, err)
	}
	if gw, err := r.GetManagedDevice(ctx, "t", "legacy-gw"); err != nil || gw.DeviceRole != "GATEWAY" || d.DeviceRole != "CHILD" {
		t.Fatalf("stored roles: gateway %+v child %q %v", gw, d.DeviceRole, err)
	}
	if binding, err := r.GetProductProtocolBinding(ctx, "t", "meter-product"); err != nil || binding.ProtocolID != "meter" || binding.Version != "1" {
		t.Fatalf("missing binding was not created: %+v %v", binding, err)
	}
}
