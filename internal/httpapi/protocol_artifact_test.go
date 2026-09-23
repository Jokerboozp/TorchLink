package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestLegacyGoProtocolUploadIsUnavailable(t *testing.T) { /* 定义 TestLegacyGoProtocolUploadIsUnavailable 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                                                 /* 更新 cfg 的值。 */
	cfg.DataDir = t.TempDir()                                                                                                                                                            /* 更新 cfg.DataDir 的值。 */
	cfg.DevMode = true                                                                                                                                                                   /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                                                                                                 /* 更新 cfg.JWTSecret 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ExternalParser{Root: cfg.DataDir}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                                                                       /* 更新 engine.Metrics 的值。 */
	if err := engine.Start(context.Background()); err != nil {                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                          /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                                          /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                                                 /* 安排函数结束时执行清理。 */
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK) /* 更新 login 的值。 */
	token := login["accessToken"].(string)                                                                                                                                                               /* 更新 token 的值。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages", token, map[string]any{                                                                                      /* 执行当前语句并推进处理流程。 */
		"id": "protocol_go", "name": "Go Worker", "parserType": parser.GoProtocolParserName, /* 执行当前语句并推进处理流程。 */
	}, http.StatusUnprocessableEntity) /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages/protocol_go/artifact", token, map[string]any{}, http.StatusNotFound) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
