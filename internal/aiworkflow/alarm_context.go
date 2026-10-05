package aiworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/core"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Alarm analysis context: one block per source, each with its own byte budget
// so a large block cannot crowd out the others. The total stays well below
// the 30 KiB workflow input limit, leaving room for the instructions and
// knowledge evidence.
const (
	alarmContextSiteWindowMs  = 2 * 60 * 60 * 1000
	alarmContextHistoryWindow = 90 * 24 * time.Hour
	alarmContextHistoryScan   = 2000
	alarmContextSiteAlarms    = 20
	alarmContextSimilarAlarms = 20
)

var alarmContextBudgets = map[string]int{
	"alarm":              4 << 10,
	"device":             1536,
	"propertyHistory":    8 << 10,
	"location":           512,
	"siteAlarms":         2560,
	"rule":               1536,
	"dispositionHistory": 1 << 10,
	"similarAlarms":      2560,
	"cameras":            1536,
	"deviceSignals":      1 << 10,
}

// alarmContextOrder is the order of the blocks in the prompt.
var alarmContextOrder = []string{"alarm", "device", "propertyHistory", "location", "siteAlarms", "rule", "dispositionHistory", "similarAlarms", "cameras", "deviceSignals"}

// alarmContext is the verified data an alarm analysis is given.
type alarmContext struct {
	blocks map[string]any
	// omitted names blocks dropped or shortened to fit their budget.
	omitted []string
	// symptoms are short facts, such as "温度 85℃ 持续上升", used to search
	// the knowledge base.
	symptoms []string
}

// buildAlarmContext gathers the analysis context of alarm. Reads go through
// the request's scoped repository, so neighbouring alarms only include devices
// the requester may see. A failed optional read leaves its block out.
func (e *Service) buildAlarmContext(ctx context.Context, alarm model.Alarm) alarmContext {
	c := alarmContext{blocks: map[string]any{}}
	c.blocks["alarm"] = alarmSummary(alarm, true)
	var product *model.Product
	if device, err := e.engine.Repo.GetManagedDevice(ctx, alarm.TenantID, alarm.DeviceID); err == nil {
		block := map[string]any{"id": device.ID, "name": device.Name, "productId": device.ProductID, "deviceRole": device.DeviceRole, "status": device.Status}
		if device.GatewayID != "" {
			block["gatewayId"] = device.GatewayID
		}
		if len(device.Tags) > 0 {
			block["tags"] = device.Tags
		}
		if p, err := e.engine.Repo.GetProduct(ctx, alarm.TenantID, device.ProductID); err == nil {
			product = &p
			block["productName"], block["category"] = p.Name, p.Category
		}
		c.blocks["device"] = block
	}
	var ruleFields []string
	if rule, ok := e.engine.RuleByID(ctx, alarm.TenantID, alarm.RuleID); ok && alarm.RuleID != "" {
		for _, condition := range rule.Conditions {
			ruleFields = append(ruleFields, condition.Field)
		}
		block := map[string]any{"id": rule.ID, "name": rule.Name, "alarmType": rule.AlarmType, "level": rule.Level, "match": rule.Match, "conditions": rule.Conditions}
		if rule.Description != "" {
			block["description"] = rule.Description
		}
		if len(rule.Recovery) > 0 {
			block["recovery"] = rule.Recovery
		}
		if rule.Expression != "" {
			block["expression"] = rule.Expression
		}
		if rule.DurationSeconds > 0 {
			block["durationSeconds"] = rule.DurationSeconds
		}
		c.blocks["rule"] = block
	}
	if history := e.alarmPropertyHistory(ctx, alarm, product, ruleFields); len(history) > 0 {
		c.blocks["propertyHistory"] = history
		c.symptoms = alarmSymptoms(history)
	}
	if alarm.Location != nil {
		c.blocks["location"] = alarm.Location
		if alarms := e.alarmsNearby(ctx, alarm); alarms != nil {
			c.blocks["siteAlarms"] = alarms
		}
	}
	if history := e.alarmDispositionHistory(ctx, alarm); history != nil {
		c.blocks["dispositionHistory"] = history
	}
	if similar, err := e.engine.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: alarm.TenantID, DeviceID: alarm.DeviceID, Summary: true, Limit: alarmContextSimilarAlarms + 1}); err == nil {
		items := []map[string]any{}
		for _, item := range similar {
			if item.ID != alarm.ID && item.AlarmType == alarm.AlarmType && len(items) < alarmContextSimilarAlarms {
				items = append(items, alarmSummary(item, false))
			}
		}
		c.blocks["similarAlarms"] = items
	}
	if cameras := alarmCameraContext(alarm); cameras != nil {
		c.blocks["cameras"] = cameras
	}
	if signals := e.deviceSignalsContext(ctx, alarm.TenantID, alarm.DeviceID); signals != nil {
		c.blocks["deviceSignals"] = signals
	}
	c.fitBudgets()
	return c
}

// payload returns the context blocks in prompt order.
func (c alarmContext) payload() []map[string]any {
	out := []map[string]any{}
	for _, name := range alarmContextOrder {
		if block, ok := c.blocks[name]; ok {
			out = append(out, map[string]any{"contextType": name, "data": block})
		}
	}
	if len(c.omitted) > 0 {
		out = append(out, map[string]any{"contextType": "omitted", "data": "以下数据超出长度预算已截断或省略：" + strings.Join(c.omitted, "、")})
	}
	return out
}

// fitBudgets shortens list blocks from the end and drops any other block that
// exceeds its budget.
func (c *alarmContext) fitBudgets() {
	for _, name := range alarmContextOrder {
		block, ok := c.blocks[name]
		if !ok {
			continue
		}
		budget := alarmContextBudgets[name]
		if jsonSize(block) <= budget {
			continue
		}
		c.omitted = append(c.omitted, name)
		switch list := block.(type) {
		case []map[string]any:
			for len(list) > 0 && jsonSize(list) > budget {
				list = list[:len(list)-1]
			}
			c.blocks[name] = list
		case map[string]any:
			if items, ok := list["items"].([]map[string]any); ok {
				for len(items) > 0 && jsonSize(list) > budget {
					items = items[:len(items)-1]
					list["items"] = items
				}
				if jsonSize(list) <= budget {
					continue
				}
			}
			delete(c.blocks, name)
		default:
			delete(c.blocks, name)
		}
	}
}

func jsonSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 1 << 30
	}
	return len(b)
}

// alarmSummary keeps the facts of an alarm without its raw payloads. With
// details, small detail values are kept for the alarm being analysed.
func alarmSummary(a model.Alarm, details bool) map[string]any {
	out := map[string]any{"alarmId": a.ID, "deviceId": a.DeviceID, "alarmType": a.AlarmType, "alarmLevel": a.AlarmLevel, "status": a.Status, "source": a.Source,
		"firstTriggeredAt": a.FirstTriggeredAt, "lastTriggeredAt": a.LastTriggeredAt, "triggerCount": a.TriggerCount}
	for key, value := range map[string]string{"deviceName": a.DeviceName, "content": a.Content, "deviceType": a.DeviceType, "componentId": a.ComponentID, "componentName": a.ComponentName, "componentLocation": a.ComponentLocation} {
		if value != "" {
			out[key] = value
		}
	}
	if a.Confidence > 0 {
		out["confidence"] = a.Confidence
	}
	if a.RecoveredAt > 0 {
		out["recoveredAt"] = a.RecoveredAt
	}
	if a.Disposition != nil {
		out["disposition"] = map[string]any{"result": a.Disposition.Result, "verifiedAt": a.Disposition.VerifiedAt}
	}
	if details && len(a.Details) > 0 {
		kept := map[string]any{}
		for key, value := range a.Details {
			if key == "videoEvent" || key == "latestVideoEvent" || key == "videoConfirmation" {
				continue
			}
			if jsonSize(value) <= 512 {
				kept[key] = value
			}
		}
		if len(kept) > 0 {
			out["details"] = kept
		}
	}
	return out
}

// alarmsNearby lists alarms of other devices on the same floor (or building)
// within two hours before the alarm, newest first.
func (e *Service) alarmsNearby(ctx context.Context, alarm model.Alarm) map[string]any {
	locator, ok := e.engine.Locator.(ports.NearbyDeviceLocator)
	if !ok {
		return nil
	}
	devices := locator.DevicesNear(ctx, alarm.TenantID, *alarm.Location, alarm.DeviceID)
	scope := "同楼层"
	if alarm.Location.FloorID == "" {
		scope = "同建筑"
	}
	out := map[string]any{"scope": scope, "devicesNearby": len(devices), "windowMinutes": alarmContextSiteWindowMs / 60000, "items": []map[string]any{}}
	if len(devices) == 0 {
		return out
	}
	filter := ports.AlarmFilter{TenantID: alarm.TenantID, DeviceIDs: devices, Start: alarm.LastTriggeredAt - alarmContextSiteWindowMs, End: alarm.LastTriggeredAt, Summary: true, Limit: alarmContextSiteAlarms}
	alarms, err := e.engine.Repo.ListAlarms(ctx, filter)
	if err != nil {
		return nil
	}
	items := make([]map[string]any, 0, len(alarms))
	for _, item := range alarms {
		summary := alarmSummary(item, false)
		if item.Location != nil {
			summary["pointName"] = item.Location.PointName
		}
		items = append(items, summary)
	}
	out["items"] = items
	if total, err := e.engine.Repo.CountAlarms(ctx, filter); err == nil {
		out["total"] = total
	}
	return out
}

var errAlarmScanLimit = errors.New("alarm scan limit")

// alarmDispositionHistory counts how this device's earlier alarms of the same
// type were verified on site in the last 90 days.
func (e *Service) alarmDispositionHistory(ctx context.Context, alarm model.Alarm) map[string]any {
	since := time.UnixMilli(alarm.LastTriggeredAt).Add(-alarmContextHistoryWindow).UnixMilli()
	results := map[string]int{}
	total, verified, scanned := 0, 0, 0
	err := e.engine.Repo.EachAlarm(ctx, ports.AlarmFilter{TenantID: alarm.TenantID, DeviceID: alarm.DeviceID, Start: since, End: alarm.LastTriggeredAt, Summary: true}, func(a model.Alarm) error {
		if scanned++; scanned > alarmContextHistoryScan {
			return errAlarmScanLimit
		}
		if a.ID == alarm.ID || a.AlarmType != alarm.AlarmType {
			return nil
		}
		total++
		if a.Disposition != nil && a.Disposition.Result != "" {
			verified++
			results[a.Disposition.Result]++
		}
		return nil
	})
	if err != nil && !errors.Is(err, errAlarmScanLimit) {
		return nil
	}
	out := map[string]any{"windowDays": int(alarmContextHistoryWindow.Hours() / 24), "sameTypeAlarms": total, "verified": verified, "byResult": results}
	if errors.Is(err, errAlarmScanLimit) {
		out["partial"] = fmt.Sprintf("仅统计最近 %d 条告警", alarmContextHistoryScan)
	}
	return out
}

// alarmCameraContext lists the alarm's cameras and the video events attached
// to it, without snapshot or clip addresses.
func alarmCameraContext(alarm model.Alarm) map[string]any {
	cameras := []map[string]any{}
	for _, camera := range alarm.Cameras {
		cameras = append(cameras, map[string]any{"cameraId": camera.CameraID, "cameraName": camera.CameraName, "point": camera.CameraPoint, "building": camera.Building, "floor": camera.Floor, "room": camera.Room, "enabled": camera.Enabled})
	}
	events := []map[string]any{}
	for _, slot := range []string{"videoEvent", "latestVideoEvent", "videoConfirmation"} {
		value, ok := alarm.Details[slot]
		if !ok {
			continue
		}
		var event model.VideoAlarmEvent
		if b, err := json.Marshal(value); err != nil || json.Unmarshal(b, &event) != nil || event.EventID == "" && event.CameraID == "" {
			continue
		}
		events = append(events, map[string]any{"slot": slot, "cameraId": event.CameraID, "alarmType": event.AlarmType, "alarmName": event.AlarmName, "confidence": event.Confidence, "eventTime": event.EventTime, "hasSnapshot": event.SnapshotURL != "", "hasClip": event.VideoClipURL != ""})
	}
	if len(cameras) == 0 && len(events) == 0 {
		return nil
	}
	return map[string]any{"cameras": cameras, "videoEvents": events}
}

// alarmSymptoms describes the measurements behind an alarm for the knowledge
// search: properties beyond their thresholds or valid range first, each with
// its latest value, unit and recent trend.
func alarmSymptoms(history []map[string]any) []string {
	type symptom struct {
		text     string
		priority int
	}
	list := []symptom{}
	for _, item := range history {
		latest, _ := item["latest"].(map[string]any)
		value, ok := historyNumber(latest["value"])
		if !ok {
			continue
		}
		name, _ := item["name"].(string)
		if name == "" {
			name, _ = item["property"].(string)
		}
		unit, _ := item["unit"].(string)
		text := fmt.Sprintf("%s %g%s", name, value, unit)
		priority := 2
		if high, ok := item["alarmHigh"].(float64); ok && value >= high {
			text, priority = text+" 超过告警阈值", 0
		} else if low, ok := item["alarmLow"].(float64); ok && value <= low {
			text, priority = text+" 低于告警阈值", 0
		} else if maximum, ok := item["max"].(float64); ok && value > maximum {
			priority = 1
		} else if minimum, ok := item["min"].(float64); ok && value < minimum {
			priority = 1
		}
		if stats, ok := item["stats1h"].(map[string]any); ok {
			if mean, ok := stats["mean"].(float64); ok && mean != 0 {
				switch change := (value - mean) / abs(mean); {
				case change > 0.1:
					text += " 持续上升"
				case change < -0.1:
					text += " 持续下降"
				}
			}
		}
		list = append(list, symptom{text, priority})
	}
	out := []string{}
	for priority := 0; priority <= 2 && len(out) < 3; priority++ {
		for _, s := range list {
			if s.priority == priority && len(out) < 3 {
				out = append(out, s.text)
			}
		}
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// deviceSignalsContext lists a device's current signals for AI analysis.
func (e *Service) deviceSignalsContext(ctx context.Context, tenant, device string) []map[string]any {
	if e.engine.DeviceSignals == nil {
		return nil
	}
	signals, err := e.engine.DeviceSignals.ListDeviceSignals(ctx, tenant, []string{device}, 10)
	if err != nil || len(signals) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(signals))
	for _, s := range signals {
		item := map[string]any{"signalType": s.SignalType, "name": core.SignalTypeNames[s.SignalType], "strength": s.Strength, "evidence": s.Evidence, "windowEnd": s.WindowEnd}
		if s.Property != "" {
			item["property"] = s.Property
		}
		out = append(out, item)
	}
	return out
}
