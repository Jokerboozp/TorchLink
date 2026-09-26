package opscenter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	// deviceNotificationWindow keeps an accepted alert firing so Alertmanager
	// can retry SMTP across restarts. It starts at the first successful post;
	// records that were never accepted keep retrying without a deadline.
	deviceNotificationWindow = 24 * time.Hour
	// deviceNotificationRetention bounds replay deduplication. Receipts are
	// purged after it, so older source events are not accepted.
	deviceNotificationRetention = 30 * 24 * time.Hour
)

// DeviceNotifications transports the first report of each business alarm.
// Its spool must be on persistent storage, just like Alertmanager data:
// <key>.json is pending or within its window, <key>.done is an empty receipt.
type DeviceNotifications struct {
	service   *Service
	dir       string
	mu        sync.Mutex
	wake      chan struct{}
	lastPost  map[string]time.Time
	lastPurge time.Time
}

type deviceNotification struct {
	Epoch       int64          `json:"epoch"`
	TriggeredAt int64          `json:"triggeredAt"`
	PostedAt    time.Time      `json:"postedAt"`
	Alert       map[string]any `json:"alert"`
}

func (s *Service) StartDeviceNotifications(ctx context.Context, bus ports.EventBus, dir string) error {
	if !configured(s.Alerts) || !configured(s.AMConfig) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	n := &DeviceNotifications{service: s, dir: dir, wake: make(chan struct{}, 1), lastPost: map[string]time.Time{}}
	if err := n.coalescePending(); err != nil {
		return err
	}
	if err := bus.Subscribe(ctx, model.TopicAlarmReported, "device-alarm-notifications", n.enqueue); err != nil {
		return err
	}
	go n.run(ctx)
	return nil
}

func (n *DeviceNotifications) enqueue(ctx context.Context, payload []byte) error {
	var alarm model.Alarm
	if err := json.Unmarshal(payload, &alarm); err != nil {
		return fmt.Errorf("decode device alarm notification: %w", err)
	}
	if alarm.Source != "device" {
		return nil
	}
	// The transactional outbox snapshots the persisted trigger count. Only the
	// first report may notify, even after receipt expiry, restart or a receiver
	// change. ACKED is still the same alarm; recovery/closure creates a new ID
	// with count 1 on the next trigger. Do not use timestamps (they can collide).
	if alarm.TriggerCount != 1 || alarm.Status != "ACTIVE" {
		return nil
	}
	if alarm.ID == "" || alarm.TenantID == "" || alarm.DeviceID == "" || alarm.TriggerID == "" || alarm.LastTriggeredAt <= 0 {
		return fmt.Errorf("device alarm notification missing identity or timestamp")
	}
	// Serialize with configuration activation/rollback. No network send is made
	// on the ingestion path; a successful handler means the event is on disk.
	s := n.service
	s.amMu.Lock()
	defer s.amMu.Unlock()
	cfg, err := s.NotificationConfig(ctx)
	if err != nil {
		return err
	}
	if cfg.DeviceAlarmReceiver == "" || alarm.LastTriggeredAt < cfg.DeviceAlarmSince || alarm.LastTriggeredAt <= s.now().Add(-deviceNotificationRetention).UnixMilli() {
		return nil
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(alarm.TenantID+"\x00"+alarm.ID+"\x00"+alarm.TriggerID)))
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, name := range []string{key + ".json", key + ".done"} {
		if _, err := os.Stat(filepath.Join(n.dir, name)); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	item := deviceNotification{Epoch: cfg.DeviceAlarmSince, TriggeredAt: alarm.LastTriggeredAt, Alert: deviceNotificationAlert(alarm, cfg.DeviceAlarmSince)}
	if err := n.write(key+".json", item); err != nil {
		return err
	}
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
}

// coalescePending upgrades the former per-report spool before subscribing or
// delivering. Prefer an already posted notification (preserving its Alertmanager
// fingerprint and retry window), otherwise the earliest report. Old posted
// duplicates are no longer refreshed and expire within their original window.
func (n *DeviceNotifications) coalescePending() error {
	entries, err := os.ReadDir(n.dir)
	if err != nil {
		return err
	}
	type pending struct {
		name string
		item deviceNotification
	}
	kept := map[string]pending{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(n.dir, entry.Name()))
		if err != nil {
			return err
		}
		var item deviceNotification
		if json.Unmarshal(data, &item) != nil {
			continue // flush reports corrupt records without blocking other alarms.
		}
		labels, _ := item.Alert["labels"].(map[string]any)
		tenant, _ := labels["tenant_id"].(string)
		alarm, _ := labels["alarm_id"].(string)
		if tenant == "" || alarm == "" {
			continue
		}
		key := tenant + "\x00" + alarm
		next := pending{name: entry.Name(), item: item}
		previous, exists := kept[key]
		if !exists {
			kept[key] = next
			continue
		}
		preferNext := item.TriggeredAt < previous.item.TriggeredAt
		if item.PostedAt.IsZero() != previous.item.PostedAt.IsZero() {
			preferNext = !item.PostedAt.IsZero()
		} else if !item.PostedAt.IsZero() && !item.PostedAt.Equal(previous.item.PostedAt) {
			preferNext = item.PostedAt.Before(previous.item.PostedAt)
		}
		discard := next.name
		if preferNext {
			kept[key] = next
			discard = previous.name
		}
		if err := n.finish(discard); err != nil {
			return err
		}
	}
	return nil
}

// write replaces a record via temporary file + sync + rename, so a partial
// record is never acknowledged.
func (n *DeviceNotifications) write(name string, item deviceNotification) error {
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(n.dir, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(n.dir, name))
}

// finish leaves an empty receipt first, so replay deduplication never has a gap.
func (n *DeviceNotifications) finish(name string) error {
	delete(n.lastPost, name)
	if err := os.WriteFile(filepath.Join(n.dir, strings.TrimSuffix(name, ".json")+".done"), nil, 0o600); err != nil {
		return err
	}
	return os.Remove(filepath.Join(n.dir, name))
}

func (n *DeviceNotifications) run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := n.flush(ctx); err != nil && ctx.Err() == nil && n.service.Log != nil {
			n.service.Log.Warn("device alarm notification retry pending", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-n.wake:
		}
	}
}

func (n *DeviceNotifications) flush(ctx context.Context) error {
	entries, err := os.ReadDir(n.dir)
	if err != nil {
		return err
	}
	now := n.service.now()
	purge := now.Sub(n.lastPurge) >= time.Hour
	if purge {
		n.lastPurge = now
	}
	var firstErr error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := entry.Name()
		switch {
		case entry.IsDir():
		case strings.HasSuffix(name, ".json"):
			err = n.deliver(ctx, name)
		case purge && strings.HasSuffix(name, ".done"):
			if info, infoErr := entry.Info(); infoErr == nil && now.Sub(info.ModTime()) > deviceNotificationRetention {
				err = os.Remove(filepath.Join(n.dir, name))
			}
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
		err = nil
	}
	return firstErr
}

func (n *DeviceNotifications) deliver(ctx context.Context, name string) error {
	data, err := os.ReadFile(filepath.Join(n.dir, name))
	if err != nil {
		return err
	}
	var item deviceNotification
	if err := json.Unmarshal(data, &item); err != nil {
		return fmt.Errorf("read notification spool %s: %w", name, err)
	}
	s := n.service
	now := s.now()
	if !item.PostedAt.IsZero() && !now.Before(item.PostedAt.Add(deviceNotificationWindow)) {
		return n.finish(name)
	}
	if now.Sub(n.lastPost[name]) < time.Minute {
		return nil
	}
	s.amMu.Lock()
	cfg, err := s.NotificationConfig(ctx)
	s.amMu.Unlock()
	if err != nil {
		return err
	}
	// Disabling or switching the receiver starts a new epoch; earlier reports
	// are dropped rather than sent to the new channel.
	if cfg.DeviceAlarmReceiver == "" || cfg.DeviceAlarmSince != item.Epoch {
		return n.finish(name)
	}
	first := item.PostedAt.IsZero()
	if first {
		item.PostedAt = now
	}
	item.Alert["startsAt"] = item.PostedAt.UTC().Format(time.RFC3339Nano)
	item.Alert["endsAt"] = item.PostedAt.Add(deviceNotificationWindow).UTC().Format(time.RFC3339Nano)
	if err := s.Alerts.PostAlerts(ctx, []map[string]any{item.Alert}); err != nil {
		return err
	}
	n.lastPost[name] = now
	if first {
		// Persist the window so a restarted worker refreshes the same alert.
		return n.write(name, item)
	}
	return nil
}

func deviceNotificationAlert(a model.Alarm, epoch int64) map[string]any {
	name := a.DeviceName
	if name == "" {
		name = a.DeviceID
	}
	kind := alarmDisplay(a.AlarmType, map[string]string{"SMOKE_DETECTED": "烟雾报警", "DEVICE_FAULT": "设备故障", "DEVICE_OFFLINE": "设备离线", "MANUAL_ALARM": "手动报警", "FIRE": "火警", "FAULT": "故障", "ALARM": "告警", "OFFLINE": "离线", "RECOVERY": "恢复"})
	level := alarmDisplay(a.AlarmLevel, map[string]string{"CRITICAL": "严重", "HIGH": "高", "MEDIUM": "中", "LOW": "低", "WARNING": "警告", "INFO": "提示"})
	annotations := map[string]string{
		"device_name": name, "device_id": a.DeviceID, "alarm_type": kind,
		"alarm_level": level, "alarm_level_code": strings.ToUpper(a.AlarmLevel),
		"occurred_at": time.UnixMilli(a.LastTriggeredAt).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"),
	}
	for key, value := range map[string]string{"component_name": a.ComponentName, "component_location": a.ComponentLocation, "building": a.BuildingID, "area": a.AreaID, "rule_name": notificationText(a.Details["ruleName"])} {
		if value != "" && value != "unknown" {
			annotations[key] = value
		}
	}
	detail := []string{"设备：" + name, "告警类型：" + kind, "告警等级：" + level, "发生时间：" + time.UnixMilli(a.LastTriggeredAt).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05 -07:00")}
	for _, pair := range [][2]string{{"部件", a.ComponentName}, {"部件位置", a.ComponentLocation}, {"建筑", a.BuildingID}, {"区域", a.AreaID}, {"规则", notificationText(a.Details["ruleName"])}} {
		if pair[1] != "" && pair[1] != "unknown" {
			detail = append(detail, pair[0]+"："+pair[1])
		}
	}
	// Whitelist human-readable event fields. Never forward Raw, arbitrary
	// properties or the complete Details payload into email.
	if message, ok := a.Details["message"].(map[string]any); ok {
		if event, ok := message["event"].(map[string]any); ok {
			for _, key := range []string{"description", "message", "content", "name"} {
				if text := notificationText(event[key]); text != "" {
					annotations["alarm_content"] = text
					detail = append(detail, "告警内容："+text)
					break
				}
			}
		}
	}
	detail = append(detail, "设备编号："+a.DeviceID, "告警编号："+a.ID, "租户："+a.TenantID, "报文编号："+a.TriggerID, "同一告警仅通知一次，恢复或关闭后再次告警才重新通知；处理状态请查看平台告警中心。")
	annotations["summary"] = name + " · " + kind + "（" + level + "）"
	annotations["description"] = strings.Join(detail, "\n")
	return map[string]any{
		"labels":      map[string]string{"alertname": "TorchLinkDeviceAlarm", deviceNotificationLabel: strconv.FormatInt(epoch, 10), "tenant_id": a.TenantID, "alarm_id": a.ID, "trigger_id": a.TriggerID},
		"annotations": annotations,
	}
}

func notificationText(value any) string {
	text, _ := value.(string)
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 2000 {
		return string(runes[:2000]) + "…"
	}
	return string(runes)
}
func alarmDisplay(value string, names map[string]string) string {
	if label := names[strings.ToUpper(value)]; label != "" {
		return label
	}
	return value
}
