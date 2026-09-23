package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net/http"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestExecutionRouteUsesTenantLeaseAndPreservesAuth(t *testing.T) { /* 定义 TestExecutionRouteUsesTenantLeaseAndPreservesAuth 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log) /* 更新 engine 的值。 */
	cfg := config.Load()                                                                                                 /* 更新 cfg 的值。 */
	cfg.ProcessRole = "gateway"                                                                                          /* 更新 cfg.ProcessRole 的值。 */
	cfg.AccessCoordination = true                                                                                        /* 更新 cfg.AccessCoordination 的值。 */
	cfg.AccessNodeURL = "http://local"                                                                                   /* 更新 cfg.AccessNodeURL 的值。 */
	cfg.JWTSecret = "routing-shared-test-secret"                                                                         /* 更新 cfg.JWTSecret 的值。 */
	server := New(cfg, engine, metrics.New(), log)                                                                       /* 更新 server 的值。 */
	token, err := server.auth.Issue("operator", "tenant", "operator", nil, time.Minute)                                  /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var received bool                                                                            /* 声明 received。 */
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 remote 的值。 */
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Iot-Gateway-Hops") != "1" { /* 判断条件并选择处理分支。 */
			t.Error("forward lost auth or routing bound") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		received = true    /* 更新 received 的值。 */
		w.WriteHeader(202) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer remote.Close()                                                                                                                  /* 安排函数结束时执行清理。 */
	if _, ok, err := repo.AcquireExecutionLease(ctx, "tenant", "profile/profile", "remote", remote.URL, time.Minute); err != nil || !ok { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "device", Tags: map[string]string{"connectorProfileId": "profile"}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	r := httptest.NewRequest("POST", "/api/v1/device-registry/device/commands", nil) /* 更新 r 的值。 */
	r.Header.Set("Authorization", "Bearer "+token)                                   /* 执行当前语句并推进处理流程。 */
	w := httptest.NewRecorder()                                                      /* 更新 w 的值。 */
	server.Handler().ServeHTTP(w, r)                                                 /* 执行当前语句并推进处理流程。 */
	if w.Code != 202 || !received {                                                  /* 判断条件并选择处理分支。 */
		t.Fatal("did not reach owning runtime", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	other, err := server.auth.Issue("operator", "other-tenant", "operator", nil, time.Minute) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	r.Header.Set("Authorization", "Bearer "+other)         /* 执行当前语句并推进处理流程。 */
	received = false                                       /* 更新 received 的值。 */
	if target := server.executionTarget(r); target != "" { /* 判断条件并选择处理分支。 */
		t.Fatal("cross tenant routed", target) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	r.Header.Set("Authorization", "Bearer "+token) /* 执行当前语句并推进处理流程。 */
	r.Header.Set("X-Iot-Gateway-Hops", "2")        /* 执行当前语句并推进处理流程。 */
	w = httptest.NewRecorder()                     /* 更新 w 的值。 */
	server.Handler().ServeHTTP(w, r)               /* 执行当前语句并推进处理流程。 */
	if w.Code != 503 || received {                 /* 判断条件并选择处理分支。 */
		t.Fatal("route loop not bounded", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
