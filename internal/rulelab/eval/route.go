package eval

import (
	"crypto/sha256"
	"fmt"
	"sort"

	"iot-platform/internal/model"
)

const DirectRulePrefix = "device-report:"

type DirectIntent struct {
	RuleID    string `json:"ruleId"`
	AlarmType string `json:"alarmType"`
	Level     string `json:"level"`
}

type ComponentRoute struct {
	RuleID    string                    `json:"ruleId"`
	Component model.ComponentStatus     `json:"component"`
	AlarmType string                    `json:"alarmType"`
	State     model.ComponentAlarmState `json:"state"`
}

type RouteDecision struct {
	DeviceAssertion bool             `json:"deviceAssertion"`
	Components      []ComponentRoute `json:"components"`
	DirectRaise     *DirectIntent    `json:"directRaise,omitempty"`
	DirectRecover   bool             `json:"directRecover"`
}

// Route retains the device assertion independently of classification. Explicit
// component observations always use the component path. A rule-handled report
// creates no additional direct alarm. Omitted normal flags recover nothing.
func Route(msg model.StandardMessage, ruleAlarmHandled bool) (RouteDecision, error) {
	out := RouteDecision{DeviceAssertion: msg.MessageType == model.AlarmReport, Components: []ComponentRoute{}}
	components, err := model.MessageComponents(msg)
	if err != nil {
		return out, err
	}
	for _, component := range components {
		kinds := make([]string, 0, len(component.Alarms))
		for kind := range component.Alarms {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			out.Components = append(out.Components, ComponentRoute{RuleID: fmt.Sprintf("%s%s:component:%x", DirectRulePrefix, kind, sha256.Sum256([]byte(component.ID))), Component: component, AlarmType: kind, State: model.ComponentAlarmState{Timestamp: component.Timestamp, MessageID: msg.MessageID, Active: component.Alarms[kind]}})
		}
	}
	if len(components) > 0 {
		return out, nil
	}
	if msg.MessageType == model.AlarmReport && !ruleAlarmHandled {
		kind, level := DirectMetadata(msg)
		out.DirectRaise = &DirectIntent{RuleID: DirectRulePrefix + kind, AlarmType: kind, Level: level}
	}
	if msg.MessageType != model.AlarmReport && DirectCleared(msg) {
		out.DirectRecover = true
	}
	return out, nil
}

type ComponentDecision struct {
	Alarm       model.Alarm               `json:"alarm"`
	Watermark   model.ComponentAlarmState `json:"watermark"`
	Event       string                    `json:"event,omitempty"`
	Applied     bool                      `json:"applied"`
	ReplayEvent bool                      `json:"replayEvent"`
}

// ComponentTransition models the same atomic watermark/record operation as
// both existing repositories, including replaying a saved event after an
// identical unfinished attempt. Intent publication remains an executor concern.
func ComponentTransition(candidate, previous model.Alarm, next, watermark model.ComponentAlarmState) ComponentDecision {
	out := ComponentDecision{Alarm: previous, Watermark: watermark}
	if next.MessageID == watermark.MessageID && next.Timestamp == watermark.Timestamp {
		out.Event, out.ReplayEvent = watermark.Event, watermark.Event != ""
		return out
	}
	if !next.Supersedes(watermark) {
		return out
	}
	out.Alarm, out.Event = model.TransitionComponentAlarm(candidate, previous, next)
	if out.Alarm.ID != previous.ID {
		out.Alarm.Version = 0 // The executor assigns the new row's CAS version.
	}
	next.AlarmID, next.Event = out.Alarm.ID, out.Event
	out.Watermark, out.Applied = next, true
	return out
}
