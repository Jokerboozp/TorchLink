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
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestLargePropertyReportStillTriggersRules(t *testing.T) { /* 定义 TestLargePropertyReportStillTriggersRules 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	properties := map[string]any{}                                                                                                                        /* 更新 properties 的值。 */
	for i := 0; i < 257; i++ {                                                                                                                            /* 循环处理当前数据。 */
		properties[fmt.Sprint(i)] = float64(i) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	properties["temperature"] = float64(90)                                                                                                                                                                                                                /* 执行当前语句并推进处理流程。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule", TenantID: "tenant", Name: "temperature", AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	msg := model.StandardMessage{TenantID: "tenant", DeviceID: "device", ProductID: "product", MessageID: "oversized", MessageType: model.PropertyReport, Properties: properties, Timestamp: time.Now().UnixMilli()} /* 更新 msg 的值。 */
	data, _ := json.Marshal(msg)                                                                                                                                                                                     /* 更新 _ 的值。 */
	if err := e.handleStandard(ctx, data); err != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal("property processing failed", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	shouldProcess, _, err := repo.ClaimStandardMessage(ctx, msg) /* 更新 err 的值。 */
	if err != nil || shouldProcess {                             /* 判断条件并选择处理分支。 */
		t.Fatal("message not completed", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant", DeviceID: "device", Limit: 10}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 1 {                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal("property report did not trigger rule alarm", alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
