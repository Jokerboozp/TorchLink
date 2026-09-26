package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"fmt"                                   /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"           /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestDeviceScopeHTTPIsolation(t *testing.T) { /* 定义 TestDeviceScopeHTTPIsolation 函数。 */
	repo := memory.NewRepository()                        /* 更新 repo 的值。 */
	ctx := context.Background()                           /* 更新 ctx 的值。 */
	now := time.Now().UnixMilli()                         /* 更新 now 的值。 */
	cfg := config.Load()                                  /* 更新 cfg 的值。 */
	cfg.AdminUser = "root"                                /* 更新 cfg.AdminUser 的值。 */
	cfg.AdminPassword = "scope-root-test"                 /* 更新 cfg.AdminPassword 的值。 */
	cfg.AdminTenants = []string{"tenant_a", "tenant_b"}   /* 更新 cfg.AdminTenants 的值。 */
	cfg.JWTSecret = "scope-test-secret-at-least-32-bytes" /* 更新 cfg.JWTSecret 的值。 */
	cfg.DevMode = true                                    /* 更新 cfg.DevMode 的值。 */
	must := func(e error) {                               /* 更新 must 的值。 */
		t.Helper()    /* 执行当前语句并推进处理流程。 */
		if e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	must(repo.SaveProduct(ctx, model.Product{TenantID: "tenant_a", ID: "product", Name: "演示产品"})) /* 执行当前语句并推进处理流程。 */
	for i := 0; i < 46; i++ {                                                                     /* 循环处理当前数据。 */
		id := fmt.Sprintf("device-%02d", i)                                                                                                                                                                 /* 更新 id 的值。 */
		must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: id, AccessKey: id, Name: id, ProductID: "product", DeviceRole: "DIRECT"}))                                           /* 执行当前语句并推进处理流程。 */
		must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant_a", DeviceID: id, ProductID: "product", BusinessStatus: "ONLINE"}))                                                            /* 执行当前语句并推进处理流程。 */
		_, _, e := repo.UpsertAlarm(ctx, model.Alarm{TenantID: "tenant_a", ID: "alarm-" + id, DeviceID: id, RuleID: id, Status: "ACTIVE", AlarmLevel: "HIGH", FirstTriggeredAt: now, LastTriggeredAt: now}) /* 更新 e 的值。 */
		must(e)                                                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
		_, e = repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "tenant_a", MessageID: "raw-" + id, DeviceID: id, ReceivedAt: now})                                                                   /* 更新 e 的值。 */
		must(e)                                                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_b", ID: "foreign-device", AccessKey: "foreign-key", Name: "其他租户设备"})) /* 执行当前语句并推进处理流程。 */
	engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}}                                                                                 /* 更新 engine 的值。 */
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                       /* 更新 api 的值。 */
	srv := httptest.NewServer(api.Handler())                                                                                                     /* 更新 srv 的值。 */
	defer srv.Close()                                                                                                                            /* 安排函数结束时执行清理。 */
	req := func(method, path, token string, body any, status int) map[string]any {                                                               /* 更新 req 的值。 */
		t.Helper()                                                                     /* 执行当前语句并推进处理流程。 */
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	login := func(name string) string { /* 更新 login 的值。 */
		t.Helper()                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": name, "password": "scope-password-test", "tenantId": "tenant_a"}, 200)["accessToken"].(string) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string) /* 更新 root 的值。 */
	_, _, err := repo.UpsertAlarm(ctx, model.Alarm{TenantID: "tenant_b", ID: "foreign-alarm", DeviceID: "foreign-device", RuleID: "foreign", Status: "ACTIVE"})           /* 更新 err 的值。 */
	must(err)                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	adminEvents := req("GET", "/api/v1/events", root, nil, 200)                                                                                                           /* 更新 adminEvents 的值。 */
	if len(adminEvents["alarms"].([]any)) != 46 || len(adminEvents["devices"].([]any)) != 46 {                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal("administrator events must contain only the current tenant's data", adminEvents) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if permissions := adminEvents["permissions"].([]any); len(permissions) != 1 || permissions[0] != "*" { /* 判断条件并选择处理分支。 */
		t.Fatal("administrator permissions changed", permissions) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, item := range adminEvents["alarms"].([]any) { /* 循环处理当前数据。 */
		if item.(map[string]any)["tenantId"] != "tenant_a" { /* 判断条件并选择处理分支。 */
			t.Fatal("administrator events leak another tenant") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	perms := []string{"menu:devices", "menu:alarms", "menu:dashboard", "menu:raw", "menu:ai", "menu:inspection", "menu:backups", "POST /api/v1/alarms/:id/actions"} /* 更新 perms 的值。 */
	ids := []string{}                                                                                                                                               /* 更新 ids 的值。 */
	for i := 0; i < 46; i += 2 {                                                                                                                                    /* 循环处理当前数据。 */
		ids = append(ids, fmt.Sprintf("device-%02d", i)) /* 更新 ids 的值。 */
	} /* 结束当前表达式或代码块。 */
	u := map[string]any{"username": "scope-user", "password": "scope-password-test", "enabled": true, "permissions": perms, "deviceScope": "selected", "deviceIds": ids} /* 更新 u 的值。 */
	req("POST", "/api/v1/access/users", root, u, 200)                                                                                                                    /* 执行当前语句并推进处理流程。 */
	token := login("scope-user")                                                                                                                                         /* 更新 token 的值。 */
	list := req("GET", "/api/v1/device-registry?page=2&pageSize=20", token, nil, 200)                                                                                    /* 更新 list 的值。 */
	if list["total"].(float64) != 23 || len(list["items"].([]any)) != 3 {                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatalf("scope pagination: %v", list) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, item := range list["items"].([]any) { /* 循环处理当前数据。 */
		id := item.(map[string]any)["device"].(map[string]any)["id"].(string) /* 更新 id 的值。 */
		n := 0                                                                /* 更新 n 的值。 */
		fmt.Sscanf(id, "device-%d", &n)                                       /* 执行当前语句并推进处理流程。 */
		if n%2 != 0 {                                                         /* 判断条件并选择处理分支。 */
			t.Fatal("ungranted device returned") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, path := range []string{"/api/v1/alarms", "/api/v1/raw-messages", "/api/v1/raw-messages?parseStatus=UNPARSED", "/api/v1/devices"} { /* 循环处理当前数据。 */
		v := req("GET", path, token, nil, 200) /* 更新 v 的值。 */
		if v["total"].(float64) != 23 {        /* 判断条件并选择处理分支。 */
			t.Fatalf("%s total=%v", path, v["total"]) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	req("GET", "/api/v1/device-registry/device-01/connection", token, nil, 403)                           /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/devices/device-01/properties/history?property=x", token, nil, 403)                /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/alarms/alarm-device-01", token, nil, 403)                                         /* 执行当前语句并推进处理流程。 */
	req("POST", "/api/v1/alarms/alarm-device-01/actions", token, map[string]string{"action": "ACK"}, 403) /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/raw-messages/raw-device-01", token, nil, 404)                                     /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/alarms/alarm-device-00", token, nil, 200)                                         /* 执行当前语句并推进处理流程。 */
	v := req("GET", "/api/v1/alarms?deviceId=device-01", token, nil, 200)                                 /* 更新 v 的值。 */
	if v["total"].(float64) != 0 {                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal("query bypass") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	v = req("GET", "/api/v1/dashboard", token, nil, 200)                   /* 更新 v 的值。 */
	if v["devices"].(float64) != 23 || v["activeAlarms"].(float64) != 23 { /* 判断条件并选择处理分支。 */
		t.Fatal("dashboard leaks outside scope", v) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	v = req("GET", "/api/v1/events", token, nil, 200)                      /* 更新 v 的值。 */
	if len(v["alarms"].([]any)) != 23 || len(v["devices"].([]any)) != 23 { /* 判断条件并选择处理分支。 */
		t.Fatal("events leak scope") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req("POST", "/api/v1/mqtt/token", token, nil, 403)                                  /* 执行当前语句并推进处理流程。 */
	req("POST", "/api/v1/mqtt/load-token", token, nil, 403)                             /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/backups", token, nil, 403)                                      /* 执行当前语句并推进处理流程。 */
	req("POST", "/api/v1/ai/chat", token, map[string]string{"question": "列出所有设备"}, 403) /* 执行当前语句并推进处理流程。 */
	// Even a broad dashboard/alarms grant cannot replace device access.
	u["permissions"] = []string{"menu:alarms", "menu:dashboard"}         /* 执行当前语句并推进处理流程。 */
	u["deviceScope"] = "all"                                             /* 执行当前语句并推进处理流程。 */
	req("PUT", "/api/v1/access/users/scope-user", root, u, 200)          /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/events", token, nil, 401)                        /* 执行当前语句并推进处理流程。 */
	token = login("scope-user")                                          /* 更新 token 的值。 */
	v = req("GET", "/api/v1/events", token, nil, 200)                    /* 更新 v 的值。 */
	if len(v["alarms"].([]any)) != 0 || len(v["devices"].([]any)) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("missing device menu must mean no device data") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	v = req("GET", "/api/v1/dashboard", token, nil, 200)                 /* 更新 v 的值。 */
	if v["devices"].(float64) != 0 || v["activeAlarms"].(float64) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("dashboard ignores device menu") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Cross-tenant grants rejected by storage lookup, not only the picker.
	u["deviceScope"] = "selected"                               /* 执行当前语句并推进处理流程。 */
	u["deviceIds"] = []string{"foreign-device"}                 /* 执行当前语句并推进处理流程。 */
	req("PUT", "/api/v1/access/users/scope-user", root, u, 422) /* 执行当前语句并推进处理流程。 */
	// Existing users with no explicit data grant fail closed.
	u["permissions"] = perms                                    /* 执行当前语句并推进处理流程。 */
	delete(u, "deviceScope")                                    /* 执行当前语句并推进处理流程。 */
	delete(u, "deviceIds")                                      /* 执行当前语句并推进处理流程。 */
	req("PUT", "/api/v1/access/users/scope-user", root, u, 200) /* 执行当前语句并推进处理流程。 */
	token = login("scope-user")                                 /* 更新 token 的值。 */
	v = req("GET", "/api/v1/device-registry", token, nil, 200)  /* 更新 v 的值。 */
	if v["total"].(float64) != 0 {                              /* 判断条件并选择处理分支。 */
		t.Fatal("missing scope defaults to all") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	v = req("GET", "/api/v1/device-registry", root, nil, 200) /* 更新 v 的值。 */
	if v["total"].(float64) != 46 {                           /* 判断条件并选择处理分支。 */
		t.Fatal("request scope contaminated administrator") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := engine.Repo.ListManagedDevices(ctx, "tenant_a") /* 更新 e 的值。 */
	must(e)                                                    /* 执行当前语句并推进处理流程。 */
	if len(rows) != 46 {                                       /* 判断条件并选择处理分支。 */
		t.Fatal("request scope contaminated background ingest") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
