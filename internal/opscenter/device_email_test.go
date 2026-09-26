package opscenter

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

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
