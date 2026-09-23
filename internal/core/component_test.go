package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"encoding/json"                         /* 执行当前语句并推进处理流程。 */
	"fmt"                                   /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"           /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestComponentAlarmLifecycleAndRecoveryIsolation(t *testing.T) { /* 定义 TestComponentAlarmLifecycleAndRecoveryIsolation 函数。 */
	ctx := context.Background()                                                                                                                           /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                        /* 更新 repo 的值。 */
	archive, _ := local.NewArchive(t.TempDir())                                                                                                           /* 更新 _ 的值。 */
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	send := func(id string, at int64, components ...model.ComponentStatus) {                                                                              /* 更新 send 的值。 */
		t.Helper()                                                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
		m := model.StandardMessage{MessageID: id, TenantID: "t", ProductID: "p", DeviceID: "controller", MessageType: model.StateChange, Timestamp: at, Event: map[string]any{"components": components}} /* 更新 m 的值。 */
		b, _ := json.Marshal(m)                                                                                                                                                                          /* 更新 _ 的值。 */
		if err := e.handleStandard(ctx, b); err != nil {                                                                                                                                                 /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	c := func(id string, fire, fault bool) model.ComponentStatus { /* 更新 c 的值。 */
		return model.ComponentStatus{ID: id, Name: "探测器", Location: "二楼走廊", Alarms: map[string]bool{"FIRE": fire, "DEVICE_FAULT": fault}} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	active := func(n int) { /* 更新 active 的值。 */
		t.Helper()                                                                                                                  /* 执行当前语句并推进处理流程。 */
		alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "controller", Status: "ACTIVE", Limit: 100}) /* 更新 err 的值。 */
		if err != nil || len(alarms) != n {                                                                                         /* 判断条件并选择处理分支。 */
			t.Fatalf("active=%+v err=%v want=%d", alarms, err, n) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	send("m1", 1000, c("A", false, false), c("B", true, true)) /* 执行当前语句并推进处理流程。 */
	active(2)                                                  /* 执行当前语句并推进处理流程。 */
	send("normal-A", 2000, c("A", false, false))               /* 执行当前语句并推进处理流程。 */
	active(2)                                                  /* 执行当前语句并推进处理流程。 */
	// Registration/connection status cannot recover either component.
	b, _ := json.Marshal(model.StandardMessage{MessageID: "online", TenantID: "t", ProductID: "p", DeviceID: "controller", MessageType: model.StateChange, Timestamp: 2500, Properties: map[string]any{"connectionStatus": "CONNECTED"}}) /* 更新 _ 的值。 */
	if err := e.handleStandard(ctx, b); err != nil {                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	active(2) /* 执行当前语句并推进处理流程。 */
	// Recovery of one category must leave the other category active.
	send("recover-fire", 3000, model.ComponentStatus{ID: "B", Alarms: map[string]bool{"FIRE": false}})   /* 执行当前语句并推进处理流程。 */
	active(1)                                                                                            /* 执行当前语句并推进处理流程。 */
	send("old-alarm", 1500, c("B", true, true))                                                          /* 执行当前语句并推进处理流程。 */
	active(1)                                                                                            /* 执行当前语句并推进处理流程。 */
	send("same-time-alarm", 3000, model.ComponentStatus{ID: "B", Alarms: map[string]bool{"FIRE": true}}) /* 执行当前语句并推进处理流程。 */
	active(2)                                                                                            /* 执行当前语句并推进处理流程。 */
	send("same-time-normal", 3000, c("B", false, false))                                                 /* 执行当前语句并推进处理流程。 */
	active(1)                                                                                            /* 执行当前语句并推进处理流程。 */
	send("recover-all", 4000, c("B", false, false))                                                      /* 执行当前语句并推进处理流程。 */
	active(0)                                                                                            /* 执行当前语句并推进处理流程。 */
	send("late-replay", 1000, c("B", true, true))                                                        /* 执行当前语句并推进处理流程。 */
	active(0)                                                                                            /* 执行当前语句并推进处理流程。 */
	send("new-cycle", 5000, c("B", true, false))                                                         /* 执行当前语句并推进处理流程。 */
	active(1)                                                                                            /* 执行当前语句并推进处理流程。 */
	send("new-cycle", 5000, c("B", true, false))                                                         /* 执行当前语句并推进处理流程。 */
	active(1)                                                                                            /* 执行当前语句并推进处理流程。 */
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE", Limit: 100})    /* 更新 _ 的值。 */
	a := alarms[0]                                                                                       /* 更新 a 的值。 */
	if a.ComponentID != "B" || a.ComponentLocation != "二楼走廊" || a.TriggerCount != 1 {                    /* 判断条件并选择处理分支。 */
		t.Fatalf("bad component alarm %+v", a) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	a.Status = "ACKED"                               /* 更新 a.Status 的值。 */
	if err := repo.UpdateAlarm(ctx, a); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	send("acked-recovery", 6000, c("B", false, false)) /* 执行当前语句并推进处理流程。 */
	active(0)                                          /* 执行当前语句并推进处理流程。 */
	got, _ := repo.GetAlarm(ctx, "t", a.ID)            /* 更新 _ 的值。 */
	if got.Status != "RECOVERED" {                     /* 判断条件并选择处理分支。 */
		t.Fatal(got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// More than the old 100-row recovery limit, each addressed independently.
	for batch := 0; batch < 2; batch++ { /* 循环处理当前数据。 */
		items := []model.ComponentStatus{} /* 更新 items 的值。 */
		for i := 0; i < 80; i++ {          /* 循环处理当前数据。 */
			items = append(items, c(fmt.Sprintf("many-%d-%d", batch, i), true, false)) /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
		send(fmt.Sprint("many", batch), 7000, items...) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for batch := 0; batch < 2; batch++ { /* 循环处理当前数据。 */
		items := []model.ComponentStatus{} /* 更新 items 的值。 */
		for i := 0; i < 80; i++ {          /* 循环处理当前数据。 */
			items = append(items, c(fmt.Sprintf("many-%d-%d", batch, i), false, false)) /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
		send(fmt.Sprint("clear", batch), 8000, items...) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	active(0) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func TestInvalidComponentsAndPlainStateCannotClearFire(t *testing.T) { /* 定义 TestInvalidComponentsAndPlainStateCannotClearFire 函数。 */
	for _, value := range []any{nil, []any{}, []any{map[string]any{"id": "x", "alarms": map[string]any{"FIRE": nil}}}, []any{map[string]any{"id": "x", "alarms": map[string]any{"FIRE": "false"}}}, []model.ComponentStatus{{ID: "x", Alarms: map[string]bool{"FIRE": true}}, {ID: "x", Alarms: map[string]bool{"FIRE": false}}}} { /* 循环处理当前数据。 */
		if _, err := model.MessageComponents(model.StandardMessage{MessageType: model.AlarmReport, Timestamp: 1000, Event: map[string]any{"components": value}}); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("accepted invalid %#v", value) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if directAlarmCleared(model.StandardMessage{MessageType: model.StateChange, Properties: map[string]any{"connectionStatus": "CONNECTED"}}) { /* 判断条件并选择处理分支。 */
		t.Fatal("connection cleared alarm") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if directAlarmTypeCleared(model.StandardMessage{Properties: map[string]any{"fault": false}}, "FIRE") { /* 判断条件并选择处理分支。 */
		t.Fatal("fault recovery cleared fire") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
