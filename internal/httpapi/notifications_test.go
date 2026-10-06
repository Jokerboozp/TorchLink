package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/notify"
	"iot-platform/internal/ports"
)

func TestNotificationManagementAndRecipientScope(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "notify-root-test"
	cfg.AdminTenants = []string{"t"}
	cfg.JWTSecret = "notify-test-secret-at-least-32-bytes"
	cfg.DevMode = true
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	cipher, _ := notify.NewCipher(cfg.JWTSecret)
	store := notify.NewMemoryStore()
	api.SetNotifications(&notify.Service{Store: store, Directory: api.NotificationDirectory(), Cipher: cipher, Sender: notify.NewSender(nil)})
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	for _, id := range []string{"d1", "d2"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id, Name: id, ProductID: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)["accessToken"].(string)

	channel := map[string]any{"id": "ding", "name": "值班群", "type": "dingtalk", "enabled": true, "secret": map[string]any{"url": "https://oapi.dingtalk.com/robot/send?access_token=secret-token", "signSecret": "SEC"}}
	saved := req("POST", "/api/v1/notifications/channels", root, channel, 200)
	raw, _ := json.Marshal(saved)
	if strings.Contains(string(raw), "secret-token") || strings.Contains(string(raw), "SEC\"") || saved["config"].(map[string]any)["urlHint"] != "https://oapi.dingtalk.com" || saved["secretSet"] != true {
		t.Fatalf("channel response leaks or lacks fields: %s", raw)
	}
	req("POST", "/api/v1/notifications/channels", root, map[string]any{"id": "evil", "name": "x", "type": "dingtalk", "secret": map[string]any{"url": "https://attacker.example/robot"}}, 422)
	// Updating without a secret keeps the stored URL.
	saved["name"] = "消防值班群"
	req("PUT", "/api/v1/notifications/channels/ding", root, saved, 200)
	if _, sealed, _ := store.GetChannel(ctx, "t", "ding"); sealed == "" {
		t.Fatal("secret lost on update")
	} else if secret, _ := cipher.Open("t", "ding", sealed); secret.URL == "" || secret.SignSecret != "SEC" {
		t.Fatalf("secret %+v", secret)
	}
	policy := map[string]any{"id": "fire", "name": "火警升级", "enabled": true, "levels": []string{"CRITICAL"}, "stages": []any{
		map[string]any{"delaySeconds": 0, "channelIds": []string{"ding"}, "users": []string{"all-user", "limited-user", "no-alarm-user"}},
		map[string]any{"delaySeconds": 300, "channelIds": []string{"ding"}, "onDuty": true},
	}}
	req("POST", "/api/v1/notifications/policies", root, policy, 200)
	req("POST", "/api/v1/notifications/policies", root, map[string]any{"name": "bad", "enabled": true, "stages": []any{map[string]any{"delaySeconds": 5, "channelIds": []string{"ding"}}}}, 422)
	req("DELETE", "/api/v1/notifications/channels/ding", root, nil, 409)

	// Recipients are limited to users who could see the alarm's device.
	users := []map[string]any{
		{"username": "all-user", "phone": "13800000001", "permissions": []string{"menu:devices", "menu:alarms"}, "deviceScope": "all"},
		{"username": "limited-user", "phone": "13800000002", "permissions": []string{"menu:devices", "menu:alarms"}, "deviceScope": "selected", "deviceIds": []string{"d2"}},
		{"username": "no-alarm-user", "phone": "13800000003", "permissions": []string{"menu:devices"}, "deviceScope": "all"},
	}
	for _, u := range users {
		u["password"], u["enabled"] = "notify-user-password", true
		req("POST", "/api/v1/access/users", root, u, 200)
	}
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "bad-phone", "password": "notify-user-password", "phone": "call me", "enabled": true}, 422)
	contacts, err := api.NotificationDirectory().UserContacts(ctx, "t", "d1", []string{"all-user", "limited-user", "no-alarm-user"}, nil)
	if err != nil || len(contacts) != 1 || contacts[0].Phone != "13800000001" {
		t.Fatalf("contacts %+v %v", contacts, err)
	}

	// The alarm timeline follows the alarm's device scope.
	if _, _, err := repo.UpsertAlarm(ctx, model.Alarm{TenantID: "t", ID: "a1", DeviceID: "d1", RuleID: "r", Status: "ACTIVE", AlarmLevel: "CRITICAL", AlarmType: "FIRE", TriggerCount: 1}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(model.Alarm{TenantID: "t", ID: "a1", DeviceID: "d1", Status: "ACTIVE", AlarmLevel: "CRITICAL", AlarmType: "FIRE", TriggerCount: 1})
	if err := api.notifications.HandleReported(ctx, payload); err != nil {
		t.Fatal(err)
	}
	timeline := req("GET", "/api/v1/alarms/a1/notifications", root, nil, 200)["items"].([]any)
	if len(timeline) != 2 || timeline[0].(map[string]any)["channelName"] != "消防值班群" {
		t.Fatalf("timeline %v", timeline)
	}
	limited := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "limited-user", "password": "notify-user-password", "tenantId": "t"}, 200)["accessToken"].(string)
	req("GET", "/api/v1/alarms/a1/notifications", limited, nil, 404)
	// Managing notifications is tenant-wide and needs the full device scope.
	req("GET", "/api/v1/notifications/policies", limited, nil, 403)
}
