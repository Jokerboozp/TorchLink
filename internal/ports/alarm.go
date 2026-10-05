package ports

import (
	"context"

	"iot-platform/internal/model"
)

// AlarmStore keeps alarms and the outbox of their events.
type AlarmStore interface {
	ApplyComponentAlarm(context.Context, model.Alarm, model.ComponentAlarmState) (model.Alarm, string, error)
	UpsertAlarm(context.Context, model.Alarm) (model.Alarm, bool, error)
	// UpsertExternalAlarm uses an external event's deterministic alarm identity.
	// It returns created and triggerChanged, preserving terminal states and
	// committing each new trigger with its report event atomically.
	UpsertExternalAlarm(context.Context, model.Alarm) (model.Alarm, bool, bool, error)
	// Alarm upserts and ApplyComponentAlarm commit the alarm report event with the
	// alarm. DrainOutbox publishes pending events in order and removes each one
	// after publish succeeds, stopping at the first failure.
	DrainOutbox(ctx context.Context, limit int, publish func(model.OutboxEvent) error) (int, error)
	GetAlarm(context.Context, string, string) (model.Alarm, error)
	ListAlarms(context.Context, AlarmFilter) ([]model.Alarm, error)
	CountAlarms(context.Context, AlarmFilter) (int, error)
	HasOpenAlarm(context.Context, string, string) (bool, error)
	UpdateAlarm(context.Context, model.Alarm) error
	// UpdateAlarmIf writes only if the stored version equals v.Version.
	UpdateAlarmIf(context.Context, model.Alarm) (bool, error)
}
