package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestTestDeviceUsesConfiguredAlarmRuleWithoutCreatingFixtureRule(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scope := testDeviceScope("tenant_test_device")
	if err := repo.SaveRule(ctx, model.AlarmRule{
		ID:        "rule_test_device_" + scope,
		TenantID:  "tenant_test_device",
		ProductID: "product_test_device_" + scope,
		Name:      "测试设备高温烟雾报警",
		AlarmType: "FIRE_RISK",
		Level:     "HIGH",
		Match:     "all",
		Conditions: []model.RuleCondition{
			{Field: "temperature", Operator: ">", Value: 80},
			{Field: "smoke", Operator: "eq", Value: true},
		},
		Recovery: []model.RuleCondition{
			{Field: "temperature", Operator: "<=", Value: 80},
			{Field: "smoke", Operator: "eq", Value: false},
		},
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	realtime := local.NewRealtime()
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	cfg.AdminTenants = []string{"tenant_test_device"}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("operator", "tenant_test_device", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	fixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated)
	productID := fixture["product"].(map[string]any)["id"].(string)
	rules := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/rules", token, nil, http.StatusOK)
	if len(rules["items"].([]any)) != 0 {
		t.Fatalf("test device provisioning created an unexpected alarm rule: %#v", rules)
	}

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/rules", token, map[string]any{
		"id":        "rule_test_device_navigation",
		"name":      "测试设备报警后打开设备管理",
		"productId": productID,
		"alarmType": "FIRE_RISK",
		"level":     "HIGH",
		"match":     "all",
		"enabled":   true,
		"conditions": []map[string]any{
			{"field": "temperature", "operator": ">", "value": 80},
			{"field": "smoke", "operator": "eq", "value": true},
		},
		"actions": []map[string]any{{"type": "OPEN_PAGE", "page": "devices"}},
	}, http.StatusCreated)

	deviceID := fixture["device"].(map[string]any)["id"].(string)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{
		"messageId": "raw_test_device_navigation",
		"payload": map[string]any{
			"properties": map[string]any{"temperature": 88.5, "smoke": true},
			"tags":       map[string]any{"cityCode": "city_001", "districtCode": "district_01", "buildingId": "A-01", "deviceType": "smoke"},
		},
	}, http.StatusCreated)

	found := false
	for _, published := range realtime.Messages {
		if published.Topic != "/iot/ui-action/tenant_test_device" {
			continue
		}
		var event model.UIActionEvent
		if err := json.Unmarshal(published.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.RuleID == "rule_test_device_navigation" && event.Action.Type == "OPEN_PAGE" && event.Action.Page == "devices" {
			found = true
		}
	}
	if !found {
		t.Fatalf("configured alarm rule did not publish OPEN_PAGE devices action: %#v", realtime.Messages)
	}
}

func TestTestDeviceDirectAlarmCreatesAlarmAndUpdatesDeviceState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	cfg.AdminTenants = []string{"tenant_direct_alarm"}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("operator", "tenant_direct_alarm", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated)
	deviceID := fixture["device"].(map[string]any)["id"].(string)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{
		"messageId": "raw_direct_alarm",
		"payload": map[string]any{
			"alarm":      true,
			"properties": map[string]any{"temperature": 88.5, "smoke": true, "battery": 92},
			"tags":       map[string]any{"deviceType": "smoke"},
		},
	}, http.StatusCreated)
	alarms := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+deviceID+"&status=ACTIVE", token, nil, http.StatusOK)
	if alarms["count"] != float64(1) {
		t.Fatalf("direct device alarm was not shown in alarm center: %#v", alarms)
	}
	items := alarms["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one direct alarm item: %#v", alarms)
	}
	item := items[0].(map[string]any)
	if item["source"] != "device" || item["alarmType"] != "SMOKE_DETECTED" || item["alarmLevel"] != "HIGH" {
		t.Fatalf("direct alarm metadata was not inferred correctly: %#v", item)
	}
	state := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices/"+deviceID+"/latest", token, nil, http.StatusOK)
	if state["state"].(map[string]any)["businessStatus"] != "ALARM" {
		t.Fatalf("device state did not change to ALARM: %#v", state)
	}

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{
		"messageId": "raw_direct_recovery",
		"payload": map[string]any{
			"properties": map[string]any{"temperature": 26.5, "smoke": false, "battery": 96},
			"tags":       map[string]any{"deviceType": "smoke"},
		},
	}, http.StatusCreated)
	alarms = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+deviceID+"&status=ACTIVE", token, nil, http.StatusOK)
	if alarms["count"] != float64(0) {
		t.Fatalf("direct device alarm was not recovered: %#v", alarms)
	}
	state = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices/"+deviceID+"/latest", token, nil, http.StatusOK)
	if state["state"].(map[string]any)["businessStatus"] != "ONLINE" {
		t.Fatalf("device state did not return to ONLINE: %#v", state)
	}
}
