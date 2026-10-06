package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// clearDuration forgets a duration rule's first match; rules without a
// duration never store one, so they cost no query per message.
func (e *Engine) clearDuration(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) error {
	if rule.DurationSeconds <= 0 {
		return nil
	}
	return e.Repo.DeleteRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID)
}

func (e *Engine) durationSatisfied(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) (bool, error) {
	now := e.Clock.Now().Unix()
	since, found, err := e.Repo.GetRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, e.Repo.SaveRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID, now)
	}
	return now-since >= rule.DurationSeconds, nil
}

func (e *Engine) raiseRuleAlarm(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) (model.Alarm, bool, error) {
	now := e.Clock.Now().UnixMilli()
	a := model.Alarm{ID: id("alarm"), TenantID: msg.TenantID, RuleID: rule.ID, TriggerID: msg.MessageID, DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), AlarmType: rule.AlarmType, AlarmLevel: rule.Level, Status: "ACTIVE", Source: "device", CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"), BuildingID: tag(msg, "buildingId", "unknown"), DeviceType: tag(msg, "deviceType", msg.ProductID), AreaID: tag(msg, "areaId", ""), FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, Content: alarmContent(msg, rule.Description, rule.Name), Details: map[string]any{"message": msg, "ruleName": rule.Name}}
	a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)
	a.Location = e.alarmLocation(ctx, msg.TenantID, msg.DeviceID, "")
	saved, created, reportChanged, err := e.upsertReportedAlarm(ctx, a, msg)
	if err != nil {
		return saved, false, err
	}
	if created {
		if e.Metrics != nil {
			e.Metrics.Inc("alarm_trigger_total")
		}
		payload, _ := json.Marshal(saved)
		e.publishEvent(ctx, model.TopicAlarmRaised, saved.ID, saved.MQTTTopic("raised"), payload)
	}
	e.flushOutbox(ctx)
	// Alarm records are deduplicated while ACTIVE/ACKED, but a new matching
	// message must still execute the rule actions. Exact duplicate messages
	// keep the original trigger ID and must not execute actions twice.
	if reportChanged {
		for _, action := range rule.Actions {
			event := model.UIActionEvent{ID: id("ui_action"), TenantID: msg.TenantID, RuleID: rule.ID, AlarmID: saved.ID, DeviceID: msg.DeviceID, Action: action, TriggeredAt: now}
			actionPayload, _ := json.Marshal(event)
			e.publishEvent(ctx, model.TopicUIAction, event.ID, fmt.Sprintf("/iot/ui-action/%s", msg.TenantID), actionPayload)
		}
	}
	return saved, created, nil
}

func (e *Engine) recoverRuleAlarm(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) error {
	// Acknowledged alarms are still open: a recovery report must close them
	// the same way as direct and component alarms, or the device stays ALARM.
	for _, status := range []string{"ACTIVE", "ACKED"} {
		alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: msg.TenantID, DeviceID: msg.DeviceID, Status: status, Limit: 100})
		if err != nil {
			return err
		}
		for _, listed := range alarms {
			if listed.RuleID != rule.ID {
				continue
			}
			a, written, err := e.mutateAlarm(ctx, listed.TenantID, listed.ID, func(a *model.Alarm) (bool, error) {
				if a.Status != "ACTIVE" && a.Status != "ACKED" {
					return false, nil
				}
				a.Status = "RECOVERED"
				a.RecoveredAt = e.Clock.Now().UnixMilli()
				return true, nil
			})
			if err != nil {
				return err
			}
			if written {
				payload, _ := json.Marshal(a)
				e.publishEvent(ctx, model.TopicAlarmRecovered, a.ID, a.MQTTTopic("recovered"), payload)
			}
		}
	}
	return nil
}

// DeleteRule removes a rule and closes any alarms that can no longer be
// recovered by the deleted rule. Historical alarm rows are retained.
//
// Every step leaves a state that a retried delete completes: the rule is
// switched off first so it raises nothing new, its alarms are recovered while
// the rule still exists, and only then the rule and its duration timers are
// removed together.
func (e *Engine) DeleteRule(ctx context.Context, tenant, ruleID string) error {
	rules, err := e.Repo.ListRules(ctx, tenant)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(rules, func(r model.AlarmRule) bool { return r.ID == ruleID })
	if index < 0 {
		return model.ErrNotFound
	}
	if rule := rules[index]; rule.Enabled {
		rule.Enabled = false
		if err = e.Repo.SaveRule(ctx, rule); err != nil {
			return err
		}
	}
	e.RulesChanged(tenant)
	if err = e.closeRuleAlarms(ctx, tenant, ruleID); err != nil {
		return err
	}
	defer e.RulesChanged(tenant)
	return e.Repo.DeleteRule(ctx, tenant, ruleID)
}

// DisableRule clears duration state and closes active/acknowledged alarms
// before a rule is switched off. Historical alarm rows remain available.
func (e *Engine) DisableRule(ctx context.Context, tenant, ruleID string) error {
	defer e.RulesChanged(tenant)
	if err := e.Repo.DeleteRulePendings(ctx, tenant, ruleID); err != nil {
		return err
	}
	return e.closeRuleAlarms(ctx, tenant, ruleID)
}

func (e *Engine) closeRuleAlarms(ctx context.Context, tenant, ruleID string) error {
	const batchSize = 1000
	affectedDevices := make(map[string]struct{})
	for _, status := range []string{"ACTIVE", "ACKED"} {
		offset := 0
		for {
			alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, Status: status, Limit: batchSize, Offset: offset})
			if err != nil {
				return err
			}
			if len(alarms) == 0 {
				break
			}
			changed := false
			for _, listed := range alarms {
				if listed.RuleID != ruleID && !strings.HasPrefix(listed.RuleID, ruleID+":external:") {
					continue
				}
				alarm, written, err := e.mutateAlarm(ctx, listed.TenantID, listed.ID, func(a *model.Alarm) (bool, error) {
					if a.Status != "ACTIVE" && a.Status != "ACKED" {
						return false, nil
					}
					a.Status = "RECOVERED"
					a.RecoveredAt = e.Clock.Now().UnixMilli()
					return true, nil
				})
				if err != nil {
					return err
				}
				if !written {
					continue
				}
				affectedDevices[alarm.DeviceID] = struct{}{}
				payload := mustJSON(alarm)
				e.publishEvent(ctx, model.TopicAlarmRecovered, alarm.ID, alarm.MQTTTopic("recovered"), payload)
				changed = true
			}
			if changed {
				// Updating rows removes them from the status-filtered result set;
				// restart at zero so offset pagination cannot skip the next row.
				offset = 0
				continue
			}
			offset += len(alarms)
		}
	}
	for deviceID := range affectedDevices {
		if err := e.syncDeviceBusinessStatus(ctx, tenant, "", deviceID); err != nil {
			return err
		}
	}
	return nil
}
