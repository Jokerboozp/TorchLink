package memory

import (
	"context"
	"fmt"
	"strings"

	"iot-platform/internal/model"
)

func (r *Repository) UpsertExternalAlarm(_ context.Context, report model.Alarm) (model.Alarm, bool, bool, error) {
	if report.TenantID == "" || report.ID == "" || report.DeviceID == "" || report.TriggerID == "" || !strings.Contains(report.RuleID, ":external:") {
		return report, false, false, fmt.Errorf("%w: external alarm identity is incomplete", model.ErrInvalidIngress)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(report.TenantID, report.ID)
	saved, exists := r.alarms[k]
	if exists {
		if saved.DeviceID != report.DeviceID || saved.RuleID != report.RuleID {
			return report, false, false, fmt.Errorf("%w: external alarm identity belongs to a different device or rule", model.ErrInvalidIngress)
		}
		if saved.TriggerID == report.TriggerID || saved.Status == "CLOSED" || saved.Status == "RECOVERED" {
			return cloneAlarm(saved), false, false, nil
		}
		saved.Details = report.Details
		saved.Content = report.Content
		saved.LastTriggeredAt = report.LastTriggeredAt
		saved.AlarmLevel = report.AlarmLevel
		saved.Confidence = report.Confidence
		saved.TriggerID = report.TriggerID
		saved.TriggerCount++
		saved.Version++
	} else {
		saved = report
		saved.TriggerCount = 1
		saved.Version = 1
	}
	r.alarms[k] = cloneAlarm(saved)
	r.addOutbox(model.AlarmReportEvent(saved, report))
	return cloneAlarm(saved), !exists, true, nil
}
