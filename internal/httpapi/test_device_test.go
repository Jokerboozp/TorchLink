package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestTestDeviceUsesConfiguredAlarmRuleWithoutCreatingFixtureRule(t *testing.T) { /* 定义 TestTestDeviceUsesConfiguredAlarmRuleWithoutCreatingFixtureRule 函数。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                          /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir())           /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	scope := testDeviceScope("tenant_test_device") /* 更新 scope 的值。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{  /* 判断条件并选择处理分支。 */
		ID:        "rule_test_device_" + scope,    /* 执行当前语句并推进处理流程。 */
		TenantID:  "tenant_test_device",           /* 执行当前语句并推进处理流程。 */
		ProductID: "product_test_device_" + scope, /* 执行当前语句并推进处理流程。 */
		Name:      "测试设备高温烟雾报警",                   /* 执行当前语句并推进处理流程。 */
		AlarmType: "FIRE_RISK",                    /* 执行当前语句并推进处理流程。 */
		Level:     "HIGH",                         /* 执行当前语句并推进处理流程。 */
		Match:     "all",                          /* 执行当前语句并推进处理流程。 */
		Conditions: []model.RuleCondition{ /* 执行当前语句并推进处理流程。 */
			{Field: "temperature", Operator: ">", Value: 80}, /* 执行当前语句并推进处理流程。 */
			{Field: "smoke", Operator: "eq", Value: true},    /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		Recovery: []model.RuleCondition{ /* 执行当前语句并推进处理流程。 */
			{Field: "temperature", Operator: "<=", Value: 80}, /* 执行当前语句并推进处理流程。 */
			{Field: "smoke", Operator: "eq", Value: false},    /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		Enabled: true, /* 执行当前语句并推进处理流程。 */
	}); err != nil { /* 结束当前表达式或代码块。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	realtime := local.NewRealtime()                                                                                                                                        /* 更新 realtime 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	if err := engine.Start(ctx); err != nil {                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                       /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                         /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                       /* 更新 cfg.JWTSecret 的值。 */
	cfg.AdminTenants = []string{"tenant_test_device"}                                          /* 更新 cfg.AdminTenants 的值。 */
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))     /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                /* 更新 server 的值。 */
	defer server.Close()                                                                       /* 安排函数结束时执行清理。 */
	token, err := api.auth.Issue("operator", "tenant_test_device", "operator", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	fixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated) /* 更新 fixture 的值。 */
	productID := fixture["product"].(map[string]any)["id"].(string)                                                                                       /* 更新 productID 的值。 */
	rules := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/rules", token, nil, http.StatusOK)                                       /* 更新 rules 的值。 */
	if len(rules["items"].([]any)) != 0 {                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("test device provisioning created an unexpected alarm rule: %#v", rules) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/rules", token, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"id":        "rule_test_device_navigation", /* 执行当前语句并推进处理流程。 */
		"name":      "测试设备报警后打开设备管理",               /* 执行当前语句并推进处理流程。 */
		"productId": productID,                     /* 执行当前语句并推进处理流程。 */
		"alarmType": "FIRE_RISK",                   /* 执行当前语句并推进处理流程。 */
		"level":     "HIGH",                        /* 执行当前语句并推进处理流程。 */
		"match":     "all",                         /* 执行当前语句并推进处理流程。 */
		"enabled":   true,                          /* 执行当前语句并推进处理流程。 */
		"conditions": []map[string]any{ /* 执行当前语句并推进处理流程。 */
			{"field": "temperature", "operator": ">", "value": 80}, /* 执行当前语句并推进处理流程。 */
			{"field": "smoke", "operator": "eq", "value": true},    /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		"actions": []map[string]any{{"type": "OPEN_PAGE", "page": "devices"}}, /* 执行当前语句并推进处理流程。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */

	deviceID := fixture["device"].(map[string]any)["id"].(string)                                                                    /* 更新 deviceID 的值。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"messageId": "raw_test_device_navigation", /* 执行当前语句并推进处理流程。 */
		"payload": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"properties": map[string]any{"temperature": 88.5, "smoke": true},                                                                 /* 执行当前语句并推进处理流程。 */
			"tags":       map[string]any{"cityCode": "city_001", "districtCode": "district_01", "buildingId": "A-01", "deviceType": "smoke"}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */

	found := false                                /* 更新 found 的值。 */
	for _, published := range realtime.Messages { /* 循环处理当前数据。 */
		if published.Topic != "/iot/ui-action/tenant_test_device" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var event model.UIActionEvent                                     /* 声明 event。 */
		if err := json.Unmarshal(published.Payload, &event); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if event.RuleID == "rule_test_device_navigation" && event.Action.Type == "OPEN_PAGE" && event.Action.Page == "devices" { /* 判断条件并选择处理分支。 */
			found = true /* 更新 found 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !found { /* 判断条件并选择处理分支。 */
		t.Fatalf("configured alarm rule did not publish OPEN_PAGE devices action: %#v", realtime.Messages) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestTestDeviceDirectAlarmCreatesAlarmAndUpdatesDeviceState(t *testing.T) { /* 定义 TestTestDeviceDirectAlarmCreatesAlarmAndUpdatesDeviceState 函数。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                          /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir())           /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	if err := engine.Start(ctx); err != nil {                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                        /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                          /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                        /* 更新 cfg.JWTSecret 的值。 */
	cfg.AdminTenants = []string{"tenant_direct_alarm"}                                          /* 更新 cfg.AdminTenants 的值。 */
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))      /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                 /* 更新 server 的值。 */
	defer server.Close()                                                                        /* 安排函数结束时执行清理。 */
	token, err := api.auth.Issue("operator", "tenant_direct_alarm", "operator", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	fixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated) /* 更新 fixture 的值。 */
	deviceID := fixture["device"].(map[string]any)["id"].(string)                                                                                         /* 更新 deviceID 的值。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{                      /* 执行当前语句并推进处理流程。 */
		"messageId": "raw_direct_alarm", /* 执行当前语句并推进处理流程。 */
		"payload": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"alarm":      true,                                                              /* 执行当前语句并推进处理流程。 */
			"properties": map[string]any{"temperature": 88.5, "smoke": true, "battery": 92}, /* 执行当前语句并推进处理流程。 */
			"tags":       map[string]any{"deviceType": "smoke"},                             /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */
	alarms := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+deviceID+"&status=ACTIVE", token, nil, http.StatusOK) /* 更新 alarms 的值。 */
	if alarms["count"] != float64(1) {                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("direct device alarm was not shown in alarm center: %#v", alarms) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	items := alarms["items"].([]any) /* 更新 items 的值。 */
	if len(items) != 1 {             /* 判断条件并选择处理分支。 */
		t.Fatalf("expected one direct alarm item: %#v", alarms) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	item := items[0].(map[string]any)                                                                        /* 更新 item 的值。 */
	if item["source"] != "device" || item["alarmType"] != "SMOKE_DETECTED" || item["alarmLevel"] != "HIGH" { /* 判断条件并选择处理分支。 */
		t.Fatalf("direct alarm metadata was not inferred correctly: %#v", item) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	state := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices/"+deviceID+"/latest", token, nil, http.StatusOK) /* 更新 state 的值。 */
	if state["state"].(map[string]any)["businessStatus"] != "ALARM" {                                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("device state did not change to ALARM: %#v", state) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"messageId": "raw_direct_recovery", /* 执行当前语句并推进处理流程。 */
		"payload": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"properties": map[string]any{"temperature": 26.5, "smoke": false, "battery": 96}, /* 执行当前语句并推进处理流程。 */
			"tags":       map[string]any{"deviceType": "smoke"},                              /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */
	alarms = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+deviceID+"&status=ACTIVE", token, nil, http.StatusOK) /* 更新 alarms 的值。 */
	if alarms["count"] != float64(0) {                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("direct device alarm was not recovered: %#v", alarms) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	state = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices/"+deviceID+"/latest", token, nil, http.StatusOK) /* 更新 state 的值。 */
	if state["state"].(map[string]any)["businessStatus"] != "ONLINE" {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("device state did not return to ONLINE: %#v", state) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
