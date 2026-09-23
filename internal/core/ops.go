package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var deviceIDPattern = regexp.MustCompile(`(?i)\b(device[_-][a-z0-9_-]+)\b`) /* 声明 deviceIDPattern。 */

// OpsChat gathers data only through controlled repository/knowledge ports. The model never receives SQL access.
func (e *Engine) OpsChat(ctx context.Context, tenantID, question string) (string, error) { /* 定义 OpsChat 函数。 */
	contextData := map[string]any{}                                                                                                                               /* 更新 contextData 的值。 */
	alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenantID, Status: "ACTIVE", Start: time.Now().Add(-24 * time.Hour).UnixMilli(), Limit: 50}) /* 更新 err 的值。 */
	if err == nil {                                                                                                                                               /* 判断条件并选择处理分支。 */
		contextData["activeAlarmsLast24h"] = alarms /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if match := deviceIDPattern.FindStringSubmatch(question); len(match) > 1 { /* 判断条件并选择处理分支。 */
		if state, stateErr := e.Repo.GetDeviceState(ctx, tenantID, match[1]); stateErr == nil { /* 判断条件并选择处理分支。 */
			contextData["deviceLatest"] = state /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if history, historyErr := e.Repo.PropertyHistory(ctx, tenantID, match[1], "temperature", time.Now().Add(-24*time.Hour).UnixMilli(), time.Now().UnixMilli(), 500); historyErr == nil { /* 判断条件并选择处理分支。 */
			contextData["temperatureHistory"] = history /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if e.KB != nil { /* 判断条件并选择处理分支。 */
		if docs, kbErr := e.KB.Search(ctx, tenantID, question, 5); kbErr == nil { /* 判断条件并选择处理分支。 */
			contextData["knowledge"] = docs /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	b, _ := json.Marshal(contextData)                                                                                                                                                                                                                                                     /* 更新 _ 的值。 */
	answer, err := e.AI.Chat(ctx, tenantID, "用户问题："+question+"\n受控平台工具返回的数据："+string(b)+"\n仅依据这些数据作答；数据不足时明确说明。")                                                                                                                                                                         /* 更新 err 的值。 */
	_ = e.Repo.SaveAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenantID, Actor: "ai-ops-chat", Action: "ai.chat", TargetType: "conversation", TargetID: id("chat"), Details: map[string]any{"question": question, "success": err == nil}, CreatedAt: e.Clock.Now().UnixMilli()}) /* 检查错误并决定后续处理。 */
	return answer, err                                                                                                                                                                                                                                                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) GenerateReport(ctx context.Context, tenantID, period string, start, end int64) (string, error) { /* 定义 GenerateReport 函数。 */
	if start <= 0 || end <= start { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("valid start/end are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenantID, Start: start, End: end, Limit: 5000}) /* 更新 err 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	states, _ := e.Repo.ListDeviceStates(ctx, tenantID)                                                                                                                                                                                                                                                                 /* 更新 _ 的值。 */
	payload, _ := json.Marshal(map[string]any{"period": period, "start": start, "end": end, "alarms": alarms, "deviceStates": states})                                                                                                                                                                                  /* 更新 _ 的值。 */
	report, err := e.AI.Chat(ctx, tenantID, "请根据受控平台数据生成消防物联网"+period+"报告，包含告警概况、高等级风险、设备离线情况、趋势、处置建议和数据局限。数据："+string(payload))                                                                                                                                                                                        /* 更新 err 的值。 */
	_ = e.Repo.SaveAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenantID, Actor: "ai-report-generator", Action: "ai.report", TargetType: "report", TargetID: id("report"), Details: map[string]any{"period": period, "start": start, "end": end, "success": err == nil}, CreatedAt: e.Clock.Now().UnixMilli()}) /* 检查错误并决定后续处理。 */
	return report, err                                                                                                                                                                                                                                                                                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
