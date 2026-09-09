package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ComponentStatus is a partial observation: omitted components/alarm types
// retain their previous state. IDs are stable within the parent device.
type ComponentStatus struct {
	ID        string          `json:"id"`
	Name      string          `json:"name,omitempty"`
	Location  string          `json:"location,omitempty"`
	Timestamp int64           `json:"timestamp"`
	Alarms    map[string]bool `json:"alarms"`
}

var componentAlarmType = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func MessageComponents(msg StandardMessage) ([]ComponentStatus, error) {
	value, exists := msg.Event["components"]
	if !exists {
		return nil, nil
	}
	if msg.MessageType != AlarmReport && msg.MessageType != StateChange && msg.MessageType != EventReport {
		return nil, errors.New("component status requires alarm, state or event message")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var components []ComponentStatus
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&components); err != nil {
		return nil, fmt.Errorf("invalid components: %w", err)
	}
	if len(components) == 0 || len(components) > 256 {
		return nil, errors.New("components must contain 1..256 objects")
	}
	var raw []struct {
		Alarms map[string]json.RawMessage `json:"alarms"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range components {
		c := &components[i]
		if strings.TrimSpace(c.ID) != c.ID || c.ID == "" || len(c.ID) > 128 || strings.ContainsAny(c.ID, "\x00\r\n") || seen[c.ID] || len(c.Name) > 256 || len(c.Location) > 512 {
			return nil, errors.New("component identity is invalid or duplicated")
		}
		seen[c.ID] = true
		if c.Timestamp == 0 {
			c.Timestamp = msg.Timestamp
		}
		if c.Timestamp <= 0 || c.Timestamp > 253402300799999 || (msg.Timestamp > 0 && c.Timestamp > msg.Timestamp+300000) {
			return nil, errors.New("invalid component timestamp")
		}
		if len(c.Alarms) == 0 || len(c.Alarms) > 32 {
			return nil, errors.New("component alarms must contain 1..32 explicit boolean states")
		}
		// encoding/json accepts null as false for bool. Reject that ambiguity.
		for kind := range c.Alarms {
			if !componentAlarmType.MatchString(kind) || string(raw[i].Alarms[kind]) == "null" {
				return nil, errors.New("invalid component alarm type or boolean")
			}
		}
	}
	return components, nil
}

// The watermark and alarm transition must commit atomically. AlarmID refers to
// the last lifecycle, including recovered/closed records; it is not a device ID.
type ComponentAlarmState struct {
	Timestamp int64  `json:"timestamp"`
	MessageID string `json:"messageId"`
	Active    bool   `json:"active"`
	AlarmID   string `json:"alarmId"`
	Event     string `json:"event,omitempty"`
}

func (next ComponentAlarmState) Supersedes(old ComponentAlarmState) bool {
	if next.Timestamp < old.Timestamp || (next.MessageID == old.MessageID && old.MessageID != "") {
		return false
	}
	// Second-resolution fire clocks can collide. An equal-time normal state
	// must not clear an asserted alarm; a later explicit normal state can.
	return next.Timestamp != old.Timestamp || (!old.Active && next.Active)
}

// TransitionComponentAlarm is shared by persistent and in-memory repositories.
func TransitionComponentAlarm(candidate, old Alarm, state ComponentAlarmState) (Alarm, string) {
	active := old.Status == "ACTIVE" || old.Status == "ACKED"
	if state.Active {
		if active {
			old.LastTriggeredAt = candidate.LastTriggeredAt
			old.TriggerCount++
			old.TriggerID = candidate.TriggerID
			old.Details = candidate.Details
			if candidate.ComponentName != "" {
				old.ComponentName = candidate.ComponentName
			}
			if candidate.ComponentLocation != "" {
				old.ComponentLocation = candidate.ComponentLocation
			}
			return old, ""
		}
		return candidate, "raised"
	}
	if active {
		old.Status = "RECOVERED"
		old.RecoveredAt = candidate.LastTriggeredAt
		return old, "recovered"
	}
	return old, ""
}
