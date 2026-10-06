package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/devicescope"
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
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	if code, text := exportCSV(t, srv, other); code != 200 || strings.Count(text, "\n") != 2 || !strings.Contains(text, "fault-1") {
		t.Fatalf("scoped export %d %q", code, text)
	}

	// The monthly PDF report needs its own permission.
	month := time.Now().In(model.ReportZone).Format("2006-01")
	monthly := func(token, query string) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequest("GET", srv.URL+alarmMonthlyPath+query, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}
	if code, pdf := monthly(root, "?month="+month); code != 200 || !bytes.HasPrefix(pdf, []byte("%PDF-1.")) || !bytes.HasSuffix(bytes.TrimSpace(pdf), []byte("%%EOF")) {
		t.Fatalf("monthly report %d %q", code, pdf[:min(len(pdf), 40)])
	}
	if code, _ := monthly(root, "?month=2999-01"); code != 400 {
		t.Fatalf("future month accepted: %d", code)
	}
	if code, _ := monthly(other, ""); code != 403 {
		t.Fatalf("monthly report without permission: %d", code)
	}
}

func exportCSV(t *testing.T, srv *httptest.Server, token string) (int, string) {
	t.Helper()
	r, _ := http.NewRequest("GET", srv.URL+alarmExportPath, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, string(body)
	}
	return resp.StatusCode, string(body)
}

// failingScan fails after some alarms were already written.
type failingScan struct{ *memory.Repository }

func (r failingScan) EachAlarm(ctx context.Context, f ports.AlarmFilter, fn func(model.Alarm) error) error {
	sent := 0
	return r.Repository.EachAlarm(ctx, f, func(a model.Alarm) error {
		if sent == alarmExportFlushRows+1 {
			return errors.New("database went away")
		}
		sent++
		return fn(a)
	})
}

// The export has no row limit, and a failure midway breaks the download
// instead of producing a shortened file.
func TestAlarmExportStreamsAndAbortsOnFailure(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	const rows = 3*alarmExportFlushRows + 7
	for i := range rows {
		a := model.Alarm{ID: fmt.Sprintf("a%d", i), TenantID: "t", DeviceID: "d", RuleID: fmt.Sprintf("r%d", i), AlarmType: "DEVICE_FAULT", AlarmLevel: "LOW", Status: "CLOSED", FirstTriggeredAt: now - int64(i), LastTriggeredAt: now - int64(i), TriggerCount: 1}
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	for _, fail := range []bool{false, true} {
		cfg := config.Load()
		cfg.AdminUser, cfg.AdminPassword = "root", "disposition-root-test"
		cfg.AdminTenants = []string{"t"}
		cfg.JWTSecret = "disposition-test-secret-at-least-32-bytes"
		cfg.DevMode = true
		var store ports.Repository = repo
		if fail {
			store = failingScan{repo}
		}
		api := New(cfg, &core.Engine{Repo: devicescope.Wrap(store), Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv := httptest.NewServer(api.Handler())
		root := requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)["accessToken"].(string)
		code, text := exportCSV(t, srv, root)
		srv.Close()
		if fail {
			if code != -1 {
				t.Fatalf("failed export completed: %d with %d lines", code, strings.Count(text, "\n"))
			}
			continue
		}
		if code != 200 || strings.Count(text, "\n") != rows+1 {
			t.Fatalf("export %d with %d lines, want %d", code, strings.Count(text, "\n"), rows+1)
		}
	}
}
