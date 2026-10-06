package core

import (
	"context"
	"iot-platform/internal/logkey"

	"iot-platform/internal/model"
)

// publishEvent delivers an event to the internal bus and the realtime channel.
// The stored state is already authoritative, so a delivery failure does not
// undo it; it is logged and counted so lost notifications are visible.
func (e *Engine) publishEvent(ctx context.Context, topic, key, realtimeTopic string, payload []byte) {
	if err := e.Bus.Publish(ctx, topic, key, payload); err != nil {
		e.deliveryFailed("bus", topic, err)
	}
	if err := e.Realtime.Publish(ctx, realtimeTopic, payload, 1, false); err != nil {
		e.deliveryFailed("realtime", topic, err)
	}
}

func (e *Engine) deliveryFailed(channel, topic string, err error) {
	if e.Metrics != nil {
		e.Metrics.Inc("event_publish_failed_total")
	}
	if e.Log != nil {
		e.Log.Warn("event delivery failed", "channel", channel, "topic", topic, "error", err)
	}
}

// RecordAudit writes an audit entry. The audited action has already happened,
// so a failed write is reported instead of failing the action.
func (e *Engine) RecordAudit(ctx context.Context, entry model.AuditLog) {
	if entry.ID == "" {
		entry.ID = model.NewAuditID()
	}
	if err := e.Repo.SaveAudit(ctx, entry); err != nil {
		if e.Metrics != nil {
			e.Metrics.Inc("audit_write_failed_total")
		}
		if e.Log != nil {
			e.Log.Error("audit write failed", logkey.Tenant, entry.TenantID, "action", entry.Action, "error", err)
		}
	}
}
