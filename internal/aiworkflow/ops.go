package aiworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/aiprompt"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// GenerateReport summarises tenant data through the ops-assistant Harness
// workflow. The data is gathered through repository ports; the model never
// receives SQL access.
func (e *Service) GenerateReport(ctx context.Context, tenantID, period string, start, end int64) (string, error) {
	if e.Authorizer != nil {
		var err error
		ctx, err = e.authorize(ctx, tenantID, WorkflowOpsReport)
		if err != nil {
			return "", err
		}
	}
	if start <= 0 || end <= start {
		return "", fmt.Errorf("valid start/end are required")
	}
	if len([]rune(period)) > 80 {
		return "", fmt.Errorf("报告周期说明不能超过 80 个字符")
	}
	filter := ports.AlarmFilter{TenantID: tenantID, Start: start, End: end, Limit: 10, Summary: true}
	alarms, err := e.engine.Repo.ListAlarms(ctx, filter)
	if err != nil {
		return "", err
	}
	total, err := e.engine.Repo.CountAlarms(ctx, filter)
	if err != nil {
		return "", err
	}
	groups, err := e.engine.Repo.DashboardCounts(ctx, tenantID, start, end)
	if err != nil {
		return "", err
	}
	// Storage aggregates obey exactly the caller's device scope. Never ship full
	// states or alarm Details (which contain the original telemetry) to Harness.
	summary := map[string]map[string]int{}
	omittedGroups := 0
	for _, g := range groups {
		if g.Kind == "product" || len(g.Key) > 128 {
			omittedGroups++
			continue
		}
		if summary[g.Kind] == nil {
			summary[g.Kind] = map[string]int{}
		}
		if len(summary[g.Kind]) >= 32 {
			omittedGroups++
			continue
		}
		summary[g.Kind][g.Key] = g.Count
	}
	samples := []map[string]any{}
	for _, a := range alarms {
		samples = append(samples, map[string]any{"alarmId": boundedText(a.ID, 128), "deviceId": boundedText(a.DeviceID, 128), "type": boundedText(a.AlarmType, 80), "level": a.AlarmLevel, "status": a.Status, "firstTriggeredAt": a.FirstTriggeredAt})
	}
	payload, err := json.Marshal(map[string]any{"period": period, "start": start, "end": end, "counts": summary, "alarmTotal": total, "recentAlarmSample": samples, "omittedAlarms": max(0, total-len(samples)), "omittedGroups": omittedGroups})
	if err != nil {
		return "", err
	}
	prompt := aiprompt.OpsReport(payload)
	if len(mustJSON(prompt)) > 20000 {
		return "", fmt.Errorf("报告统计摘要超出输入预算，请缩短报告时段")
	}
	var alarmTypes []string
	for _, a := range alarms {
		alarmTypes = append(alarmTypes, boundedText(a.AlarmType, 80))
	}
	result, err := e.runBusinessWorkflow(ctx, tenantID, WorkflowOpsReport, aiprompt.OpsReportVersion, prompt, retrievalQuery("消防物联网运维 告警处置建议", alarmTypes), []string{"query_device_latest", "query_alarm_list", "query_property_history", "query_similar_alarms", "query_knowledge_base"}, 8192)
	_ = e.engine.Repo.SaveAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenantID, Actor: "ai-report-generator", Action: "ai.report", TargetType: "report", TargetID: id("report"), Details: map[string]any{"period": period, "start": start, "end": end, "runId": result.RunID, "success": err == nil}, CreatedAt: e.engine.Clock.Now().UnixMilli()})
	return result.Answer, err
}

func boundedText(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}
