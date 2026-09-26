package opscenter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
	return model.Alarm{ID: "alarm-1", TriggerID: "report-1", LastTriggeredAt: now.UnixMilli(), TenantID: "tenant-1", DeviceID: "device-1", DeviceName: "一楼烟感", ComponentName: "回路 1", ComponentLocation: "大厅", AlarmType: "FIRE", AlarmLevel: "HIGH", Source: "device", Status: "ACTIVE", FirstTriggeredAt: now.UnixMilli(), Details: map[string]any{"message": map[string]any{"event": map[string]any{"description": "烟雾浓度超限"}, "raw": map[string]any{"secret": "do-not-send"}}}}
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
func TestDeviceNotificationsDistinctAndDurable(t *testing.T) {
	n, am, now := notificationFixture(t)
	a := notificationAlarm(*now)
	enqueueAlarm(t, n, a)
	enqueueAlarm(t, n, a)
	a.ID = "alarm-2"
	enqueueAlarm(t, n, a)
	a.TenantID = "tenant-2"
	enqueueAlarm(t, n, a)
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 3 {
		t.Fatalf("want 3 unique notifications, got %d", len(am.posted))
	}
	identities := map[string]bool{}
	for _, alert := range am.posted {
		labels := alert["labels"].(map[string]any)
		key := labels["tenant_id"].(string) + labels["alarm_id"].(string)
		if identities[key] {
			t.Fatal("events grouped across tenant/alarm identity")
		}
		identities[key] = true
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
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 3 {
		t.Fatal("posted again before refresh interval")
	}
	original, _ := json.Marshal(am.posted)
	// A fresh worker recovers the persisted alerts with the same fingerprints and
	// timestamps, allowing Alertmanager to deduplicate even after API restart.
	restarted := &DeviceNotifications{service: n.service, dir: n.dir, wake: make(chan struct{}, 1), lastPost: map[string]time.Time{}}
	am.posted = nil
	if err := restarted.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, _ := json.Marshal(am.posted)
	if string(original) != string(recovered) {
		t.Fatal("restart changed notification identity or timestamps")
	}
	*now = now.Add(49 * time.Hour)
	if err := restarted.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	enqueueAlarm(t, restarted, a)
	files, _ := os.ReadDir(n.dir)
	if len(files) != 0 {
		t.Fatal("expired receipt retained or replayed")
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
	failing := &failingNotificationBackend{fakeAlertmanager: am, failed: true}
	n.service.Alerts = failing
	a := notificationAlarm(*now)
	old := a
	old.LastTriggeredAt = now.Add(-time.Minute).UnixMilli()
	enqueueAlarm(t, n, old)
	video := a
	video.Source = "video"
	enqueueAlarm(t, n, video)
	files, _ := os.ReadDir(n.dir)
	if len(files) != 0 {
		t.Fatal("historical or video alarm enqueued")
	}
	enqueueAlarm(t, n, a)
	if err := n.flush(context.Background()); err == nil {
		t.Fatal("failure hidden")
	}
	failing.failed = false
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 1 {
		t.Fatal("pending event lost")
	}
	cfg, _ := n.service.NotificationConfig(context.Background())
	cfg.DeviceAlarmReceiver = ""
	for i := range cfg.Receivers {
		cfg.Receivers[i].OriginalName = cfg.Receivers[i].Name
	}
	if _, err := n.service.SaveNotificationConfig(context.Background(), cfg, "test"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.ID = "disabled-alarm"
	a.LastTriggeredAt = now.UnixMilli()
	enqueueAlarm(t, n, a)
	if len(am.posted) != 1 {
		t.Fatal("disabled notification still sent")
	}
	files, _ = os.ReadDir(n.dir)
	if len(files) != 1 {
		t.Fatal("disabled notification enqueued")
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

func TestRepeatedActiveAlarmReportsSendSeparately(t *testing.T) {
	n, am, now := notificationFixture(t)
	a := notificationAlarm(*now)
	// The alarm began before notification was enabled; this report is new.
	a.FirstTriggeredAt = now.Add(-time.Hour).UnixMilli()
	enqueueAlarm(t, n, a)
	a.TriggerID = "report-2"
	a.LastTriggeredAt++
	a.Details = map[string]any{"message": map[string]any{"event": map[string]any{"description": "第二次报警内容"}}}
	enqueueAlarm(t, n, a)
	enqueueAlarm(t, n, a)
	if err := n.flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(am.posted) != 2 {
		t.Fatalf("want 2 emails for 2 reports of one active alarm, got %d", len(am.posted))
	}
	triggers := map[string]bool{}
	for _, alert := range am.posted {
		labels := alert["labels"].(map[string]any)
		trigger := labels["trigger_id"].(string)
		triggers[trigger] = true
		if labels["alarm_id"] != a.ID {
			t.Fatal("changed business alarm identity")
		}
		if trigger == "report-2" && !strings.Contains(alert["annotations"].(map[string]any)["description"].(string), "第二次报警内容") {
			t.Fatal("repeated report lost its current details")
		}
	}
	if !triggers["report-1"] || !triggers["report-2"] {
		t.Fatal("reports were merged")
	}
}
