package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

func (e *Engine) alarmLocation(ctx context.Context, tenant, deviceID, componentID string) *model.AlarmLocation {
	if e.Locator == nil {
		return nil
	}
	return e.Locator.AlarmLocation(ctx, tenant, deviceID, componentID)
}

func (e *Engine) alarmDeviceName(ctx context.Context, tenantID, deviceID string) string {
	device, err := e.Repo.GetManagedDevice(ctx, tenantID, deviceID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(device.Name)
}

func (e *Engine) SetAlarmStatus(ctx context.Context, tenant, alarmID, status, actor string) (model.Alarm, error) {
	now := e.Clock.Now().UnixMilli()
	a, _, err := e.mutateAlarm(ctx, tenant, alarmID, func(a *model.Alarm) (bool, error) {
		switch status {
		case "ACKED":
			if a.Status != "ACTIVE" {
				return false, fmt.Errorf("only active alarms can be acknowledged")
			}
			a.Status = status
			a.AckedAt = now
		case "RECOVERED":
			// An external system may assert recovery for alarm types that no
			// clearing property describes.
			if a.Status != "ACTIVE" && a.Status != "ACKED" {
				return false, fmt.Errorf("only active or acknowledged alarms can be recovered")
			}
			a.Status = status
			a.RecoveredAt = now
		case "CLOSED":
			if a.RequiresVerification() && a.Disposition == nil {
				return false, model.ErrDispositionRequired
			}
			a.Status = status
			a.ClosedAt = now
		case "SUPPRESSED":
			// Suppression silences an open alarm; reopening a recovered or
			// closed one as suppressed would rewrite its outcome.
			if a.Status != "ACTIVE" && a.Status != "ACKED" {
				return false, fmt.Errorf("only active or acknowledged alarms can be suppressed")
			}
			a.Status = status
		default:
			return false, fmt.Errorf("unsupported status %s", status)
		}
		return true, nil
	})
	if err != nil {
		return a, err
	}
	if err := e.syncDeviceBusinessStatus(ctx, a.TenantID, "", a.DeviceID); err != nil {
		return a, err
	}
	e.RecordAudit(ctx, model.AuditLog{TenantID: tenant, Actor: actor, Action: "alarm." + strings.ToLower(status), TargetType: "alarm", TargetID: alarmID, CreatedAt: now})
	payload := mustJSON(a)
	if status == "RECOVERED" {
		e.publishEvent(ctx, model.TopicAlarmRecovered, a.ID, a.MQTTTopic("recovered"), payload)
		return a, nil
	}
	e.publishEvent(ctx, model.TopicAlarmConfirmed, a.ID, a.MQTTTopic("confirmed"), payload)
	return a, nil
}

// VerifyAlarm records the on-site verification of an alarm. It can be
// corrected until the alarm is closed.
func (e *Engine) VerifyAlarm(ctx context.Context, tenant, alarmID string, d model.AlarmDisposition, actor string) (model.Alarm, error) {
	if !model.ValidDispositionResult(d.Result) {
		return model.Alarm{}, fmt.Errorf("unknown verification result %q", d.Result)
	}
	now := e.Clock.Now().UnixMilli()
	d.Handler, d.VerifiedAt = actor, now
	// The AI snapshot is taken by the platform, never from the request.
	d.AIAnalysisAt, d.AIRiskLevel, d.AIPromptVersion = 0, "", ""
	if analysis, ok := e.latestAIAnalysis(ctx, tenant, alarmID); ok {
		d.AIAnalysisAt, d.AIRiskLevel, d.AIPromptVersion = analysis.CreatedAt, analysis.RiskLevel, analysis.PromptVersion
	}
	a, _, err := e.mutateAlarm(ctx, tenant, alarmID, func(a *model.Alarm) (bool, error) {
		if a.Status == "CLOSED" {
			return false, fmt.Errorf("closed alarms cannot be verified again")
		}
		if d.ArrivedAt != 0 && (d.ArrivedAt < a.FirstTriggeredAt || d.ArrivedAt > now) {
			return false, fmt.Errorf("arrival time must be between the alarm and now")
		}
		disposition := d
		a.Disposition = &disposition
		return true, nil
	})
	if err != nil {
		return a, err
	}
	e.RecordAudit(ctx, model.AuditLog{TenantID: tenant, Actor: actor, Action: "alarm.verify", TargetType: "alarm", TargetID: alarmID, Details: map[string]any{"result": d.Result, "dispatchId": d.DispatchID}, CreatedAt: now})
	return a, nil
}

// latestAIAnalysis returns the newest successful analysis of an alarm across
// knowledge scopes; fallback results written after a failed run are skipped.
func (e *Engine) latestAIAnalysis(ctx context.Context, tenant, alarmID string) (model.AIAnalysis, bool) {
	var latest model.AIAnalysis
	found := false
	for _, scope := range []string{model.AIAnalysisScopeNone, model.AlarmAnalysisWorkflowID, model.AIAnalysisScopeLegacyTenant} {
		analysis, err := e.Repo.GetAIAnalysis(ctx, tenant, alarmID, scope)
		if err != nil || analysis.Error != "" || analysis.RiskLevel == "" || found && analysis.CreatedAt <= latest.CreatedAt {
			continue
		}
		latest, found = analysis, true
	}
	return latest, found
}

// AddAlarmAttachment records an uploaded attachment, whose ID comes from
// NewAlarmAttachmentID, on an alarm that is not closed yet.
func (e *Engine) AddAlarmAttachment(ctx context.Context, tenant, alarmID string, att model.AlarmAttachment, actor string) (model.Alarm, error) {
	att.UploadedBy, att.UploadedAt = actor, e.Clock.Now().UnixMilli()
	return e.changeAttachments(ctx, tenant, alarmID, actor, "alarm.attachment.add", att, func(a *model.Alarm) error {
		if len(a.Attachments) >= model.MaxAlarmAttachments {
			return model.ErrTooManyAttachments
		}
		a.Attachments = append(a.Attachments, att)
		return nil
	})
}

// NewAlarmAttachmentID reserves the identity of an attachment before its
// file is stored, so the object key is known up front.
func NewAlarmAttachmentID() string { return id("att") }

// RemoveAlarmAttachment drops an attachment from an alarm that is not closed
// yet and returns it, so the caller can delete the file.
func (e *Engine) RemoveAlarmAttachment(ctx context.Context, tenant, alarmID, attachmentID, actor string) (model.AlarmAttachment, error) {
	var removed model.AlarmAttachment
	_, err := e.changeAttachments(ctx, tenant, alarmID, actor, "alarm.attachment.remove", model.AlarmAttachment{ID: attachmentID}, func(a *model.Alarm) error {
		index := slices.IndexFunc(a.Attachments, func(v model.AlarmAttachment) bool { return v.ID == attachmentID })
		if index < 0 {
			return model.ErrAttachmentNotFound
		}
		removed = a.Attachments[index]
		a.Attachments = slices.Delete(slices.Clone(a.Attachments), index, index+1)
		return nil
	})
	return removed, err
}

func (e *Engine) changeAttachments(ctx context.Context, tenant, alarmID, actor, action string, att model.AlarmAttachment, change func(*model.Alarm) error) (model.Alarm, error) {
	a, _, err := e.mutateAlarm(ctx, tenant, alarmID, func(a *model.Alarm) (bool, error) {
		if a.Status == "CLOSED" {
			return false, model.ErrAlarmClosed
		}
		return true, change(a)
	})
	if err != nil {
		return a, err
	}
	e.RecordAudit(ctx, model.AuditLog{TenantID: tenant, Actor: actor, Action: action, TargetType: "alarm", TargetID: alarmID, Details: map[string]any{"attachmentId": att.ID, "name": att.Name, "size": att.Size}, CreatedAt: e.Clock.Now().UnixMilli()})
	return a, nil
}
