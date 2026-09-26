package opscenter

import (
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
	"iot-platform/internal/model"
)

const deviceNotificationLabel = "torchlink_device_notification"
const deviceDiscardReceiver = "torchlink-device-discard"

func isManagedNotificationRoute(r amRoute) bool {
	if isTestRoute(r) {
		return true
	}
	for _, text := range r.Matchers {
		if m, ok := parseMatcher(text); ok && m.Name == deviceNotificationLabel {
			return true
		}
	}
	return false
}

// The activation boundary lives in the managed route, so it is applied and
// rolled back atomically with the receiver. Replayed historical events are ignored.
func deviceNotificationRoute(route *yaml.Node) (string, int64) {
	if children := mapGet(route, "routes"); children != nil {
		for _, child := range children.Content {
			var r amRoute
			if child.Decode(&r) != nil || r.Receiver == deviceDiscardReceiver {
				continue
			}
			for _, text := range r.Matchers {
				if m, ok := parseMatcher(text); ok && m.Name == deviceNotificationLabel && m.IsEqual && !m.IsRegex {
					since, err := strconv.ParseInt(m.Value, 10, 64)
					if err == nil && since > 0 {
						return r.Receiver, since
					}
				}
			}
		}
	}
	return "", 0
}

func configureDeviceNotifications(route, receivers *yaml.Node, in, current model.OpsNotificationConfig, now time.Time) error {
	receiver := in.DeviceAlarmReceiver
	if receiver != "" {
		var selected *model.OpsReceiver
		for i := range in.Receivers {
			if in.Receivers[i].Name == receiver {
				selected = &in.Receivers[i]
			}
		}
		if selected == nil || len(selected.Emails) == 0 || len(selected.Webhooks) > 0 || len(selected.ReadOnly) > 0 {
			return invalid("deviceAlarmReceiver", "请选择只配置邮件渠道的设备告警接收人")
		}
		for _, email := range selected.Emails {
			if email.SendResolved {
				return invalid("deviceAlarmReceiver", "设备告警邮件不发送恢复通知，请关闭该接收人的恢复通知")
			}
		}
	}
	// Use readable device details for new dedicated mail channels; retain any
	// explicitly supplied custom templates.
	for _, node := range receivers.Content {
		if name := mapGet(node, "name"); name == nil || name.Value != receiver {
			continue
		}
		if emails := mapGet(node, "email_configs"); emails != nil {
			for _, email := range emails.Content {
				headers := mapGet(email, "headers")
				if headers == nil {
					headers = newMap()
					mapSet(email, "headers", headers)
				}
				if mapGet(headers, "Subject") == nil {
					mapSet(headers, "Subject", str("【炬联设备告警】{{ .CommonAnnotations.summary }}"))
				}
				if html := mapGet(email, "html"); html == nil || html.Value == legacyDeviceEmailHTML {
					mapSet(email, "html", str(deviceEmailHTML))
				}
			}
		}
	}
	// A terminal discard route also catches alerts still retained by Alertmanager
	// after disabling/changing the feature; they must never fall through to ops mail.
	children := mapGet(route, "routes")
	if children == nil {
		children = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		mapSet(route, "routes", children)
	}
	managed := []*yaml.Node{}
	if receiver != "" {
		since := current.DeviceAlarmSince
		same := receiver == current.DeviceAlarmReceiver
		for _, r := range in.Receivers {
			if r.Name == receiver && r.OriginalName == current.DeviceAlarmReceiver {
				same = true
			}
		}
		if !same || since == 0 {
			since = now.UnixMilli()
		}
		managed = append(managed, buildRoute(model.OpsRoute{Receiver: receiver,
			Matchers: []model.OpsMatcher{{Name: deviceNotificationLabel, Value: strconv.FormatInt(since, 10), IsEqual: true}, {Name: "trigger_id", Value: ".+", IsEqual: true, IsRegex: true}},
			GroupBy:  []string{"tenant_id", "alarm_id", "trigger_id", deviceNotificationLabel}, GroupWait: "1s", GroupInterval: "1m", RepeatInterval: "48h"}))
	}
	managed = append(managed, buildRoute(model.OpsRoute{Receiver: deviceDiscardReceiver,
		Matchers: []model.OpsMatcher{{Name: deviceNotificationLabel, Value: ".+", IsEqual: true, IsRegex: true}}}))
	children.Content = append(managed, children.Content...)
	discard := newMap()
	mapSet(discard, "name", str(deviceDiscardReceiver))
	receivers.Content = append(receivers.Content, discard)
	return nil
}
