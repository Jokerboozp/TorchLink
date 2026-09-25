package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"fmt"               /* 执行当前语句并推进处理流程。 */
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

func TestHealthInspectionPDFDownload(t *testing.T) { /* 定义 TestHealthInspectionPDFDownload 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                                                                    /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(context.Background()); err != nil {                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                                                                 /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                                                                                                                                   /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                                                                                                                 /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                          /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                                          /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                                                 /* 安排函数结束时执行清理。 */
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK) /* 更新 login 的值。 */
	report := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection", login["accessToken"].(string), map[string]any{}, http.StatusOK)                                /* 更新 report 的值。 */
	generatedAt, ok := report["generatedAt"].(float64)                                                                                                                                                   /* 更新 ok 的值。 */
	if !ok || generatedAt <= 0 {                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatalf("health inspection generatedAt = %#v", report["generatedAt"]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/health-inspection/pdf", bytes.NewReader([]byte(`{}`))) /* 更新 err 的值。 */
	if err != nil {                                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	request.Header.Set("Authorization", "Bearer "+login["accessToken"].(string)) /* 执行当前语句并推进处理流程。 */
	request.Header.Set("Content-Type", "application/json")                       /* 执行当前语句并推进处理流程。 */
	response, err := server.Client().Do(request)                                 /* 更新 err 的值。 */
	if err != nil {                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()            /* 安排函数结束时执行清理。 */
	data, err := io.ReadAll(response.Body) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/pdf" || !bytes.Contains([]byte(response.Header.Get("Content-Disposition")), []byte(fmt.Sprintf("health-inspection-%d.pdf", int64(generatedAt)))) || !bytes.HasPrefix(data, []byte("%PDF-1.4")) { /* 判断条件并选择处理分支。 */
		t.Fatalf("PDF response status=%d type=%q disposition=%q prefix=%q", response.StatusCode, response.Header.Get("Content-Type"), response.Header.Get("Content-Disposition"), data[:min(len(data), 8)]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
