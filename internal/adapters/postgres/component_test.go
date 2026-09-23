package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                         /* 执行当前语句并推进处理流程。 */
	"fmt"                             /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5"         /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5/pgxpool" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"     /* 执行当前语句并推进处理流程。 */
	"os"                              /* 执行当前语句并推进处理流程。 */
	"sync"                            /* 执行当前语句并推进处理流程。 */
	"testing"                         /* 执行当前语句并推进处理流程。 */
	"time"                            /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestComponentAlarmAtomicWatermarkAndRestart(t *testing.T) { /* 定义 TestComponentAlarmAtomicWatermarkAndRestart 函数。 */
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN") /* 更新 dsn 的值。 */
	if dsn == "" {                            /* 判断条件并选择处理分支。 */
		t.Skip("IOT_TEST_POSTGRES_DSN not configured") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx := context.Background()         /* 更新 ctx 的值。 */
	admin, err := pgxpool.New(ctx, dsn) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer admin.Close()                                               /* 安排函数结束时执行清理。 */
	name := fmt.Sprintf("component_test_%d", time.Now().UnixNano())   /* 更新 name 的值。 */
	ident := pgx.Identifier{name}.Sanitize()                          /* 更新 ident 的值。 */
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE") }() /* 安排函数结束时执行清理。 */
	cfg, err := pgxpool.ParseConfig(dsn)                                       /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg.ConnConfig.RuntimeParams["search_path"] = name /* 执行当前语句并推进处理流程。 */
	pool, err := pgxpool.NewWithConfig(ctx, cfg)       /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer pool.Close()                       /* 安排函数结束时执行清理。 */
	repo := &Repository{pool: pool}          /* 更新 repo 的值。 */
	if err = repo.Migrate(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.Migrate(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("migration not idempotent", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", ComponentID: "c", ComponentName: "探测器", ComponentLocation: "三楼", RuleID: "device-report:FIRE:component:c", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device", FirstTriggeredAt: 1000, LastTriggeredAt: 1000, TriggerCount: 1, TriggerID: "m1"} /* 更新 a 的值。 */
	state := model.ComponentAlarmState{Timestamp: 1000, MessageID: "m1", Active: true}                                                                                                                                                                                                                                             /* 更新 state 的值。 */
	var wg sync.WaitGroup                                                                                                                                                                                                                                                                                                          /* 声明 wg。 */
	for i := 0; i < 8; i++ {                                                                                                                                                                                                                                                                                                       /* 循环处理当前数据。 */
		wg.Add(1)   /* 执行当前语句并推进处理流程。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()                                                                    /* 安排函数结束时执行清理。 */
			saved, event, err := repo.ApplyComponentAlarm(ctx, a, state)                       /* 更新 err 的值。 */
			if err != nil || saved.ID != "a" || event != "raised" || saved.TriggerCount != 1 { /* 判断条件并选择处理分支。 */
				t.Errorf("concurrent receive: %+v %s %v", saved, event, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wg.Wait() /* 执行当前语句并推进处理流程。 */
	// Another component under the same controller stays active.
	b := a                                                               /* 更新 b 的值。 */
	b.ID = "b"                                                           /* 更新 b.ID 的值。 */
	b.ComponentID = "other"                                              /* 更新 b.ComponentID 的值。 */
	b.RuleID = "device-report:FIRE:component:other"                      /* 更新 b.RuleID 的值。 */
	if _, _, err = repo.ApplyComponentAlarm(ctx, b, state); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	pool.Close()                                /* 执行当前语句并推进处理流程。 */
	pool, err = pgxpool.NewWithConfig(ctx, cfg) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer pool.Close()                                                                 /* 安排函数结束时执行清理。 */
	repo = &Repository{pool: pool}                                                     /* 更新 repo 的值。 */
	a.LastTriggeredAt = 2000                                                           /* 更新 a.LastTriggeredAt 的值。 */
	state = model.ComponentAlarmState{Timestamp: 2000, MessageID: "m2", Active: false} /* 更新 state 的值。 */
	saved, event, err := repo.ApplyComponentAlarm(ctx, a, state)                       /* 更新 err 的值。 */
	if err != nil || saved.Status != "RECOVERED" || event != "recovered" {             /* 判断条件并选择处理分支。 */
		t.Fatal(saved, event, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	state = model.ComponentAlarmState{Timestamp: 1500, MessageID: "old", Active: true} /* 更新 state 的值。 */
	saved, event, err = repo.ApplyComponentAlarm(ctx, a, state)                        /* 更新 err 的值。 */
	if err != nil || saved.Status != "RECOVERED" || event != "" {                      /* 判断条件并选择处理分支。 */
		t.Fatal("stale resurrected alarm", saved, event, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	active, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "d", Status: "ACTIVE", Limit: 100}) /* 更新 err 的值。 */
	if err != nil || len(active) != 1 || active[0].ComponentID != "other" {                                            /* 判断条件并选择处理分支。 */
		t.Fatal(active, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Same IDs in another tenant are independent.
	a.TenantID = "other-tenant"                                                                   /* 更新 a.TenantID 的值。 */
	a.ID = "a"                                                                                    /* 更新 a.ID 的值。 */
	state = model.ComponentAlarmState{Timestamp: 1000, MessageID: "m1", Active: true}             /* 更新 state 的值。 */
	if _, event, err = repo.ApplyComponentAlarm(ctx, a, state); err != nil || event != "raised" { /* 判断条件并选择处理分支。 */
		t.Fatal(event, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cancelled, cancel := context.WithCancel(ctx)                               /* 更新 cancel 的值。 */
	cancel()                                                                   /* 执行当前语句并推进处理流程。 */
	a.TenantID = "cancelled"                                                   /* 更新 a.TenantID 的值。 */
	if _, _, err = repo.ApplyComponentAlarm(cancelled, a, state); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("cancelled transaction succeeded") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var count int                                                                                                                                  /* 声明 count。 */
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM component_alarm_state WHERE tenant_id='cancelled'`).Scan(&count); err != nil || count != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal(count, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
