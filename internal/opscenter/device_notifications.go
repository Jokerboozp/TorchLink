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

// DeviceNotifications transports per-report receipt events, not business alarm
// state. Its spool must be on persistent storage, just like Alertmanager data.
// Stable identity and timestamps let Alertmanager deduplicate retries/restarts.
type DeviceNotifications struct {
	service  *Service
	dir      string
	mu       sync.Mutex
	wake     chan struct{}
	lastPost map[string]time.Time
}

type deviceNotification struct {
	Epoch       int64          `json:"epoch"`
	TriggeredAt int64          `json:"triggeredAt"`
	EndsAt      time.Time      `json:"endsAt"`
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
	now := s.now()
	if cfg.DeviceAlarmReceiver == "" || alarm.LastTriggeredAt < cfg.DeviceAlarmSince || alarm.LastTriggeredAt <= now.Add(-48*time.Hour).UnixMilli() {
		return nil
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(alarm.TenantID+"\x00"+alarm.ID+"\x00"+alarm.TriggerID)))
	file := filepath.Join(n.dir, key+".json")
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, err := os.Stat(file); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	ends := now.Add(24 * time.Hour)
	item := deviceNotification{Epoch: cfg.DeviceAlarmSince, TriggeredAt: alarm.LastTriggeredAt, EndsAt: ends,
		Alert: deviceNotificationAlert(alarm, cfg.DeviceAlarmSince, now, ends)}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	// A temporary file + sync + rename avoids acknowledging a partial record.
	tmp, err := os.CreateTemp(n.dir, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return err
	}
	select {
	case n.wake <- struct{}{}:
	default:
	}
	return nil
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
	var firstErr error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := n.deliver(ctx, entry.Name()); err != nil && firstErr == nil {
			firstErr = err
		}
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
	// Keep expired records long enough to reject replay, without retaining device
	// details indefinitely. Older source events are also rejected at enqueue.
	if now.UnixMilli() > item.TriggeredAt+(48*time.Hour).Milliseconds() && now.After(item.EndsAt) {
		delete(n.lastPost, name)
		return os.Remove(filepath.Join(n.dir, name))
	}
	labels, _ := item.Alert["labels"].(map[string]any)
	if labels["trigger_id"] == nil {
		return nil
	}
	if !now.Before(item.EndsAt) || now.Sub(n.lastPost[name]) < time.Minute {
		return nil
	}
	s.amMu.Lock()
	cfg, err := s.NotificationConfig(ctx)
	s.amMu.Unlock()
	if err != nil {
		return err
	}
	if cfg.DeviceAlarmReceiver == "" || cfg.DeviceAlarmSince != item.Epoch {
		return nil
	}
	if err := s.Alerts.PostAlerts(ctx, []map[string]any{item.Alert}); err != nil {
		return err
	}
	n.lastPost[name] = now
	return nil
}

func deviceNotificationAlert(a model.Alarm, epoch int64, start, end time.Time) map[string]any {
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
	detail = append(detail, "设备编号："+a.DeviceID, "告警编号："+a.ID, "租户："+a.TenantID, "报文编号："+a.TriggerID, "本次报警上报单独通知，确认与恢复状态请查看平台告警中心。")
	annotations["summary"] = name + " · " + kind + "（" + level + "）"
	annotations["description"] = strings.Join(detail, "\n")
	return map[string]any{
		"labels":      map[string]string{"alertname": "TorchLinkDeviceAlarm", deviceNotificationLabel: strconv.FormatInt(epoch, 10), "tenant_id": a.TenantID, "alarm_id": a.ID, "trigger_id": a.TriggerID},
		"annotations": annotations,
		"startsAt":    start.UTC().Format(time.RFC3339Nano), "endsAt": end.UTC().Format(time.RFC3339Nano),
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
