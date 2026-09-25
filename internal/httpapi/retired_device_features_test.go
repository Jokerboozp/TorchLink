package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
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

func TestRetiredDeviceFeaturesHaveNoRoutes(t *testing.T) { /* 定义 TestRetiredDeviceFeaturesHaveNoRoutes 函数。 */
	repo := memory.NewRepository()         /* 更新 repo 的值。 */
	root := t.TempDir()                    /* 更新 root 的值。 */
	archive, err := local.NewArchive(root) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                                                               /* 更新 log 的值。 */
	cfg := config.Load()                                                                                                                                                /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                                                                                                  /* 更新 cfg.DataDir 的值。 */
	cfg.JWTSecret = "retired-feature-test-key-32-characters"                                                                                                            /* 更新 cfg.JWTSecret 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)                                     /* 更新 engine 的值。 */
	api := New(cfg, engine, metrics.New(), log)                                                                                                                         /* 更新 api 的值。 */
	if err = repo.SaveManagedDevice(context.Background(), model.ManagedDevice{TenantID: "tenant", ID: "device", ProductID: "product", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Minute) /* 更新 _ 的值。 */
	for _, route := range []struct{ method, path string }{                    /* 循环处理当前数据。 */
		{"GET", "/api/v1/device-twins/device"}, {"PATCH", "/api/v1/device-twin-topology"}, /* 执行当前语句并推进处理流程。 */
		{"GET", "/api/v1/device-registry/device/shadow"}, {"PATCH", "/api/v1/device-registry/device/shadow"}, /* 执行当前语句并推进处理流程。 */
		{"GET", "/api/v1/device-registry/device/shadow/history"}, {"GET", "/api/v1/device-registry/device/shadows"}, {"GET", "/api/v1/device-shadow"}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		req := httptest.NewRequest(route.method, route.path, nil) /* 更新 req 的值。 */
		req.Header.Set("Authorization", "Bearer "+token)          /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                               /* 更新 w 的值。 */
		api.Handler().ServeHTTP(w, req)                           /* 执行当前语句并推进处理流程。 */
		if w.Code != 404 {                                        /* 判断条件并选择处理分支。 */
			t.Fatalf("%s %s: got %d, want 404", route.method, route.path, w.Code) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	req := httptest.NewRequest("GET", "/api/v1/device-registry/device/connection", nil) /* 更新 req 的值。 */
	req.Header.Set("Authorization", "Bearer "+token)                                    /* 执行当前语句并推进处理流程。 */
	w := httptest.NewRecorder()                                                         /* 更新 w 的值。 */
	api.Handler().ServeHTTP(w, req)                                                     /* 执行当前语句并推进处理流程。 */
	if w.Code != 200 {                                                                  /* 判断条件并选择处理分支。 */
		t.Fatalf("device detail was removed with advanced features: %d", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
