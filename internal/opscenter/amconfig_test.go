package opscenter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type fakeAlertmanager struct {
	ports.AlertmanagerBackend
	file    string
	posted  []map[string]any
	reloads int
}

func (f *fakeAlertmanager) Configured() bool { return true }

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
