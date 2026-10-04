package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestSitesUnitGrantsPlansAndAlarmLocation(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "sites-root-password"
	cfg.AdminTenants = []string{"t"}
	cfg.JWTSecret = "sites-test-secret-at-least-32-bytes-long"
	cfg.DevMode = true
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := New(cfg, &core.Engine{Repo: repo, Archive: archive, Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	for _, id := range []string{"panel", "pump", "other"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, Name: id + " 设备", AccessKey: id, ProductID: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)["accessToken"].(string)
	unit := req("POST", "/api/v1/sites/units", root, map[string]any{"name": "示例单位"}, 201)
	building := req("POST", "/api/v1/sites/buildings", root, map[string]any{"unitId": unit["id"], "name": "1 号楼"}, 201)
	floor := req("POST", "/api/v1/sites/floors", root, map[string]any{"buildingId": building["id"], "name": "3F", "level": 3}, 201)
	floorPath := "/api/v1/sites/floors/" + floor["id"].(string)

	upload := func(path, token string, data []byte, status int) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("file", "plan.png")
		_, _ = part.Write(data)
		_ = form.Close()
		r, _ := http.NewRequest("PUT", srv.URL+path, &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("upload %s: %d, want %d", path, resp.StatusCode, status)
		}
	}
	var plan bytes.Buffer
	_ = png.Encode(&plan, image.NewRGBA(image.Rect(0, 0, 40, 20)))
	upload(floorPath+"/plan?version=1", root, []byte("<svg onload=alert(1)>"), 422)
	upload(floorPath+"/plan?version=1", root, plan.Bytes(), 200)
	req("POST", "/api/v1/sites/points", root, map[string]any{"floorId": floor["id"], "deviceId": "panel", "name": "消防主机", "x": 0.5, "y": 0.5}, 201)
	req("POST", "/api/v1/sites/points", root, map[string]any{"unitId": unit["id"], "deviceId": "missing"}, 403)

	// A user granted the unit sees its placed devices, and a device placed
	// later joins the grant without editing the user.
	user := map[string]any{"username": "unit-user", "password": "unit-user-password", "enabled": true, "permissions": []string{"menu:devices", "menu:alarms", "menu:sites"}, "deviceScope": "selected", "deviceIds": []string{}, "unitIds": []string{unit["id"].(string)}}
	req("POST", "/api/v1/access/users", root, user, 200)
	token := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "unit-user", "password": "unit-user-password", "tenantId": "t"}, 200)["accessToken"].(string)
	for i, a := range []model.Alarm{{ID: "a-panel", DeviceID: "panel"}, {ID: "a-pump", DeviceID: "pump"}, {ID: "a-other", DeviceID: "other"}} {
		a.TenantID, a.RuleID, a.Status, a.AlarmType, a.AlarmLevel, a.FirstTriggeredAt, a.LastTriggeredAt, a.TriggerCount = "t", a.ID, "ACTIVE", "FIRE", "HIGH", int64(i+1), int64(i+1), 1
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	detail := req("GET", "/api/v1/alarms/a-panel", token, nil, 200)
	location := detail["location"].(map[string]any)
	if location["pointName"] != "消防主机" || location["floorName"] != "3F" || location["current"] != true {
		t.Fatalf("alarm raised before placement shows the current location: %v", location)
	}
	req("GET", "/api/v1/alarms/a-pump", token, nil, 403)
	r, _ := http.NewRequest("GET", srv.URL+"/api/v1/alarms/a-panel/location-plan", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(served, plan.Bytes()) {
		t.Fatalf("alarm floor plan: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	pump := req("POST", "/api/v1/sites/points", root, map[string]any{"unitId": unit["id"], "deviceId": "pump"}, 201)
	req("GET", "/api/v1/alarms/a-pump", token, nil, 200)
	alarmIDs := func(token string) []string {
		t.Helper()
		ids := []string{}
		for _, item := range req("GET", "/api/v1/alarms", token, nil, 200)["items"].([]any) {
			ids = append(ids, item.(map[string]any)["alarmId"].(string))
		}
		slices.Sort(ids)
		return ids
	}
	if ids := alarmIDs(token); !slices.Equal(ids, []string{"a-panel", "a-pump"}) {
		t.Fatalf("unit user alarm list: %v", ids)
	}
	// A unit granted through a role reaches users that inherit role scopes.
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "unit-role", "name": "单位值守", "permissions": []string{"menu:devices", "menu:alarms"}, "deviceScope": "selected", "unitIds": []string{unit["id"].(string)}}, 200)
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "role-user", "password": "role-user-password", "enabled": true, "roleIds": []string{"unit-role"}, "deviceScope": "inherit"}, 200)
	roleToken := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "role-user", "password": "role-user-password", "tenantId": "t"}, 200)["accessToken"].(string)
	if ids := alarmIDs(roleToken); !slices.Equal(ids, []string{"a-panel", "a-pump"}) {
		t.Fatalf("role unit grant alarm list: %v", ids)
	}
	req("GET", "/api/v1/alarms/a-other", roleToken, nil, 403)
	listed := req("GET", "/api/v1/sites", token, nil, 200)
	if points := listed["points"].([]any); len(points) != 2 || points[0].(map[string]any)["deviceName"] != "panel 设备" {
		t.Fatalf("unit user points: %v", points)
	}
	// The user cannot place a device outside their scope.
	req("POST", "/api/v1/sites/points", token, map[string]any{"unitId": unit["id"], "deviceId": "other"}, 403)
	req("DELETE", "/api/v1/sites/points/"+pump["id"].(string)+"?version=1", root, nil, 200)
	req("GET", "/api/v1/alarms/a-pump", token, nil, 403)
	// Unit grants require the selected scope and existing units.
	user["unitIds"] = []string{"missing"}
	req("PUT", "/api/v1/access/users/unit-user", root, user, 422)
	req("DELETE", "/api/v1/sites/units/"+unit["id"].(string)+"?version=1", root, nil, 409)
}
