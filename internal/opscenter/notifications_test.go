package opscenter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type fakeAlertmanager struct {
	ports.AlertmanagerBackend
	file    string
	posted  []map[string]any
	reloads int
}

func (f *fakeAlertmanager) Configured() bool { return true }
func (f *fakeAlertmanager) Status(context.Context) model.OpsComponentStatus {
	return model.OpsComponentStatus{ID: "alertmanager", Name: "Alertmanager", Configured: true, State: "ok"}
}

// Reload fails like Alertmanager does when a route names a missing receiver.
func (f *fakeAlertmanager) Reload(context.Context) error {
	f.reloads++
	content, _ := os.ReadFile(f.file)
	var cfg struct {
		Route     amRoute `yaml:"route"`
		Receivers []struct {
			Name string `yaml:"name"`
		} `yaml:"receivers"`
	}
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return &ports.OpsUpstreamError{Status: 500, Message: err.Error()}
	}
	names := map[string]bool{}
	for _, r := range cfg.Receivers {
		names[r.Name] = true
	}
	var check func(amRoute) error
	check = func(r amRoute) error {
		if r.Receiver != "" && !names[r.Receiver] {
			return &ports.OpsUpstreamError{Status: 500, Message: `undefined receiver "` + r.Receiver + `" used in route`}
		}
		for _, c := range r.Routes {
			if err := check(c); err != nil {
				return err
			}
		}
		return nil
	}
	if strings.Contains(string(content), "REJECT") {
		return &ports.OpsUpstreamError{Status: 500, Message: "bad config"}
	}
	return check(cfg.Route)
}

func (f *fakeAlertmanager) PostAlerts(_ context.Context, alerts []map[string]any) error {
	f.posted = append(f.posted, alerts...)
	return nil
}

const baseAMConfig = `global:
  resolve_timeout: 5m
route:
  receiver: ops
  group_by: [alertname]
  routes:
    - receiver: oncall
      matchers: ['severity="critical"']
receivers:
  - name: ops
    webhook_configs:
      - url: https://hooks.example.com/secret-token
        send_resolved: true
        tls_config:
          insecure_skip_verify: false
  - name: oncall
    email_configs:
      - to: oncall@example.com
        smarthost: smtp.example.com:587
        auth_username: bot
        auth_password: s3cret
    wechat_configs:
      - corp_id: x
inhibit_rules:
  - source_matchers: ['severity="critical"']
    target_matchers: ['severity="warning"']
`

func newAMService(t *testing.T, content string) (*Service, *fakeAlertmanager, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "alertmanager.yml")
	if err := os.WriteFile(file, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	am := &fakeAlertmanager{file: file}
	return &Service{Alerts: am, AMConfig: observability.NewFileStore(file, filepath.Join(dir, "state"), 0o640)}, am, file
}

func TestNotificationConfigMasksSecrets(t *testing.T) {
	svc, _, _ := newAMService(t, baseAMConfig)
	cfg, err := svc.NotificationConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Writable || cfg.Route.Receiver != "ops" || len(cfg.Route.Routes) != 1 || cfg.InhibitRules != 1 {
		t.Fatalf("cfg = %+v", cfg)
	}
	webhook := cfg.Receivers[0].Webhooks[0]
	if !webhook.URL.Set || webhook.URL.Hint != "https://hooks.example.com/…" || webhook.URL.Value != "" {
		t.Fatalf("webhook secret leaked or missing: %+v", webhook)
	}
	email := cfg.Receivers[1].Emails[0]
	if !email.AuthPassword.Set || email.AuthPassword.Value != "" || cfg.Receivers[1].ReadOnly[0] != "wechat_configs" {
		t.Fatalf("email = %+v readOnly = %v", email, cfg.Receivers[1].ReadOnly)
	}
}

func TestSaveNotificationConfigKeepsSecretsAndUnknownFields(t *testing.T) {
	svc, am, file := newAMService(t, baseAMConfig)
	cfg, _ := svc.NotificationConfig(context.Background())
	cfg.Receivers[0].OriginalName = "ops"
	cfg.Receivers[1].OriginalName = "oncall"
	cfg.Receivers[1].Emails[0].To = "duty@example.com"
	cfg.Receivers[0].Webhooks[0].BearerToken = model.OpsSecret{Mode: "replace", Value: "tok-123"}
	cfg.Route.GroupWait = "45s"
	saved, err := svc.SaveNotificationConfig(context.Background(), cfg, "ops@t")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(file)
	text := string(content)
	for _, want := range []string{"https://hooks.example.com/secret-token", "auth_password: s3cret", "insecure_skip_verify: false", "corp_id: x", "credentials: tok-123", "duty@example.com", "resolve_timeout: 5m", "inhibit_rules", "group_wait: 45s", testLabel} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config lost %q:\n%s", want, text)
		}
	}
	if am.reloads != 1 || saved.UpdatedBy != "ops@t" || len(saved.Route.Routes) != 1 {
		t.Fatalf("reloads = %d saved = %+v", am.reloads, saved)
	}
	if err := svc.TestReceiver(context.Background(), "ops", "ops@t"); err != nil {
		t.Fatal(err)
	}
	if labels := am.posted[0]["labels"].(map[string]string); labels[testLabel] != "ops" {
		t.Fatalf("test alert labels = %v", labels)
	}
}

func TestSaveNotificationConfigClearsAndRollsBack(t *testing.T) {
	svc, am, file := newAMService(t, baseAMConfig)
	cfg, _ := svc.NotificationConfig(context.Background())
	stale := cfg.Revision
	cfg.Receivers[0].OriginalName, cfg.Receivers[1].OriginalName = "ops", "oncall"
	cfg.Receivers[1].Emails[0].AuthPassword = model.OpsSecret{Mode: "clear"}
	if _, err := svc.SaveNotificationConfig(context.Background(), cfg, "x"); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(file); strings.Contains(string(content), "s3cret") {
		t.Fatal("cleared password must be removed")
	}
	cfg.Revision = stale
	if _, err := svc.SaveNotificationConfig(context.Background(), cfg, "x"); !errors.Is(err, ports.ErrOpsConflict) {
		t.Fatalf("stale revision must conflict, got %v", err)
	}
	current, _ := svc.NotificationConfig(context.Background())
	before, _ := os.ReadFile(file)
	current.Receivers[0].OriginalName, current.Receivers[1].OriginalName = "ops", "oncall"
	current.Receivers[0].Webhooks[0].URL = model.OpsSecret{Mode: "replace", Value: "https://REJECT.example.com/"}
	_, err := svc.SaveNotificationConfig(context.Background(), current, "x")
	var apply *ApplyError
	if !errors.As(err, &apply) || !apply.RolledBack || am.reloads < 3 {
		t.Fatalf("expected rollback, got %v", err)
	}
	after, _ := os.ReadFile(file)
	if string(stripHeader(after)) != string(stripHeader(before)) {
		t.Fatal("rejected config must be restored")
	}
}

func TestNotificationValidation(t *testing.T) {
	svc, _, _ := newAMService(t, baseAMConfig)
	cfg, _ := svc.NotificationConfig(context.Background())
	cfg.Receivers[0].OriginalName, cfg.Receivers[1].OriginalName = "ops", "oncall"
	bad := cfg
	bad.Receivers = cfg.Receivers[:1]
	var v *ValidationError
	if _, err := svc.SaveNotificationConfig(context.Background(), bad, "x"); !errors.As(err, &v) {
		t.Fatalf("removing a receiver still used by a route must fail, got %v", err)
	}
	newHook := cfg
	newHook.Receivers = append([]model.OpsReceiver{}, cfg.Receivers...)
	newHook.Receivers[0].Webhooks = append(newHook.Receivers[0].Webhooks, model.OpsWebhookReceiver{URL: model.OpsSecret{Mode: "keep"}})
	if _, err := svc.SaveNotificationConfig(context.Background(), newHook, "x"); !errors.As(err, &v) {
		t.Fatalf("new webhook without URL must fail, got %v", err)
	}
	route := cfg
	route.Route.Routes = []model.OpsRoute{{Receiver: "ops"}}
	if _, err := svc.SaveNotificationConfig(context.Background(), route, "x"); !errors.As(err, &v) {
		t.Fatalf("child route without matcher must fail, got %v", err)
	}
}

func TestUnsupportedRouteFieldsMakeRouteReadOnly(t *testing.T) {
	content := strings.Replace(baseAMConfig, "  group_by: [alertname]\n", "  group_by: [alertname]\n  active_time_intervals: [office]\n", 1)
	svc, _, file := newAMService(t, content)
	cfg, err := svc.NotificationConfig(context.Background())
	if err != nil || cfg.Writable {
		t.Fatalf("route with unsupported fields must be read-only: %+v %v", cfg, err)
	}
	cfg.Receivers[0].OriginalName, cfg.Receivers[1].OriginalName = "ops", "oncall"
	cfg.Receivers[1].Emails[0].To = "new@example.com"
	if _, err := svc.SaveNotificationConfig(context.Background(), cfg, "x"); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(file)
	if !strings.Contains(string(saved), "active_time_intervals") || !strings.Contains(string(saved), "new@example.com") {
		t.Fatalf("receiver edits must keep the read-only route intact:\n%s", saved)
	}
}

func TestRepeatedReceiverTestsHaveIndependentNotificationGroups(t *testing.T) {
	svc, am, file := newAMService(t, baseAMConfig)
	cfg, err := svc.NotificationConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Receivers {
		cfg.Receivers[i].OriginalName = cfg.Receivers[i].Name
	}
	if _, err := svc.SaveNotificationConfig(context.Background(), cfg, "test"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := svc.TestReceiver(context.Background(), "ops", "test"); err != nil {
			t.Fatal(err)
		}
	}
	first := am.posted[0]["labels"].(map[string]string)
	second := am.posted[1]["labels"].(map[string]string)
	if first[testIDLabel] == "" || first[testIDLabel] == second[testIDLabel] {
		t.Fatal("repeated tests must not share Alertmanager's deduplication identity")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Route amRoute `yaml:"route"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range config.Route.Routes {
		if r.Receiver == "ops" && isTestRoute(r) {
			for _, group := range r.GroupBy {
				if group == testIDLabel {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("test IDs must create separate notification groups")
	}
	// Older route configuration requires an explicit save so tests cannot be
	// silently merged into one notification after upgrading the API.
	if err := os.WriteFile(file, []byte(strings.ReplaceAll(string(data), "      - "+testIDLabel+"\n", "")), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := svc.TestReceiver(context.Background(), "ops", "test"); err == nil {
		t.Fatal("legacy test grouping should require a config refresh")
	}
}

func TestDeviceEmailTemplate(t *testing.T) {
	now := time.Date(2026, 9, 26, 3, 0, 0, 0, time.UTC)
	a := notificationAlarm(now)
	a.BuildingID, a.AreaID = "A 栋", "一层公共区域"
	alert := deviceNotificationAlert(a, 1)
	tmpl, err := template.New("email").Parse(deviceEmailHTML)
	if err != nil {
		t.Fatal(err)
	}
	render := func() string {
		t.Helper()
		var b bytes.Buffer
		if err := tmpl.Execute(&b, map[string]any{"CommonAnnotations": alert["annotations"], "CommonLabels": alert["labels"]}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	body := render()
	for _, want := range []string{"一楼烟感", "火警", "告警等级 · 高", "2026-09-26 11:00:00", "烟雾浓度超限", "大厅", "A 栋", "alarm-1", "report-1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing mail detail %q", want)
		}
	}
	if strings.Contains(body, "do-not-send") || strings.Contains(body, "<no value>") {
		t.Fatal("unexpected mail content")
	}
	if file := os.Getenv("IOT_TEST_EMAIL_PREVIEW"); file != "" {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	annotations := alert["annotations"].(map[string]string)
	annotations["device_name"] = `<img src=x onerror="alert(1)">`
	annotations["alarm_content"] = `<script>alert(1)</script>`
	body = render()
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("unescaped device-provided HTML")
	}
	alert["annotations"] = map[string]string{"summary": "通知测试", "description": "这是一封渠道测试邮件"}
	body = render()
	if !strings.Contains(body, "这是一封渠道测试邮件") || strings.Contains(body, "<no value>") || strings.Contains(body, "位置与来源") {
		t.Fatal("generic test or older notifications did not render cleanly")
	}
}

func TestDeviceEmailTemplateMigrationKeepsCustomHTML(t *testing.T) {
	for _, tc := range []struct{ name, current, want string }{
		{"legacy", legacyDeviceEmailHTML, deviceEmailHTML},
		{"custom", "<p>My custom notification</p>", "<p>My custom notification</p>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, am, _ := notificationFixture(t)
			data, err := os.ReadFile(am.file)
			if err != nil {
				t.Fatal(err)
			}
			root, err := parseAMRoot(data)
			if err != nil {
				t.Fatal(err)
			}
			for _, receiver := range mapGet(root, "receivers").Content {
				if mapGet(receiver, "name").Value == "device-mail" {
					mapSet(mapGet(receiver, "email_configs").Content[0], "html", str(tc.current))
				}
			}
			// Encode through the same YAML node representation as production.
			var buf bytes.Buffer
			encoder := yaml.NewEncoder(&buf)
			if err := encoder.Encode(root); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(am.file, buf.Bytes(), 0o640); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			cfg, err := n.service.NotificationConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for i := range cfg.Receivers {
				cfg.Receivers[i].OriginalName = cfg.Receivers[i].Name
			}
			if _, err := n.service.SaveNotificationConfig(ctx, cfg, "test"); err != nil {
				t.Fatal(err)
			}
			data, _ = os.ReadFile(am.file)
			root, _ = parseAMRoot(data)
			for _, receiver := range mapGet(root, "receivers").Content {
				if mapGet(receiver, "name").Value == "device-mail" && mapGet(mapGet(receiver, "email_configs").Content[0], "html").Value != tc.want {
					t.Fatal("incorrect template migration")
				}
			}
		})
	}
}

func notificationFixture(t *testing.T) (*DeviceNotifications, *fakeAlertmanager, *time.Time) {
	t.Helper()
	return notificationFixtureAt(t, time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC))
}

// notificationFixtureAt drives the service from a fake clock starting at start,
// independent of the wall clock.
func notificationFixtureAt(t *testing.T, start time.Time) (*DeviceNotifications, *fakeAlertmanager, *time.Time) {
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
	now := start
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
	for _, start := range []time.Time{
		time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2090, 6, 15, 23, 0, 0, 0, time.UTC),
	} {
		t.Run(start.Format("2006-01-02"), func(t *testing.T) {
			n, am, now := notificationFixtureAt(t, start)
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
			// Retention counts from the receipt time on the service clock, not the
			// wall clock, so the fixture dates are chosen far from the real time.
			*now = now.Add(deviceNotificationRetention - time.Hour)
			if err := restarted.flush(ctx); err != nil || spoolFiles(t, n.dir, ".done") != 3 {
				t.Fatal("receipts purged before retention", err)
			}
			*now = now.Add(2 * time.Hour)
			if err := restarted.flush(ctx); err != nil || spoolFiles(t, n.dir, ".done") != 0 {
				t.Fatal("receipts not purged after retention", err)
			}
		})
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

type notificationClock struct{ at time.Time }

func (c notificationClock) Now() time.Time { return c.at }

func TestDeviceNotificationAlarmLifecycle(t *testing.T) {
	for _, kind := range []string{"direct", "rule", "component"} {
		for _, status := range []string{"RECOVERED", "CLOSED"} {
			t.Run(kind+"/"+status, func(t *testing.T) {
				n, am, now := notificationFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				bus := local.NewBus()
				defer func() { cancel(); _ = bus.Close() }()
				repo := memory.NewRepository()
				engine := core.New(repo, nil, bus, local.NewRealtime(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
				engine.Clock = notificationClock{*now}
				delivered := make(chan string, 8)
				if err := bus.Subscribe(ctx, model.TopicAlarmReported, "mail-test", func(ctx context.Context, payload []byte) error {
					if err := n.enqueue(ctx, payload); err != nil {
						return err
					}
					var alarm model.Alarm
					if err := json.Unmarshal(payload, &alarm); err != nil {
						return err
					}
					delivered <- alarm.TriggerID
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if err := engine.Start(ctx); err != nil {
					t.Fatal(err)
				}
				if kind == "rule" {
					if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r", TenantID: "t", Name: "烟雾规则", Enabled: true, AlarmType: "FIRE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "smoke", Operator: "eq", Value: true}}}); err != nil {
						t.Fatal(err)
					}
				}
				send := func(i int) {
					t.Helper()
					msg := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: fmt.Sprintf("m%d", i), Timestamp: now.UnixMilli() + int64(i), MessageType: model.AlarmReport, Properties: map[string]any{"smoke": true}, Event: map[string]any{"alarmType": "FIRE", "description": "烟雾报警"}}
					if kind == "component" {
						msg.MessageType = model.StateChange
						msg.Event["components"] = []model.ComponentStatus{{ID: "c", Name: "探测器", Alarms: map[string]bool{"FIRE": true}}}
					}
					b, err := json.Marshal(msg)
					if err != nil {
						t.Fatal(err)
					}
					if err := bus.Publish(ctx, model.TopicDeviceBusiness, model.DeviceKey(msg.TenantID, msg.DeviceID), b); err != nil {
						t.Fatal(err)
					}
					// The outbox relay may deliver on its background goroutine.
					select {
					case triggerID := <-delivered:
						if triggerID != msg.MessageID {
							t.Fatalf("want report %s, got %s", msg.MessageID, triggerID)
						}
					case <-time.After(2 * time.Second):
						t.Fatalf("alarm report %s was not delivered", msg.MessageID)
					}
				}
				flush := func(want int) {
					t.Helper()
					if err := n.flush(ctx); err != nil {
						t.Fatal(err)
					}
					if len(am.posted) != want {
						t.Fatalf("want %d notifications, got %d", want, len(am.posted))
					}
				}
				send(1)
				send(2)
				flush(1)
				alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE"})
				if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 2 {
					t.Fatalf("incorrect active alarm: %+v %v", alarms, err)
				}
				originalID := alarms[0].ID
				if _, err := engine.SetAlarmStatus(ctx, "t", originalID, "ACKED", "test"); err != nil {
					t.Fatal(err)
				}
				send(3)
				flush(1)
				if status == "CLOSED" {
					if _, err := engine.VerifyAlarm(ctx, "t", originalID, model.AlarmDisposition{Result: model.DispositionTest}, "test"); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := engine.SetAlarmStatus(ctx, "t", originalID, status, "test"); err != nil {
					t.Fatal(err)
				}
				flush(1) // Resolving or closing does not itself send email.
				send(4)
				send(5)
				flush(2)
				alarms, err = repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE"})
				if err != nil || len(alarms) != 1 || alarms[0].ID == originalID || alarms[0].TriggerCount != 2 {
					t.Fatalf("incorrect next alarm: %+v %v", alarms, err)
				}
			})
		}
	}
}

// This opt-in test requires a disposable Alertmanager and its mounted config
// directory. It delivers exclusively to the test SMTP listener, never 163.
func TestDeviceMailIntegration(t *testing.T) {
	url := os.Getenv("IOT_TEST_ALERTMANAGER_URL")
	if url == "" {
		t.Skip("requires isolated Alertmanager; see docs/PLATFORM.md")
	}
	dir := os.Getenv("IOT_TEST_ALERTMANAGER_DIR")
	smtpHost := os.Getenv("IOT_TEST_SMTP_HOST")
	if dir == "" || smtpHost == "" {
		t.Fatal("set IOT_TEST_ALERTMANAGER_DIR and IOT_TEST_SMTP_HOST")
	}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	messages := make(chan string, 20)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go captureSMTP(conn, messages)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	file := filepath.Join(dir, "alertmanager.yml")
	initial := []byte("route:\n  receiver: discard\nreceivers:\n  - name: discard\n")
	if err := os.WriteFile(file, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &Service{AMConfig: observability.NewFileStore(file, t.TempDir(), 0o644), Alerts: observability.NewAlertmanager(url, 5*time.Second)}
	cfg, err := svc.NotificationConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	noTLS := false
	cfg.Receivers = append(cfg.Receivers, model.OpsReceiver{Name: "local-mail", Emails: []model.OpsEmailReceiver{{To: "test@example.invalid", From: "torchlink@example.invalid", Smarthost: net.JoinHostPort(smtpHost, fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)), RequireTLS: &noTLS}}})
	cfg.DeviceAlarmReceiver = "local-mail"
	if _, err := svc.SaveNotificationConfig(ctx, cfg, "integration-test"); err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	defer bus.Close()
	if err := svc.StartDeviceNotifications(ctx, bus, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, archive, bus, local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	bodies := map[string]bool{}
	for phase := 0; phase < 2; phase++ {
		for i := 1; i <= 2; i++ {
			id := fmt.Sprintf("mail-device-%d", i)
			if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "mail-test", ID: id, AccessKey: id, ProductID: "json_sensor", Name: fmt.Sprintf("邮件测试烟感%d", i), Status: "ENABLED"}); err != nil {
				t.Fatal(err)
			}
			raw := model.RawMessage{MessageID: fmt.Sprintf("mail-raw-%d-%d", phase, i), TenantID: "mail-test", DeviceID: id, ProductID: "json_sensor", Protocol: "json", PayloadFormat: "json", ReceivedAt: time.Now().UnixMilli(), Payload: json.RawMessage(`{"messageType":"ALARM_REPORT","event":{"alarmType":"FIRE","alarmLevel":"HIGH","description":"烟雾浓度超限"}}`)}
			if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
				t.Fatal(err)
			}
			// Distinct reports of the same active alarm must not generate more mail.
			raw.MessageID += "-repeat"
			if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
				t.Fatal(err)
			}
			// The same raw message retried still produces just one receipt.
			if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
				t.Fatal(err)
			}
		}
		alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "mail-test"})
		if err != nil || len(alarms) != (phase+1)*2 {
			t.Fatalf("unexpected business alarm count: %d %v", len(alarms), err)
		}
		for range 2 {
			select {
			case raw := <-messages:
				message, err := mail.ReadMessage(strings.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(subject, "邮件测试烟感") || !strings.Contains(string(body), "烟雾浓度超限") || !strings.Contains(string(body), "告警编号") || !strings.Contains(string(body), "记录信息") || !strings.Contains(string(body), "本次上报时间") {
					t.Fatalf("missing mail detail: %s %s", subject, body)
				}
				if bodies[string(body)] {
					t.Fatalf("duplicate mail: %s", subject)
				}
				bodies[string(body)] = true
			case <-ctx.Done():
				t.Fatal("mail delivery timed out")
			}
		}
		select {
		case raw := <-messages:
			t.Fatalf("unexpected extra mail: %.100s", raw)
		case <-time.After(2 * time.Second):
		}
		if phase == 0 {
			for i, alarm := range alarms {
				status := "RECOVERED"
				if i == 1 {
					status = "CLOSED"
				}
				if _, err := engine.SetAlarmStatus(ctx, alarm.TenantID, alarm.ID, status, "integration-test"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Log("Each alarm produced one SMTP email despite repeated reports; recovery and closure allowed new alarm emails")
}

func captureSMTP(conn net.Conn, messages chan<- string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(conn)
	fmt.Fprint(conn, "220 local-test ESMTP\r\n")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			fmt.Fprint(conn, "250 local-test\r\n")
		case strings.HasPrefix(line, "DATA"):
			fmt.Fprint(conn, "354 end with dot\r\n")
			var body strings.Builder
			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
				body.WriteString(strings.TrimPrefix(line, "."))
			}
			messages <- body.String()
			fmt.Fprint(conn, "250 accepted\r\n")
		case strings.HasPrefix(line, "QUIT"):
			fmt.Fprint(conn, "221 bye\r\n")
			return
		default:
			fmt.Fprint(conn, "250 ok\r\n")
		}
	}
}

// The initial configuration routes every platform alert to a receiver without
// integrations; the overview must say so instead of reporting Alertmanager OK.
func TestAlertmanagerStatusWarnsWithoutReceivers(t *testing.T) {
	ctx := context.Background()
	initial := "route:\n  receiver: platform-null\nreceivers:\n  - name: platform-null\n"
	svc, _, _ := newAMService(t, initial)
	if status, _ := svc.Component(ctx, "alertmanager"); status.State != "degraded" || !strings.Contains(status.Message, "未配置告警接收人") {
		t.Fatalf("missing receiver warning: %+v", status)
	}
	svc, _, _ = newAMService(t, baseAMConfig)
	if status, _ := svc.Component(ctx, "alertmanager"); status.State != "ok" {
		t.Fatalf("configured receivers must not warn: %+v", status)
	}
}
