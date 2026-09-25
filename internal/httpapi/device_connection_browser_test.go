package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"os"                /* 执行当前语句并推进处理流程。 */
	"os/exec"           /* 执行当前语句并推进处理流程。 */
	"path/filepath"     /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
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

func TestDeviceConnectionBrowser(t *testing.T) { /* 定义 TestDeviceConnectionBrowser 函数。 */
	if os.Getenv("IOT_TEST_BROWSER") == "" { /* 判断条件并选择处理分支。 */
		t.Skip("set IOT_TEST_BROWSER after frontend build") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                           /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                                           /* 更新 repo 的值。 */
	root := t.TempDir()                                                      /* 更新 root 的值。 */
	archive, err := local.NewArchive(root)                                   /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                         /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	cfg := config.Load()                                                                                          /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                                            /* 更新 cfg.DataDir 的值。 */
	cfg.JWTSecret = "device-detail-isolated-test-key-32-characters"                                               /* 更新 cfg.JWTSecret 的值。 */
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"
	api := New(cfg, engine, metrics.New(), log)                                                                                                                                           /* 更新 api 的值。 */
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "long-product-1788991005167", Name: "本地联调产品 1788991005167", Status: "ENABLED", Transport: "HTTP"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "local-check-1788991005167", ProductID: "long-product-1788991005167", Name: "本地联调传感器", DeviceRole: "DIRECT", Status: "ENABLED", AccessKey: "fixture-key", SecretHash: "fixture-hash", Connector: "HTTP"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveStandardMessage(ctx, model.StandardMessage{MessageID: "msg_detail", RawMessageID: "raw_detail", TenantID: "tenant", ProductID: "long-product-1788991005167", DeviceID: "local-check-1788991005167", MessageType: model.PropertyReport, Timestamp: time.Now().UnixMilli(), Properties: map[string]any{"temperature": 42, "location": strings.Repeat("long-device-location/", 12)}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))          /* 更新 assets 的值。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if strings.HasPrefix(r.URL.Path, "/api/") { /* 判断条件并选择处理分支。 */
			api.Handler().ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			assets.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close()                                                                                                               /* 安排函数结束时执行清理。 */
	token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute)                                                    /* 更新 _ 的值。 */
	viewer, _ := api.auth.Issue("reader", "tenant", "viewer", nil, time.Minute)                                                        /* 更新 _ 的值。 */
	cmd := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "device-connection-check.mjs")) /* 更新 cmd 的值。 */
	cmd.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_VIEWER_TOKEN="+viewer)            /* 更新 cmd.Env 的值。 */
	if out, err := cmd.CombinedOutput(); err != nil {                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatalf("browser: %v\n%s", err, out) /* 验证实际结果符合预期。 */
	} else { /* 结束当前表达式或代码块。 */
		t.Log(string(out)) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
