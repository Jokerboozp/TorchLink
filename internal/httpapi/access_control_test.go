package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"regexp"                                /* 执行当前语句并推进处理流程。 */
	"strings"                               /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestAccessControlLifecycleAndIsolation(t *testing.T) { /* 定义 TestAccessControlLifecycleAndIsolation 函数。 */
	repo := memory.NewRepository()                                                         /* 更新 repo 的值。 */
	cfg := config.Load()                                                                   /* 更新 cfg 的值。 */
	cfg.AdminUser = "root"                                                                 /* 更新 cfg.AdminUser 的值。 */
	cfg.AdminPassword = "root-password-test"                                               /* 更新 cfg.AdminPassword 的值。 */
	cfg.AdminTenants = []string{"tenant_a", "tenant_b"}                                    /* 更新 cfg.AdminTenants 的值。 */
	cfg.JWTSecret = "test-only-secret-for-iam-at-least-32"                                 /* 更新 cfg.JWTSecret 的值。 */
	cfg.DevMode = true                                                                     /* 更新 cfg.DevMode 的值。 */
	engine := &core.Engine{Repo: repo}                                                     /* 更新 engine 的值。 */
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                            /* 更新 server 的值。 */
	defer server.Close()                                                                   /* 安排函数结束时执行清理。 */
	req := func(method, path, token string, body any, status int) map[string]any {         /* 更新 req 的值。 */
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	login := func(user, password, tenant string, status int) map[string]any { /* 更新 login 的值。 */
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": tenant}, status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root := login("root", cfg.AdminPassword, "tenant_a", 200)["accessToken"].(string)                                                                                                                                   /* 更新 root 的值。 */
	role := map[string]any{"id": "device_reader", "name": "设备查看", "permissions": []string{"menu:devices"}}                                                                                                              /* 更新 role 的值。 */
	req("POST", "/api/v1/access/roles", root, role, 200)                                                                                                                                                                /* 执行当前语句并推进处理流程。 */
	user := map[string]any{"username": "demo_user", "displayName": "演示用户", "password": "initial-password-test", "enabled": true, "roleIds": []string{"device_reader"}, "permissions": []string{}, "deviceScope": "all"} /* 更新 user 的值。 */
	req("POST", "/api/v1/access/users", root, user, 200)                                                                                                                                                                /* 执行当前语句并推进处理流程。 */
	token := login("demo_user", "initial-password-test", "tenant_a", 200)["accessToken"].(string)                                                                                                                       /* 更新 token 的值。 */
	login("demo_user", "initial-password-test", "tenant_b", 401)                                                                                                                                                        /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/device-registry", token, nil, 200)                                                                                                                                                              /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/products", token, nil, 200)                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */ // Read-only lookup for device editor.
	req("POST", "/api/v1/device-registry", token, map[string]string{}, 403)                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/access/users", token, nil, 403)                                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v2/protocols/test/releases/v1/source", token, nil, 403)                                                                                                                                            /* 执行当前语句并推进处理流程。 */
	req("POST", "/mcp", token, map[string]string{}, 403)                                                                                                                                                                /* 执行当前语句并推进处理流程。 */
	state, err := repo.LoadAccessState(context.Background(), "tenant_a")                                                                                                                                                /* 更新 err 的值。 */
	if err != nil || state.Users[0].PasswordHash == "initial-password-test" || state.Users[0].PasswordHash == "" {                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal("password is not hashed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	list := req("GET", "/api/v1/access/users", root, nil, 200) /* 更新 list 的值。 */
	for _, u := range list["items"].([]any) {                  /* 循环处理当前数据。 */
		if _, ok := u.(map[string]any)["passwordHash"]; ok { /* 判断条件并选择处理分支。 */
			t.Fatal("password hash exposed") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	// Permissions are loaded on every request, so existing tokens reflect role edits.
	role["permissions"] = []string{"menu:devices", "POST /api/v1/device-registry"} /* 执行当前语句并推进处理流程。 */
	req("PUT", "/api/v1/access/roles/device_reader", root, role, 200)              /* 执行当前语句并推进处理流程。 */
	req("POST", "/api/v1/device-registry", token, map[string]string{}, 422)        /* 执行当前语句并推进处理流程。 */
	role["permissions"] = []string{}                                               /* 执行当前语句并推进处理流程。 */
	req("PUT", "/api/v1/access/roles/device_reader", root, role, 200)              /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/device-registry", token, nil, 403)                         /* 执行当前语句并推进处理流程。 */
	for _, permission := range api.permissionCatalog() {                           /* 循环处理当前数据。 */
		if permission.Kind != "action" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		parts := strings.SplitN(permission.ID, " ", 2)                                     /* 更新 parts 的值。 */
		path := regexp.MustCompile(`:[A-Za-z]+`).ReplaceAllString(parts[1], "denied-test") /* 更新 path 的值。 */
		req(parts[0], path, token, map[string]any{}, 403)                                  /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	role["permissions"] = []string{"*"}                                                                                           /* 执行当前语句并推进处理流程。 */
	req("PUT", "/api/v1/access/roles/device_reader", root, role, 422)                                                             /* 执行当前语句并推进处理流程。 */
	role["permissions"] = []string{}                                                                                              /* 执行当前语句并推进处理流程。 */
	req("DELETE", "/api/v1/access/roles/device_reader", root, nil, 409)                                                           /* 执行当前语句并推进处理流程。 */
	req("POST", "/api/v1/access/users/demo_user/password", root, map[string]string{"password": "replacement-password-test"}, 200) /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/auth/me", token, nil, 401)                                                                                /* 执行当前语句并推进处理流程。 */
	login("demo_user", "initial-password-test", "tenant_a", 401)                                                                  /* 执行当前语句并推进处理流程。 */
	token = login("demo_user", "replacement-password-test", "tenant_a", 200)["accessToken"].(string)                              /* 更新 token 的值。 */
	user["enabled"] = false                                                                                                       /* 执行当前语句并推进处理流程。 */
	req("PUT", "/api/v1/access/users/demo_user", root, user, 200)                                                                 /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/auth/me", token, nil, 401)                                                                                /* 执行当前语句并推进处理流程。 */
	login("demo_user", "replacement-password-test", "tenant_a", 401)                                                              /* 执行当前语句并推进处理流程。 */
	otherRoot := login("root", cfg.AdminPassword, "tenant_b", 200)["accessToken"].(string)                                        /* 更新 otherRoot 的值。 */
	req("DELETE", "/api/v1/access/users/demo_user", otherRoot, nil, 404)                                                          /* 执行当前语句并推进处理流程。 */
	req("DELETE", "/api/v1/access/users/demo_user", root, nil, 200)                                                               /* 执行当前语句并推进处理流程。 */
	user["enabled"] = true                                                                                                        /* 执行当前语句并推进处理流程。 */
	req("POST", "/api/v1/access/users", root, user, 200)                                                                          /* 执行当前语句并推进处理流程。 */
	req("GET", "/api/v1/auth/me", token, nil, 401)                                                                                /* 执行当前语句并推进处理流程。 */
	// Old revisions may not overwrite concurrent changes.
	if ok, err := repo.SaveAccessState(context.Background(), "tenant_a", state); err != nil || ok { /* 判断条件并选择处理分支。 */
		t.Fatal("stale access state was accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	broker, err := api.auth.IssueBrowserMQTT("demo_user", "tenant_a", []string{"/iot/alarm/tenant_a/#"}, 1000000000) /* 更新 err 的值。 */
	if err != nil {                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req("GET", "/api/v1/device-registry", broker, nil, 403) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func TestUserPermissionsCombineRolesAndIndividualGrants(t *testing.T) { /* 定义 TestUserPermissionsCombineRolesAndIndividualGrants 函数。 */
	state := model.AccessState{Roles: []model.PlatformRole{{ID: "reader", Permissions: []string{"menu:devices"}}}}                       /* 更新 state 的值。 */
	viewer := model.PlatformUser{DeviceScope: "all", RoleIDs: []string{"reader"}}                                                        /* 更新 viewer 的值。 */
	editor := model.PlatformUser{DeviceScope: "all", RoleIDs: []string{"reader"}, Permissions: []string{"POST /api/v1/device-registry"}} /* 更新 editor 的值。 */
	if allowsRoute(effectivePermissions(state, viewer), "POST", "/api/v1/device-registry") {                                             /* 判断条件并选择处理分支。 */
		t.Fatal("viewer unexpectedly inherited another user's permission") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !allowsRoute(effectivePermissions(state, editor), "POST", "/api/v1/device-registry") { /* 判断条件并选择处理分支。 */
		t.Fatal("individual grant missing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !allowsRoute(effectivePermissions(state, editor), "GET", "/api/v1/device-registry") { /* 判断条件并选择处理分支。 */
		t.Fatal("role menu missing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
