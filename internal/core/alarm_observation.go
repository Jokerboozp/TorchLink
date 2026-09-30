package core

import (
	"context"
	"errors"
	"iot-platform/internal/model"
)

func (e *Engine) sourceAlarmObservation(ctx context.Context, msg model.StandardMessage, origin, signal, kind, alarmType, component string, rule *model.AlarmRule) (model.AlarmObservation, error) {
	now := e.Clock.Now().UnixMilli()
	o := model.AlarmObservation{TenantID: msg.TenantID, DeviceID: msg.DeviceID, ComponentID: component, SourceSystem: "STANDARD_MESSAGE", SourceEventID: msg.MessageID, StandardMessageID: msg.MessageID, RawMessageID: msg.RawMessageID, EventIndex: signal, OriginKind: origin, SignalKey: signal, FactKind: kind, AlarmType: alarmType, EventAt: msg.Timestamp, TimeQuality: "UNVERIFIED", RecordedAt: now, EvaluationAt: now, SourceInputHash: model.ObservationHash(msg), ProtocolVersion: msg.ParserVersion, Payload: map[string]any{"message": msg}, HistoricalQuality: "LIVE", IdentityQuality: "PLATFORM_INPUT"}
	if o.SourceEventID == "" {
		o.SourceEventID = msg.RawMessageID
	}
	// Trust is an explicit validated ingest attribute, never inferred from a
	// positive device timestamp or proximity to the processing clock.
	if q := msg.Tags["eventTimeQuality"]; q == "TRUSTED" || q == "CONFLICTED" || q == "ESTIMATED" || q == "MISSING" {
		o.TimeQuality = q
	}
	if msg.Timestamp <= 0 {
		o.TimeQuality = "MISSING"
	}
	if msg.RawMessageID != "" {
		receipt, cached := ctx.Value(alarmSourceReceiptKey{}).(*alarmSourceReceipt)
		if !cached {
			receipt = &alarmSourceReceipt{}
		}
		if !receipt.loaded {
			index, err := e.Repo.GetRawIndex(ctx, msg.TenantID, msg.RawMessageID)
			if err == nil {
				receipt.receivedAt = index.ReceivedAt
			} else if !errors.Is(err, model.ErrNotFound) {
				return o, err
			}
			receipt.loaded = true
		}
		o.ReceivedAt = receipt.receivedAt
	}
	if rule != nil {
		o.RuleID = rule.ID
		o.RuleVersion = rule.Version
		o.ConditionHash = model.ObservationHash(struct {
			Conditions []model.RuleCondition
			Recovery   []model.RuleCondition
			Match      string
			Expression string
		}{rule.Conditions, rule.Recovery, rule.Match, rule.Expression})
	}
	return o, nil
}

// Only explicit booleans can assert the same direct signal later cleared by
// that flag. A generic ALARM_REPORT remains REPORT_ONLY.
func directAlarmObservationKind(msg model.StandardMessage, kind string) string {
	keys := map[string][]string{"FIRE": {"fireAlarm"}, "SMOKE_DETECTED": {"smoke", "smokeDetected"}, "DEVICE_FAULT": {"fault", "powerFault", "openCircuit", "shortCircuit", "removed", "sensorFault", "upgradeFault"}, "DEVICE_OFFLINE": {"offline"}, "MANUAL_ALARM": {"alarm"}}[kind]
	for _, key := range keys {
		if v, ok := messageValue(msg, key); ok {
			if active, ok := v.(bool); ok && active {
				return "ASSERT"
			}
		}
	}
	return "REPORT"
}

type alarmSourceReceiptKey struct{}
type alarmSourceReceipt struct {
	loaded     bool
	receivedAt int64
}
