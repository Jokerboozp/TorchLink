package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                              /* 执行当前语句并推进处理流程。 */
	"fmt"                                  /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5"              /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5/pgxpool"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/repositorytest" /* 执行当前语句并推进处理流程。 */
	"os"                                   /* 执行当前语句并推进处理流程。 */
	"testing"                              /* 执行当前语句并推进处理流程。 */
	"time"                                 /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Uses only its own temporary schema in an explicitly configured test database.
func TestDeviceOperationsMigrationAndAtomicity(t *testing.T) { /* 定义 TestDeviceOperationsMigrationAndAtomicity 函数。 */
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN") /* 更新 dsn 的值。 */
	if dsn == "" {                            /* 判断条件并选择处理分支。 */
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx := context.Background()       /* 更新 ctx 的值。 */
	admin, e := pgxpool.New(ctx, dsn) /* 更新 e 的值。 */
	if e != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer admin.Close()                                              /* 安排函数结束时执行清理。 */
	name := fmt.Sprintf("operations_test_%d", time.Now().UnixNano()) /* 更新 name 的值。 */
	ident := pgx.Identifier{name}.Sanitize()                         /* 更新 ident 的值。 */
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+ident); e != nil {    /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }() /* 安排函数结束时执行清理。 */
	cfg, e := pgxpool.ParseConfig(dsn)                                                          /* 更新 e 的值。 */
	if e != nil {                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg.ConnConfig.RuntimeParams["search_path"] = name /* 执行当前语句并推进处理流程。 */
	pool, e := pgxpool.NewWithConfig(ctx, cfg)         /* 更新 e 的值。 */
	if e != nil {                                      /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer pool.Close()                /* 安排函数结束时执行清理。 */
	r := &Repository{pool: pool}      /* 更新 r 的值。 */
	if e = r.Migrate(ctx); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = r.Migrate(ctx); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("migration is not repeatable", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var created bool                                                                                                                                                                                                               /* 声明 created。 */
	if e = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('edge_node','edge_read_job','edge_program'))`).Scan(&created); e != nil || created { /* 判断条件并选择处理分支。 */
		t.Fatal("fresh schema still creates edge tables", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Upgrading existing installations must leave historical node records intact.
	if _, e = r.pool.Exec(ctx, `CREATE TABLE edge_node(tenant_id text,id text,body jsonb); INSERT INTO edge_node VALUES ('legacy','node','{"status":"DISABLED"}')`); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = r.Migrate(ctx); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var preserved int                                                                                                                                   /* 声明 preserved。 */
	if e = r.pool.QueryRow(ctx, `SELECT count(*) FROM edge_node WHERE tenant_id='legacy' AND id='node'`).Scan(&preserved); e != nil || preserved != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("historical node data was changed", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	verifyRetiredDeviceTablesUntouched(t, r)                                                                                  /* 执行当前语句并推进处理流程。 */
	verifyOnboardingAndParseMigration(t, r)                                                                                   /* 执行当前语句并推进处理流程。 */
	repositorytest.AccessStatus(t, r)                                                                                         /* 执行当前语句并推进处理流程。 */
	repositorytest.ExecutionLease(t, r)                                                                                       /* 执行当前语句并推进处理流程。 */
	repositorytest.RawReservation(t, r)                                                                                       /* 执行当前语句并推进处理流程。 */
	repositorytest.ProtocolRegistration(t, r)                                                                                 /* 执行当前语句并推进处理流程。 */
	repositorytest.ProtocolChildren(t, r)                                                                                     /* 执行当前语句并推进处理流程。 */
	d := model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "key", SecretHash: "hash"} /* 更新 d 的值。 */
	if e = r.SaveManagedDevice(ctx, d); e != nil {                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	other := d                                         /* 更新 other 的值。 */
	other.ID = "other"                                 /* 更新 other.ID 的值。 */
	other.AccessKey = "taken"                          /* 更新 other.AccessKey 的值。 */
	if e = r.SaveManagedDevice(ctx, other); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, e = r.ChangeDeviceCredential(ctx, "t", "d", "taken", "newhash", 1); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate credential accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	pending, e := r.ListCredentialRevocations(ctx, "t", "d", false) /* 更新 e 的值。 */
	if e != nil || len(pending) != 0 {                              /* 判断条件并选择处理分支。 */
		t.Fatal("transaction leaked revoke", pending, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	saved, e := r.GetManagedDevice(ctx, "t", "d") /* 更新 e 的值。 */
	if e != nil || saved.SecretHash != "hash" {   /* 判断条件并选择处理分支。 */
		t.Fatal("transaction changed original", saved, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	saved, revoke, e := r.ChangeDeviceCredential(ctx, "t", "d", "newkey", "newhash", 2) /* 更新 e 的值。 */
	if e != nil || saved.AccessKey != "newkey" || revoke.Username != "key" {            /* 判断条件并选择处理分支。 */
		t.Fatal(saved, revoke, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	pending, e = r.ListCredentialRevocations(ctx, "t", "d", true) /* 更新 e 的值。 */
	if e != nil || len(pending) != 1 {                            /* 判断条件并选择处理分支。 */
		t.Fatal(pending, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q := model.DeviceCommand{TenantID: "t", DeviceID: "d", ProductID: "p", ID: "c1", Type: "set", Data: map[string]any{}, Status: "DISPATCHING", CreatedAt: 1} /* 更新 q 的值。 */
	if _, created, e := r.CreateDeviceCommand(ctx, q); e != nil || !created {                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(created, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, created, e := r.CreateDeviceCommand(ctx, q); e != nil || created { /* 判断条件并选择处理分支。 */
		t.Fatal(created, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = r.CompleteDeviceCommand(ctx, "t", "other", "c1", map[string]any{"success": true}, 2); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	commands, _, e := r.ListDeviceCommands(ctx, "t", "d", 20, 0) /* 更新 e 的值。 */
	if e != nil || commands[0].Status != "DISPATCHING" {         /* 判断条件并选择处理分支。 */
		t.Fatal(commands, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = r.CompleteDeviceCommand(ctx, "t", "d", "c1", map[string]any{"success": true}, 3); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = r.UpdateDeviceCommandDispatch(ctx, "t", "c1", "SENT", "", 4); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	commands, _, e = r.ListDeviceCommands(ctx, "t", "d", 20, 0) /* 更新 e 的值。 */
	if e != nil || commands[0].Status != "SUCCEEDED" {          /* 判断条件并选择处理分支。 */
		t.Fatal(commands, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = r.SaveDeviceStateEvent(ctx, model.DeviceState{TenantID: "t", DeviceID: "d", ConnectionStatus: "CONNECTED"}); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	events, total, e := r.ListDeviceStateEvents(ctx, "t", "d", 20, 0) /* 更新 e 的值。 */
	if e != nil || total != 1 || events[0].RecordedAt <= 0 {          /* 判断条件并选择处理分支。 */
		t.Fatal(events, total, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = r.SaveStandardMessage(ctx, model.StandardMessage{TenantID: "t", DeviceID: "d", MessageID: "m1", MessageType: model.EventReport, Timestamp: 1}); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	messages, total, e := r.ListDeviceMessages(ctx, "t", "d", model.EventReport, 20, 0) /* 更新 e 的值。 */
	if e != nil || total != 1 || len(messages) != 1 {                                   /* 判断条件并选择处理分支。 */
		t.Fatal(messages, total, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

} /* 结束当前表达式或代码块。 */

func verifyOnboardingAndParseMigration(t *testing.T, r *Repository) { /* 定义 verifyOnboardingAndParseMigration 函数。 */
	t.Helper()                                                                                                        /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                                                                                       /* 更新 ctx 的值。 */
	idx := model.RawArchiveIndex{TenantID: "t", MessageID: "raw-parse", ProductID: "p", DeviceID: "d", ArchivedAt: 1} /* 更新 idx 的值。 */
	if _, e := r.SaveRawIndex(ctx, idx); e != nil {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e := r.MarkRawParseResult(ctx, "t", idx.MessageID, 123, "invalid frame"); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e := r.Migrate(ctx); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	saved, e := r.GetRawIndex(ctx, "t", idx.MessageID)                                    /* 更新 e 的值。 */
	if e != nil || saved.ParseError != "invalid frame" || saved.ParseAttemptedAt != 123 { /* 判断条件并选择处理分支。 */
		t.Fatal("parse migration lost evidence", saved, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e := r.MarkRawParseResult(ctx, "other", idx.MessageID, 456, ""); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	saved, _ = r.GetRawIndex(ctx, "t", idx.MessageID) /* 更新 _ 的值。 */
	if saved.ParseError == "" {                       /* 判断条件并选择处理分支。 */
		t.Fatal("cross tenant update") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	svc := onboarding.New(r, parser.NewPlatformRegistry(t.TempDir()), t.TempDir(), nil) /* 更新 svc 的值。 */
	q := onboarding.EnrollRequest{RequestID: "req-1", NewProduct: &onboarding.NewProduct{ID: "new-product", Name: "test product", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"}, Device: onboarding.EnrollDevice{ID: "new-device", Name: "test"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}}
	first, e := svc.Enroll(ctx, "t", q)
	if e != nil || first.Reused || first.Credential.Secret == "" {
		t.Fatal(e)
	}
	again, e := svc.Enroll(ctx, "t", q)
	if e != nil || !again.Reused || again.Credential.Secret != "" {
		t.Fatal("persistent idempotency failed", e)
	}
	filtered, total, e := r.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: "t", Role: "DIRECT", RestrictProducts: true, ProductIDs: []string{"new-product"}, Query: "NEW-dev", Status: "ENABLED", Runtime: "NEVER_SEEN"}, 10, 0)
	if e != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != "new-device" || filtered[0].SecretHash == "" {
		t.Fatal("filtered device list", filtered, total, e)
	}
	if _, total, e = r.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: "t", Query: "new_dev"}, 10, 0); e != nil || total != 0 {
		t.Fatal("keyword wildcard was not escaped", total, e)
	}
	q.Device.Name = "changed"                      /* 更新 q.Name 的值。 */
	if _, e := svc.Enroll(ctx, "t", q); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("different request accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
