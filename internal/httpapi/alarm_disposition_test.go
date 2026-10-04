package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestAlarmVerificationStatisticsAndExport(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "disposition-root-test"
	cfg.AdminTenants = []string{"t"}
	cfg.JWTSecret = "disposition-test-secret-at-least-32-bytes"
	cfg.DevMode = true
	api := New(cfg, &core.Engine{Repo: repo, Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	now := time.Now().UnixMilli()
	for i, a := range []model.Alarm{
		{ID: "fire-1", DeviceID: "d1", DeviceName: "一号烟感", AlarmType: "SMOKE_DETECTED", AlarmLevel: "CRITICAL"},
		{ID: "fire-2", DeviceID: "d1", DeviceName: "一号烟感", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH"},
		{ID: "fault-1", DeviceID: "d2", AlarmType: "DEVICE_FAULT", AlarmLevel: "LOW"},
	} {
		a.TenantID, a.RuleID, a.Status, a.FirstTriggeredAt, a.LastTriggeredAt, a.TriggerCount = "t", a.ID, "ACTIVE", now-int64(10-i)*60_000, now-int64(10-i)*60_000, 1
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: a.DeviceID, AccessKey: a.DeviceID, ProductID: "p"})
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)["accessToken"].(string)
	req("POST", "/api/v1/alarms/fire-1/actions", root, map[string]any{"action": "acked"}, 200)
	req("POST", "/api/v1/alarms/fire-1/actions", root, map[string]any{"action": "closed"}, 422)
	req("POST", "/api/v1/alarms/fire-1/disposition", root, map[string]any{"result": "maybe"}, 422)
	req("POST", "/api/v1/alarms/fire-1/disposition", root, map[string]any{"result": "false_alarm", "notes": "厨房油烟"}, 200)
	req("POST", "/api/v1/alarms/fire-2/disposition", root, map[string]any{"result": "REAL_FIRE", "dispatchId": "missing"}, 422)
	req("POST", "/api/v1/alarms/fire-2/disposition", root, map[string]any{"result": "REAL_FIRE"}, 200)
	closed := req("POST", "/api/v1/alarms/fire-1/actions", root, map[string]any{"action": "closed"}, 200)
	if closed["status"] != "CLOSED" || closed["disposition"].(map[string]any)["handler"] != "root" {
		t.Fatalf("closed %v", closed)
	}
	// A low device fault needs no verification to close.
	req("POST", "/api/v1/alarms/fault-1/actions", root, map[string]any{"action": "closed"}, 200)

	stats := req("GET", alarmStatisticsPath, root, nil, 200)
	byResult := stats["byResult"].(map[string]any)
	if stats["total"] != float64(3) || stats["verified"] != float64(2) || byResult["FALSE_ALARM"] != float64(1) || stats["falseAlarmRate"] != 0.5 || stats["acknowledge"].(map[string]any)["count"] != float64(1) {
		t.Fatalf("statistics %v", stats)
	}
	if top := stats["topFalseAlarmDevices"].([]any); len(top) != 1 || top[0].(map[string]any)["deviceId"] != "d1" {
		t.Fatalf("top devices %v", top)
	}
	r, _ := http.NewRequest("GET", srv.URL+alarmExportPath, nil)
	r.Header.Set("Authorization", "Bearer "+root)
	resp, err := srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(body)
	if resp.StatusCode != 200 || !strings.HasPrefix(text, "\ufeff告警编号") || !strings.Contains(text, "误报") || !strings.Contains(text, "厨房油烟") || strings.Count(text, "\n") != 4 {
		t.Fatalf("export %d %q", resp.StatusCode, text)
	}
	// Users limited to other devices can neither verify nor see these alarms.
	user := map[string]any{"username": "other", "password": "other-password-1", "enabled": true, "permissions": []string{"menu:devices", "menu:alarms", "POST /api/v1/alarms/:id/disposition", "GET " + alarmExportPath}, "deviceScope": "selected", "deviceIds": []string{"d2"}}
	req("POST", "/api/v1/access/users", root, user, 200)
	other := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "other", "password": "other-password-1", "tenantId": "t"}, 200)["accessToken"].(string)
	req("POST", "/api/v1/alarms/fire-2/disposition", other, map[string]any{"result": "TEST"}, 403)
	if scoped := req("GET", alarmStatisticsPath, other, nil, 200); scoped["total"] != float64(1) {
		t.Fatalf("scoped statistics %v", scoped)
	}
}
