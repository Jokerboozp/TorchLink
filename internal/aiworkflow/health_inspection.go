package aiworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/aiprompt"
	"iot-platform/internal/core"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// InspectDeviceHealth builds a deterministic health snapshot first, then asks
// the configured model for a narrative. The deterministic portion remains
// useful when AI is unavailable and prevents the model from inventing device
// counts or last-seen times.
func (e *Service) InspectDeviceHealth(ctx context.Context, tenantID string) (model.DeviceHealthReport, error) {
	if e.Authorizer != nil {
		var err error
		ctx, err = e.authorize(ctx, tenantID, WorkflowHealthInspection)
		if err != nil {
			return model.DeviceHealthReport{}, err
		}
	}
	now := e.engine.Clock.Now().UnixMilli()
	// Active alarms are streamed and devices read in pages, so the inspection
	// holds one small item per device rather than the tenant's full records.
	activeAlarms := make(map[string]int)
	alarmCount := 0
	if err := e.engine.Repo.EachAlarm(ctx, ports.AlarmFilter{TenantID: tenantID, Status: "ACTIVE", Summary: true}, func(alarm model.Alarm) error {
		activeAlarms[alarm.DeviceID]++
		alarmCount++
		return nil
	}); err != nil {
		return model.DeviceHealthReport{}, err
	}
	// Health signals add findings such as stuck values; the job keeps at most
	// a few per device, so the tenant's list stays small.
	signals := map[string][]model.DeviceSignal{}
	if e.engine.DeviceSignals != nil {
		if list, err := e.engine.DeviceSignals.ListDeviceSignals(ctx, tenantID, nil, 5000); err == nil {
			for _, s := range list {
				signals[s.DeviceID] = append(signals[s.DeviceID], s)
			}
		}
	}
	items := []model.DeviceHealthItem{}
	seen := map[string]struct{}{}
	for offset := 0; ; offset += inspectionPageSize {
		devices, _, err := e.engine.Repo.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: tenantID}, inspectionPageSize, offset)
		if err != nil {
			return model.DeviceHealthReport{}, err
		}
		ids := make([]string, 0, len(devices))
		for _, device := range devices {
			ids = append(ids, device.ID)
		}
		states, err := e.engine.Repo.GetDeviceStatesByIDs(ctx, tenantID, ids)
		if err != nil {
			return model.DeviceHealthReport{}, err
		}
		for _, device := range devices {
			if _, ok := seen[device.ID]; ok {
				continue
			}
			seen[device.ID] = struct{}{}
			items = append(items, withSignals(healthItem(device.ID, device.Name, device.ProductID, states[device.ID], activeAlarms[device.ID], now), signals[device.ID]))
		}
		if len(devices) < inspectionPageSize {
			break
		}
	}
	for offset := 0; ; offset += inspectionPageSize {
		states, _, err := e.engine.Repo.ListUnregisteredDeviceStatesPage(ctx, tenantID, inspectionPageSize, offset)
		if err != nil {
			return model.DeviceHealthReport{}, err
		}
		for _, state := range states {
			if _, ok := seen[state.DeviceID]; ok {
				continue
			}
			seen[state.DeviceID] = struct{}{}
			items = append(items, withSignals(healthItem(state.DeviceID, state.DeviceID, state.ProductID, state, activeAlarms[state.DeviceID], now), signals[state.DeviceID]))
		}
		if len(states) < inspectionPageSize {
			break
		}
	}
	sort.Slice(items, func(i, j int) bool {
		severityRank := map[string]int{"CRITICAL": 0, "HIGH": 1, "MEDIUM": 2, "LOW": 3, "INFO": 4}
		if severityRank[items[i].Severity] != severityRank[items[j].Severity] {
			return severityRank[items[i].Severity] < severityRank[items[j].Severity]
		}
		return items[i].DeviceID < items[j].DeviceID
	})
	counts := map[string]int{"total": len(items), "healthy": 0, "attention": 0, "critical": 0, "offline": 0, "activeAlarms": alarmCount}
	for _, item := range items {
		if item.Severity == "INFO" {
			counts["healthy"]++
		} else {
			counts["attention"]++
		}
		if item.Severity == "CRITICAL" {
			counts["critical"]++
		}
		if item.BusinessStatus == "OFFLINE" || item.BusinessStatus == "SUSPECTED_OFFLINE" {
			counts["offline"]++
		}
	}
	report := model.DeviceHealthReport{TenantID: tenantID, GeneratedAt: now, Counts: counts, Items: items, Summary: fmt.Sprintf("共检查 %d 个设备：%d 个正常，%d 个需要关注，%d 个离线或疑似离线。", counts["total"], counts["healthy"], counts["attention"], counts["offline"])}
	if e.AIWorkflowsReady() {
		payload, _ := json.Marshal(inspectionPromptSnapshot(now, counts, items))
		prompt := aiprompt.HealthInspection(payload)
		result, adviceErr := e.runBusinessWorkflow(ctx, tenantID, WorkflowHealthInspection, aiprompt.HealthInspectionVersion, prompt, inspectionRetrievalQuery(items), []string{"query_system_overview", "query_device_latest", "query_alarm_list", "query_property_history", "query_knowledge_base"}, 4096)
		if adviceErr != nil {
			report.Warnings = append(report.Warnings, "AI 巡检建议生成失败："+adviceErr.Error())
		} else {
			report.AIAdvice = result.Answer
		}
	} else {
		report.Warnings = append(report.Warnings, "AI 巡检建议未生成："+ErrAIWorkflowsUnavailable.Error())
	}
	e.engine.RecordAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenantID, Actor: "ai-health-inspection", Action: "ai.health-inspection", TargetType: "device-health", TargetID: fmt.Sprintf("inspection_%d", now), Details: map[string]any{"counts": counts, "success": true}, CreatedAt: now})
	return report, nil
}

func healthItem(deviceID, deviceName, productID string, state model.DeviceState, activeAlarmCount int, now int64) model.DeviceHealthItem {
	status := strings.ToUpper(strings.TrimSpace(state.BusinessStatus))
	if status == "" {
		status = "NEVER_SEEN"
	}
	severity := "INFO"
	findings := []string{}
	if status == "OFFLINE" || status == "SUSPECTED_OFFLINE" {
		severity = "HIGH"
		findings = append(findings, "设备已离线或疑似离线")
	}
	if state.LastSeenAt == 0 {
		severity = "HIGH"
		findings = append(findings, "设备尚未收到有效上报")
	} else if now-state.LastSeenAt > 24*time.Hour.Milliseconds() && severity == "INFO" {
		severity = "MEDIUM"
		findings = append(findings, "超过 24 小时未上报")
	}
	if activeAlarmCount > 0 {
		if severity == "INFO" || severity == "MEDIUM" {
			severity = "HIGH"
		}
		findings = append(findings, fmt.Sprintf("存在 %d 条活动告警", activeAlarmCount))
	}
	if len(findings) == 0 {
		findings = append(findings, "最近状态正常")
	}
	return model.DeviceHealthItem{DeviceID: deviceID, DeviceName: deviceName, ProductID: productID, BusinessStatus: status, DataStatus: state.DataStatus, LastSeenAt: state.LastSeenAt, ActiveAlarmCount: activeAlarmCount, Severity: severity, Findings: findings}
}

// withSignals adds a device's health signals to its findings; a healthy
// device with signals needs attention.
func withSignals(item model.DeviceHealthItem, signals []model.DeviceSignal) model.DeviceHealthItem {
	if len(signals) == 0 {
		return item
	}
	if item.Severity == "INFO" {
		item.Severity = "MEDIUM"
		item.Findings = item.Findings[:0]
	}
	for _, s := range signals {
		finding := "健康信号：" + core.SignalTypeNames[s.SignalType]
		if s.Property != "" {
			finding += "（" + s.Property + "）"
		}
		item.Findings = append(item.Findings, finding)
	}
	return item
}

// inspectionPageSize is how many devices or states one inspection query reads.
const inspectionPageSize = 500

// inspectionPromptLimit caps how many devices are written into the model
// prompt: a large tenant would otherwise exceed the model's context window.
const inspectionPromptLimit = 40

// inspectionPromptSnapshot keeps the most severe devices (items are sorted by
// severity) and replaces healthy and overflow devices by counts. The full
// report returned to the page is unchanged.
// inspectionRetrievalQuery asks for maintenance guidance on the products and
// findings that need attention, not for the snapshot itself.
func inspectionRetrievalQuery(items []model.DeviceHealthItem) string {
	var terms []string
	for _, item := range items {
		if item.Severity == "INFO" {
			continue
		}
		terms = append(terms, item.ProductID)
		terms = append(terms, item.Findings...)
	}
	return retrievalQuery("设备巡检 维护保养 故障处置", terms)
}

func inspectionPromptSnapshot(now int64, counts map[string]int, items []model.DeviceHealthItem) map[string]any {
	selected := []model.DeviceHealthItem{}
	omitted := 0
	for _, item := range items {
		if item.Severity == "INFO" {
			continue
		}
		if len(selected) >= inspectionPromptLimit {
			omitted++
			continue
		}
		selected = append(selected, item)
	}
	snapshot := map[string]any{"generatedAt": now, "counts": counts, "devicesNeedingAttention": selected}
	if omitted > 0 {
		snapshot["omittedAttentionDevices"] = omitted
		snapshot["note"] = fmt.Sprintf("另有 %d 台需关注的设备未列出，按严重程度只列出前 %d 台；健康设备只计入 counts。", omitted, inspectionPromptLimit)
	}
	return snapshot
}
