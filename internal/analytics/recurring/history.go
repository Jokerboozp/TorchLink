package recurring

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sort"
)

type HistoricalNormalization struct {
	Observations []model.AlarmObservation
	Quality      string
	Limitations  []string
}

// NormalizeHistoricalMessage reads successfully parsed, immutable historical
// input. It never reingests, changes production alarm state or expands a
// TriggerCount into invented events. Rule evaluations cannot be reconstructed
// from a standard message without their original rule/version decision.
func NormalizeHistoricalMessage(msg model.StandardMessage, index *model.RawArchiveIndex, recordedAt int64) (HistoricalNormalization, error) {
	out := HistoricalNormalization{Observations: []model.AlarmObservation{}, Quality: "PARTIAL", Limitations: []string{"HISTORICAL_COLLECTION_COVERAGE_UNKNOWN", "RULE_EVALUATIONS_NOT_RECONSTRUCTED"}}
	if msg.MessageID == "" || msg.TenantID == "" || msg.DeviceID == "" {
		return out, model.ErrNotFound
	}
	components, err := model.MessageComponents(msg)
	if err != nil {
		return out, err
	}
	newFact := func(signal, kind, alarmType, component string, eventAt int64) model.AlarmObservation {
		o := model.AlarmObservation{TenantID: msg.TenantID, DeviceID: msg.DeviceID, ComponentID: component, SourceSystem: "HISTORICAL_STANDARD_MESSAGE", SourceEventID: msg.MessageID, StandardMessageID: msg.MessageID, RawMessageID: msg.RawMessageID, EventIndex: signal, OriginKind: "COMPONENT_STATE", SignalKey: signal, AlarmType: alarmType, FactKind: kind, EventAt: eventAt, TimeQuality: "UNVERIFIED", RecordedAt: recordedAt, ProtocolVersion: msg.ParserVersion, SourceInputHash: model.ObservationHash(msg), IdentityQuality: "PLATFORM_INPUT", Acceptance: "HISTORICAL_UNRESOLVED", Reason: "ORIGINAL_PRODUCTION_ACCEPTANCE_UNKNOWN", HistoricalQuality: "PARTIAL", Payload: map[string]any{"message": msg}}
		if msg.Tags["eventTimeQuality"] == "TRUSTED" {
			o.TimeQuality = "TRUSTED"
		}
		if index != nil && index.TenantID == msg.TenantID && index.MessageID == msg.RawMessageID {
			o.ReceivedAt = index.ReceivedAt
		}
		return o
	}
	for _, c := range components {
		kinds := make([]string, 0, len(c.Alarms))
		for kind := range c.Alarms {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			fact := "CLEAR"
			if c.Alarms[kind] {
				fact = "ASSERT"
			}
			o := newFact("component:"+c.ID+":"+kind, fact, kind, c.ID, c.Timestamp)
			if err := o.Normalize(); err != nil {
				return out, err
			}
			out.Observations = append(out.Observations, o)
		}
	}
	if len(components) == 0 && msg.MessageType == model.AlarmReport {
		kind, _ := msg.Event["alarmType"].(string)
		if kind == "" {
			kind = "UNKNOWN"
		}
		o := newFact("device:"+kind, "REPORT", kind, "", msg.Timestamp)
		o.OriginKind = "DEVICE_DIRECT"
		if err := o.Normalize(); err != nil {
			return out, err
		}
		out.Observations = append(out.Observations, o)
		out.Limitations = append(out.Limitations, "REPORT_ONLY_NO_RECOVERY_BOUNDARY")
	}
	return out, nil
}

// PersistHistoricalObservations uses only the governance fact port. A
// checkpoint may advance only after every member in the batch has committed.
// The returned count is consumed members, not a maximum source sequence.
func PersistHistoricalObservations(ctx context.Context, store ports.AlarmObservationStore, items []model.AlarmObservation) (int, error) {
	consumed := 0
	for _, o := range items {
		if _, _, err := store.SaveAlarmObservation(ctx, o); err != nil {
			return consumed, err
		}
		consumed++
	}
	return consumed, nil
}
