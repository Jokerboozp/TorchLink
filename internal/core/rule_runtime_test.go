package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"log/slog"      /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type ruleTestClock struct{ now time.Time } /* 定义 ruleTestClock 类型。 */

func (c *ruleTestClock) Now() time.Time { return c.now } /* 定义 Now 函数。 */

type failFirstStateRepository struct { /* 定义 failFirstStateRepository 类型。 */
	ports.Repository      /* 执行当前语句并推进处理流程。 */
	fail             bool /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (r *failFirstStateRepository) UpsertDeviceState(ctx context.Context, state model.DeviceState) error { /* 定义 UpsertDeviceState 函数。 */
	if r.fail { /* 判断条件并选择处理分支。 */
		r.fail = false                                     /* 更新 r.fail 的值。 */
		return errors.New("simulated state write failure") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.UpsertDeviceState(ctx, state) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func newRuleTestEngine(t *testing.T, repo ports.Repository, clock *ruleTestClock) *Engine { /* 定义 newRuleTestEngine 函数。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	e.Clock = clock                                                                                                                                       /* 更新 e.Clock 的值。 */
	return e                                                                                                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func standardRuleMessage(id string, ts int64) []byte { /* 定义 standardRuleMessage 函数。 */
	b, _ := json.Marshal(model.StandardMessage{MessageID: id, RawMessageID: "raw-" + id, TenantID: "tenant-a", ProductID: "sensor", DeviceID: "device-a", MessageType: model.PropertyReport, Timestamp: ts, Properties: map[string]any{"temperature": 90}}) /* 更新 _ 的值。 */
	return b                                                                                                                                                                                                                                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestDurationRuleSurvivesEngineRestart(t *testing.T) { /* 定义 TestDurationRuleSurvivesEngineRestart 函数。 */
	ctx := context.Background()                                                                                                                                                                                                                                                                                      /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                                                                                                                                                                                   /* 更新 repo 的值。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-duration", TenantID: "tenant-a", ProductID: "sensor", Name: "持续高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, DurationSeconds: 10, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	clock := &ruleTestClock{now: time.Unix(1000, 0)}                                             /* 更新 clock 的值。 */
	first := newRuleTestEngine(t, repo, clock)                                                   /* 更新 first 的值。 */
	if err := first.handleStandard(ctx, standardRuleMessage("message-1", 1000000)); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(alarms) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("alarm triggered before duration: %#v", alarms) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	clock.now = time.Unix(1011, 0)                                                                /* 更新 clock.now 的值。 */
	second := newRuleTestEngine(t, repo, clock)                                                   /* 更新 second 的值。 */
	if err := second.handleStandard(ctx, standardRuleMessage("message-2", 1011000)); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 1 {                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("persisted duration did not trigger: alarms=%#v err=%v", alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDuplicateStandardMessageDoesNotRetriggerAlarm(t *testing.T) { /* 定义 TestDuplicateStandardMessageDoesNotRetriggerAlarm 函数。 */
	ctx := context.Background()                                                                                                                                                                                                                                                                                                                                  /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                                                                                                                                                                                                                               /* 更新 repo 的值。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-duplicate", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Actions: []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)}) /* 更新 e 的值。 */
	realtime := e.Realtime.(*local.Realtime)                                 /* 更新 realtime 的值。 */
	payload := standardRuleMessage("same-message", 1000000)                  /* 更新 payload 的值。 */
	if err := e.handleStandard(ctx, payload); err != nil {                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.handleStandard(ctx, payload); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 1 {                             /* 判断条件并选择处理分支。 */
		t.Fatalf("duplicate retriggered alarm: alarms=%#v err=%v", alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var actionCount int                           /* 声明 actionCount。 */
	for _, published := range realtime.Messages { /* 循环处理当前数据。 */
		if published.Topic == "/iot/ui-action/tenant-a" { /* 判断条件并选择处理分支。 */
			actionCount++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if actionCount != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("duplicate message executed rule action %d times, want 1", actionCount) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestMatchingAlarmRuleActionsRunForEachNewAlarmMessage(t *testing.T) { /* 定义 TestMatchingAlarmRuleActionsRunForEachNewAlarmMessage 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{ /* 判断条件并选择处理分支。 */
		ID: "rule-actions", TenantID: "tenant-a", ProductID: "sensor", Name: "高温动作", /* 执行当前语句并推进处理流程。 */
		AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, /* 执行当前语句并推进处理流程。 */
		Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, /* 执行当前语句并推进处理流程。 */
		Actions:    []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}},                 /* 执行当前语句并推进处理流程。 */
	}); err != nil { /* 结束当前表达式或代码块。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	realtime := local.NewRealtime()                                                                                                            /* 更新 realtime 的值。 */
	e := New(repo, archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	e.Clock = &ruleTestClock{now: time.Unix(1000, 0)}                                                                                          /* 更新 e.Clock 的值。 */

	for _, messageID := range []string{"alarm-message-1", "alarm-message-2"} { /* 循环处理当前数据。 */
		if err := e.handleStandard(ctx, standardRuleMessage(messageID, e.Clock.Now().UnixMilli())); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		e.Clock = &ruleTestClock{now: e.Clock.Now().Add(time.Second)} /* 更新 e.Clock 的值。 */
	} /* 结束当前表达式或代码块。 */

	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", DeviceID: "device-a", Status: "ACTIVE"}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 2 {                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected deduplicated alarm: alarms=%#v err=%v", alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var actionCount int                           /* 声明 actionCount。 */
	for _, published := range realtime.Messages { /* 循环处理当前数据。 */
		if published.Topic == "/iot/ui-action/tenant-a" { /* 判断条件并选择处理分支。 */
			actionCount++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if actionCount != 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("matching alarm rule action count = %d, want 2", actionCount) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestUnprocessedStandardMessageIsRetriedAfterDownstreamFailure(t *testing.T) { /* 定义 TestUnprocessedStandardMessageIsRetriedAfterDownstreamFailure 函数。 */
	ctx := context.Background()                                                                                                                                                                                                                                                            /* 更新 ctx 的值。 */
	base := memory.NewRepository()                                                                                                                                                                                                                                                         /* 更新 base 的值。 */
	repo := &failFirstStateRepository{Repository: base, fail: true}                                                                                                                                                                                                                        /* 更新 repo 的值。 */
	if err := base.SaveRule(ctx, model.AlarmRule{ID: "rule-retry", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)}) /* 更新 e 的值。 */
	payload := standardRuleMessage("retry-message", 1000000)                 /* 更新 payload 的值。 */
	if err := e.handleStandard(ctx, payload); err == nil {                   /* 判断条件并选择处理分支。 */
		t.Fatal("expected first downstream failure") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.handleStandard(ctx, payload); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	alarms, err := base.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 1 {                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("unprocessed message was not retried: alarms=%#v err=%v", alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDeleteRuleRecoversActiveAlarmsAndClearsPending(t *testing.T) { /* 定义 TestDeleteRuleRecoversActiveAlarmsAndClearsPending 函数。 */
	ctx := context.Background()                                                                                                                                                                                                                                                             /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                                                                                                                                                          /* 更新 repo 的值。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-delete", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})                      /* 更新 e 的值。 */
	if err := e.handleStandard(ctx, standardRuleMessage("delete-message", 1000000)); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.DeleteRule(ctx, "tenant-a", "rule-delete"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if active, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(active) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("deleted rule left active alarms: %#v", active) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	recovered, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "RECOVERED"}) /* 更新 err 的值。 */
	if err != nil || len(recovered) != 1 {                                                               /* 判断条件并选择处理分支。 */
		t.Fatalf("deleted rule did not retain recovered history: alarms=%#v err=%v", recovered, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDisableRuleRecoversActiveAlarmsAndClearsPending(t *testing.T) { /* 定义 TestDisableRuleRecoversActiveAlarmsAndClearsPending 函数。 */
	ctx := context.Background()                                                                                                                                                                                                                                                              /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                                                                                                                                                           /* 更新 repo 的值。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-disable", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})                       /* 更新 e 的值。 */
	if err := e.handleStandard(ctx, standardRuleMessage("disable-message", 1000000)); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.DisableRule(ctx, "tenant-a", "rule-disable"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if active, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(active) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("disabled rule left active alarms: %#v", active) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	recovered, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "RECOVERED"}) /* 更新 err 的值。 */
	if err != nil || len(recovered) != 1 {                                                               /* 判断条件并选择处理分支。 */
		t.Fatalf("disabled rule did not retain recovered history: alarms=%#v err=%v", recovered, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
