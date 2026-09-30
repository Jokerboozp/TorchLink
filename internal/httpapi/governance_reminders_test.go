package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func TestGovernanceReminderAPIUsesRecipientAndRecordPermission(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", AccessKey: "fixture-device"}); err != nil {
		t.Fatal(err)
	}
	permissions := []string{"menu:devices", "menu:alarms", "menu:alarmGovernance", "action:alarmGovernance:record"}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "recipient", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"d"}, Permissions: permissions}, {Username: "other", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"d"}, Permissions: permissions}}}
	if _, err := repo.SaveAccessState(ctx, "t", state); err != nil {
		t.Fatal(err)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "reminder-api-fixture-secret-more-than-32", DevMode: true}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	admin, _ := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	recipient, _ := api.auth.IssueUser("recipient", "t", 1, time.Hour)
	other, _ := api.auth.IssueUser("other", "t", 1, time.Hour)
	now := time.Now().UnixMilli()
	obs, _, err := repo.SaveAlarmObservation(ctx, model.AlarmObservation{TenantID: "t", DeviceID: "d", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE", SourceSystem: "protocol", SourceEventID: "report", FactKind: "REPORT", EventAt: now - 1000})
	if err != nil {
		t.Fatal(err)
	}
	request := func(token, method, path string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+"/api/v1/alarm-governance"+path, token, body, status)
	}
	c := request(admin, "POST", "/cases", map[string]any{"title": "核实设备", "deviceId": "d", "alarmType": "FIRE", "originKind": "DEVICE_DIRECT", "signalKey": "device:FIRE", "ownerUserId": "recipient", "observationIds": []string{obs.ID}, "idempotencyKey": "case"}, 201)
	body := c["body"].(map[string]any)
	round := body["currentRoundId"].(string)
	request(admin, "POST", "/rounds/"+round+"/measures", map[string]any{"content": "现场检查设备与环境", "ownerUserId": "recipient", "dueAt": now - 100, "basis": "现场核实", "requiresAcceptance": true, "idempotencyKey": "measure"}, 201)
	refresh := request(recipient, "POST", "/reminders/refresh", nil, 200)
	if refresh["created"] != float64(1) {
		t.Fatal(refresh)
	}
	page := request(recipient, "GET", "/reminders?status=UNREAD", nil, 200)
	items := page["items"].([]any)
	if page["total"] != float64(1) || len(items) != 1 {
		t.Fatal(page)
	}
	reminder := items[0].(map[string]any)
	foreign := request(other, "GET", "/reminders", nil, 200)
	if foreign["total"] != float64(0) {
		t.Fatal("another recipient saw task", foreign)
	}
	action := map[string]any{"expectedVersion": reminder["version"], "idempotencyKey": "read"}
	request(other, "POST", "/reminders/"+reminder["id"].(string)+"/read", action, 403)
	read := request(recipient, "POST", "/reminders/"+reminder["id"].(string)+"/read", action, 200)
	if read["status"] != "READ" {
		t.Fatal(read)
	}
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].Permissions = []string{"menu:devices", "menu:alarms", "menu:alarmGovernance"}
	if _, err = repo.SaveAccessState(ctx, "t", state); err != nil {
		t.Fatal(err)
	}
	request(recipient, "GET", "/reminders", nil, 200)
	request(recipient, "POST", "/reminders/"+reminder["id"].(string)+"/handled", map[string]any{"expectedVersion": read["version"], "idempotencyKey": "handled"}, 403)
}
