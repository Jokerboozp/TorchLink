package opscenter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func notificationFixture(t *testing.T) (*DeviceNotifications, *fakeAlertmanager, *time.Time) {
	t.Helper()
	svc, am, _ := newAMService(t, `route:
  receiver: ops
receivers:
  - name: ops
  - name: device-mail
    email_configs:
      - to: recipient@example.com
        from: sender@example.com
        smarthost: smtp.example.com:587
        send_resolved: false
`)
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	cfg, _ := svc.NotificationConfig(context.Background())
	cfg.DeviceAlarmReceiver = "device-mail"
	for i := range cfg.Receivers {
		cfg.Receivers[i].OriginalName = cfg.Receivers[i].Name
	}
	if _, err := svc.SaveNotificationConfig(context.Background(), cfg, "test"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	return &DeviceNotifications{service: svc, dir: t.TempDir(), wake: make(chan struct{}, 1), lastPost: map[string]time.Time{}}, am, &now
}
func notificationAlarm(now time.Time) model.Alarm {
	return model.Alarm{ID: "alarm-1", TriggerID: "report-1", LastTriggeredAt: now.UnixMilli(), TenantID: "tenant-1", DeviceID: "device-1", DeviceName: "一楼烟感", ComponentName: "回路 1", ComponentLocation: "大厅", AlarmType: "FIRE", AlarmLevel: "HIGH", Source: "device", Status: "ACTIVE", TriggerCount: 1, FirstTriggeredAt: now.UnixMilli(), Details: map[string]any{"message": map[string]any{"event": map[string]any{"description": "烟雾浓度超限"}, "raw": map[string]any{"secret": "do-not-send"}}}}
}
func enqueueAlarm(t *testing.T, n *DeviceNotifications, a model.Alarm) {
	t.Helper()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if err = n.enqueue(context.Background(), b); err != nil {
		t.Fatal(err)
	}
}
func spoolFiles(t *testing.T, dir, ext string) int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*"+ext))
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}
func TestDeviceNotificationsDistinctAndDurable(t *testing.T) {
	n, am, now := notificationFixture(t)
	ctx := context.Background()
	a := notificationAlarm(*now)
	enqueueAlarm(t, n, a)
	enqueueAlarm(t, n, a)
	a.ID = "alarm-2"
	enqueueAlarm(t, n, a)
	a.TenantID = "tenant-2"
	enqueueAlarm(t, n, a)
	if err := n.flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 3 {
		t.Fatalf("want 3 unique notifications, got %d", len(am.posted))
	}
	identities := map[string]bool{}
	for _, alert := range am.posted {
		labels := alert["labels"].(map[string]any)
		identities[labels["tenant_id"].(string)+labels["alarm_id"].(string)] = true
		detail := alert["annotations"].(map[string]any)["description"].(string)
		for _, want := range []string{"一楼烟感", "回路 1", "大厅", "火警", "烟雾浓度超限"} {
			if !strings.Contains(detail, want) {
				t.Fatalf("missing %s", want)
			}
		}
		if strings.Contains(detail, "do-not-send") {
			t.Fatal("raw content exposed")
		}
	}
	if len(identities) != 3 {
		t.Fatal("events grouped across tenant/alarm identity")
	}
	if err := n.flush(ctx); err != nil || len(am.posted) != 3 {
		t.Fatal("posted again before refresh interval", err)
	}
	// A restarted worker refreshes the same alerts with identical timestamps.
	original, _ := json.Marshal(am.posted)
	restarted := &DeviceNotifications{service: n.service, dir: n.dir, wake: make(chan struct{}, 1), lastPost: map[string]time.Time{}}
	am.posted = nil
	if err := restarted.flush(ctx); err != nil {
		t.Fatal(err)
	}
	if recovered, _ := json.Marshal(am.posted); string(original) != string(recovered) {
		t.Fatal("restart changed notification identity or timestamps")
	}
	// After the window only receipts remain, and they still reject replays.
	*now = now.Add(deviceNotificationWindow)
	am.posted = nil
	if err := restarted.flush(ctx); err != nil {
		t.Fatal(err)
	}
	enqueueAlarm(t, restarted, a)
	if len(am.posted) != 0 || spoolFiles(t, n.dir, ".json") != 0 || spoolFiles(t, n.dir, ".done") != 3 {
		t.Fatal("expired notification retained or replayed")
	}
	*now = now.Add(deviceNotificationRetention + time.Hour)
	if err := restarted.flush(ctx); err != nil || spoolFiles(t, n.dir, ".done") != 0 {
		t.Fatal("receipts not purged after retention", err)
	}
}

type failingNotificationBackend struct {
	*fakeAlertmanager
	failed bool
}

func (f *failingNotificationBackend) PostAlerts(ctx context.Context, alerts []map[string]any) error {
	if f.failed {
		return errors.New("unavailable")
	}
	return f.fakeAlertmanager.PostAlerts(ctx, alerts)
}
func TestDeviceNotificationsRetryAndFiltering(t *testing.T) {
	n, am, now := notificationFixture(t)
	ctx := context.Background()
	failing := &failingNotificationBackend{fakeAlertmanager: am, failed: true}
	n.service.Alerts = failing
	a := notificationAlarm(*now)
	old := a
	old.LastTriggeredAt = now.Add(-time.Minute).UnixMilli()
	enqueueAlarm(t, n, old)
	video := a
	video.Source = "video"
	enqueueAlarm(t, n, video)
	if spoolFiles(t, n.dir, ".json") != 0 {
		t.Fatal("historical or video alarm enqueued")
	}
	enqueueAlarm(t, n, a)
	if err := n.flush(ctx); err == nil {
		t.Fatal("failure hidden")
	}
	// An outage longer than the delivery window must not drop the report.
	*now = now.Add(3 * deviceNotificationWindow)
	failing.failed = false
	if err := n.flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 1 {
		t.Fatal("pending event lost after a long outage")
	}
	if ends, _ := time.Parse(time.RFC3339Nano, am.posted[0]["endsAt"].(string)); !ends.Equal(now.Add(deviceNotificationWindow)) {
		t.Fatal("delivery window must start at the first accepted post", ends)
	}
	cfg, _ := n.service.NotificationConfig(ctx)
	cfg.DeviceAlarmReceiver = ""
	for i := range cfg.Receivers {
		cfg.Receivers[i].OriginalName = cfg.Receivers[i].Name
	}
	if _, err := n.service.SaveNotificationConfig(ctx, cfg, "test"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if err := n.flush(ctx); err != nil {
		t.Fatal(err)
	}
	a.ID = "disabled-alarm"
	a.LastTriggeredAt = now.UnixMilli()
	enqueueAlarm(t, n, a)
	if len(am.posted) != 1 || spoolFiles(t, n.dir, ".json") != 0 {
		t.Fatal("disabled notification still pending or sent")
	}
}

func TestDeviceNotificationManagedRoutesAndValidation(t *testing.T) {
	n, am, now := notificationFixture(t)
	ctx := context.Background()
	cfg, _ := n.service.NotificationConfig(ctx)
	since := cfg.DeviceAlarmSince
	if cfg.DeviceAlarmReceiver != "device-mail" || since == 0 || len(cfg.Route.Routes) != 0 || len(cfg.Receivers) != 2 {
		t.Fatalf("invalid public configuration: %+v", cfg)
	}
	for i := range cfg.Receivers {
		cfg.Receivers[i].OriginalName = cfg.Receivers[i].Name
	}
	*now = now.Add(time.Minute)
	saved, err := n.service.SaveNotificationConfig(ctx, cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if saved.DeviceAlarmSince != since {
		t.Fatal("ordinary edit reset activation time")
	}
	content, _ := os.ReadFile(am.file)
	if strings.Count(string(content), "name: "+deviceDiscardReceiver) != 1 {
		t.Fatal("discard receiver duplicated")
	}
	root, _ := parseAMRoot(content)
	var route amRoute
	_ = mapGet(root, "route").Decode(&route)
	var found bool
	for _, r := range route.Routes {
		if r.Receiver == "device-mail" && !isTestRoute(r) {
			found = true
			if strings.Join(r.GroupBy, ",") != "tenant_id,alarm_id,trigger_id,"+deviceNotificationLabel || r.GroupWait != "1s" || r.RepeatInterval != "48h" {
				t.Fatalf("unsafe grouping: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("no device notification route")
	}
	saved.Receivers[1].Emails[0].SendResolved = true
	if _, err := n.service.SaveNotificationConfig(ctx, saved, "test"); err == nil {
		t.Fatal("receipt must not send misleading recovery emails")
	}
	saved.Receivers[1].Emails[0].SendResolved = false
	saved.DeviceAlarmReceiver = "ops"
	if _, err := n.service.SaveNotificationConfig(ctx, saved, "test"); err == nil {
		t.Fatal("empty receiver must be rejected")
	}
}

func TestBrokenNotificationDoesNotBlockOtherAlarms(t *testing.T) {
	n, am, now := notificationFixture(t)
	if err := os.WriteFile(n.dir+"/000-broken.json", []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	enqueueAlarm(t, n, notificationAlarm(*now))
	if err := n.flush(context.Background()); err == nil {
		t.Fatal("corruption must be reported")
	}
	if len(am.posted) != 1 {
		t.Fatal("one corrupted receipt blocked an unrelated alarm")
	}
}

func TestDeviceRouteCannotStandInForTestRoute(t *testing.T) {
	n, am, _ := notificationFixture(t)
	content, err := os.ReadFile(am.file)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.ReplaceAll(string(content), testLabel, "removed_test_route"))
	if err := os.WriteFile(am.file, content, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := n.service.TestReceiver(context.Background(), "device-mail", "test"); err == nil {
		t.Fatal("missing dedicated test route must not send via the default receiver")
	}
	if len(am.posted) != 0 {
		t.Fatal("test alert posted without its own route")
	}
}

func TestRepeatedActiveAlarmReportsNotifyOnlyOnce(t *testing.T) {
	n, am, now := notificationFixture(t)
	a := notificationAlarm(*now)
	enqueueAlarm(t, n, a)
	// Distinct reports can share a millisecond timestamp; count defines the lifecycle.
	a.TriggerID = "report-2"
	a.TriggerCount = 2
	enqueueAlarm(t, n, a)
	a.Status = "ACKED"
	a.TriggerID = "report-3"
	a.TriggerCount = 3
	enqueueAlarm(t, n, a)
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 1 {
		t.Fatalf("want 1 notification for active/acked alarm, got %d", len(am.posted))
	}
	if am.posted[0]["labels"].(map[string]any)["trigger_id"] != "report-1" {
		t.Fatal("lost first report")
	}
	// A long-lived alarm must not notify again after disk receipts have expired.
	*now = now.Add(deviceNotificationRetention + 2*time.Hour)
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(deviceNotificationRetention + 2*time.Hour)
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if spoolFiles(t, n.dir, ".done") != 0 {
		t.Fatal("receipt not expired")
	}
	a.LastTriggeredAt = now.UnixMilli()
	a.Status = "ACTIVE"
	a.TriggerID = "report-4"
	a.TriggerCount = 4
	restarted := &DeviceNotifications{service: n.service, dir: n.dir, wake: make(chan struct{}, 1), lastPost: map[string]time.Time{}}
	enqueueAlarm(t, restarted, a)
	if err := restarted.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 1 {
		t.Fatal("repeated alarm notified after restart and receipt expiry")
	}
}

func TestDeviceNotificationsCoalesceLegacySpool(t *testing.T) {
	for _, posted := range []bool{false, true} {
		t.Run(fmt.Sprint("posted=", posted), func(t *testing.T) {
			n, am, now := notificationFixture(t)
			cfg, err := n.service.NotificationConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= 3; i++ {
				a := notificationAlarm(*now)
				a.TriggerID = fmt.Sprintf("report-%d", i)
				item := deviceNotification{Epoch: cfg.DeviceAlarmSince, TriggeredAt: now.UnixMilli() + int64(i), Alert: deviceNotificationAlert(a, cfg.DeviceAlarmSince)}
				if posted && i == 2 {
					item.PostedAt = *now
				}
				if err := n.write(fmt.Sprintf("old-%d.json", i), item); err != nil {
					t.Fatal(err)
				}
			}
			if err := n.coalescePending(); err != nil {
				t.Fatal(err)
			}
			if err := n.coalescePending(); err != nil {
				t.Fatal(err)
			}
			if spoolFiles(t, n.dir, ".json") != 1 || spoolFiles(t, n.dir, ".done") != 2 {
				t.Fatal("legacy duplicates retained")
			}
			if err := n.flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := "report-1"
			if posted {
				want = "report-2"
			}
			if len(am.posted) != 1 || am.posted[0]["labels"].(map[string]any)["trigger_id"] != want {
				t.Fatalf("wrong retained notification: %+v", am.posted)
			}
		})
	}
}
