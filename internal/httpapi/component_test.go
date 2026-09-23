package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"encoding/json"                         /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"           /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net/http"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"os"                                    /* 执行当前语句并推进处理流程。 */
	"os/exec"                               /* 执行当前语句并推进处理流程。 */
	"path/filepath"                         /* 执行当前语句并推进处理流程。 */
	"strings"                               /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestComponentAlarmAPIAndBrowser(t *testing.T) { /* 定义 TestComponentAlarmAPIAndBrowser 函数。 */
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute) /* 更新 cancel 的值。 */
	defer cancel()                                                        /* 安排函数结束时执行清理。 */
	root := t.TempDir()                                                   /* 更新 root 的值。 */
	repo := memory.NewRepository()                                        /* 更新 repo 的值。 */
	archive, err := local.NewArchive(root)                                /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                                                                                                                  /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)                                                                                                          /* 更新 engine 的值。 */
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", ParserType: parser.StandardParserName, Status: "PUBLISHED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = engine.Start(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ProductID: "p", ID: "controller", Name: "一号消防控制器", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                         /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                           /* 更新 cfg.DataDir 的值。 */
	api := New(cfg, engine, metrics.New(), log)                                                  /* 更新 api 的值。 */
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))          /* 更新 assets 的值。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if strings.HasPrefix(r.URL.Path, "/api/") { /* 判断条件并选择处理分支。 */
			api.Handler().ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			assets.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close()                                        /* 安排函数结束时执行清理。 */
	send := func(id string, at int64, part string, fire bool) { /* 更新 send 的值。 */
		t.Helper()                                                                                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
		payload, _ := json.Marshal(map[string]any{"id": id, "timestamp": at, "data": map[string]any{"components": []model.ComponentStatus{{ID: part, Name: "烟感探测器", Location: "二楼走廊", Alarms: map[string]bool{"FIRE": fire, "DEVICE_FAULT": fire}}}}}) /* 更新 _ 的值。 */
		raw, err := onboarding.StandardRaw("tenant", "p", "controller", "event", "HTTP", payload)                                                                                                                                                      /* 更新 err 的值。 */
		if err != nil {                                                                                                                                                                                                                                /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if _, _, err = engine.IngestRaw(ctx, raw); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	send("fire-B", 1000, "loop-1/node-7", true)                                                              /* 执行当前语句并推进处理流程。 */
	send("normal-A", 2000, "loop-1/node-8", false)                                                           /* 执行当前语句并推进处理流程。 */
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant", Status: "ACTIVE", Limit: 100}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 2 {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)                                 /* 更新 _ 的值。 */
	detail := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/alarms/"+alarms[0].ID, token, nil, 200) /* 更新 detail 的值。 */
	if detail["componentId"] != "loop-1/node-7" || detail["componentLocation"] != "二楼走廊" {                       /* 判断条件并选择处理分支。 */
		t.Fatal(detail) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	other, _ := api.auth.Issue("other", "other-tenant", "operator", nil, time.Hour)                    /* 更新 _ 的值。 */
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/alarms/"+alarms[0].ID, other, nil, 404) /* 执行当前语句并推进处理流程。 */
	t.Run("browser", func(t *testing.T) {                                                              /* 执行当前语句并推进处理流程。 */
		if os.Getenv("IOT_TEST_BROWSER") == "" { /* 判断条件并选择处理分支。 */
			t.Skip("Chrome not configured") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "component-alarm-check.mjs")) /* 更新 command 的值。 */
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)                                           /* 更新 command.Env 的值。 */
		out, err := command.CombinedOutput()                                                                                                 /* 更新 err 的值。 */
		if err != nil {                                                                                                                      /* 判断条件并选择处理分支。 */
			t.Fatalf("browser: %v %s", err, out) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		t.Log(string(out)) /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/alarms/"+alarms[0].ID+"/actions", token, map[string]any{"action": "ACKED"}, 200) /* 执行当前语句并推进处理流程。 */
	send("recover-B", 3000, "loop-1/node-7", false)                                                                                              /* 执行当前语句并推进处理流程。 */
	saved, err := repo.GetAlarm(ctx, "tenant", alarms[0].ID)                                                                                     /* 更新 err 的值。 */
	if err != nil || saved.Status != "RECOVERED" {                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(saved, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
