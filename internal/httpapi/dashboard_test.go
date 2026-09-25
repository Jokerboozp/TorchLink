package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"fmt"               /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"net/url"           /* 执行当前语句并推进处理流程。 */
	"os"                /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"github.com/jackc/pgx/v5"                 /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5/pgxpool"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/postgres" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"              /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"             /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestDashboard(t *testing.T) { /* 定义 TestDashboard 函数。 */
	t.Run("memory", func(t *testing.T) { checkDashboard(t, memory.NewRepository()) }) /* 执行当前语句并推进处理流程。 */
	t.Run("postgres", func(t *testing.T) {                                            /* 执行当前语句并推进处理流程。 */
		dsn := os.Getenv("IOT_TEST_POSTGRES_DSN") /* 更新 dsn 的值。 */
		if dsn == "" {                            /* 判断条件并选择处理分支。 */
			t.Skip("IOT_TEST_POSTGRES_DSN not configured") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		ctx := context.Background()         /* 更新 ctx 的值。 */
		admin, err := pgxpool.New(ctx, dsn) /* 更新 err 的值。 */
		if err != nil {                     /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer admin.Close()                                               /* 安排函数结束时执行清理。 */
		schema := fmt.Sprintf("dashboard_test_%d", time.Now().UnixNano()) /* 更新 schema 的值。 */
		ident := pgx.Identifier{schema}.Sanitize()                        /* 更新 ident 的值。 */
		if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer func() { /* 安排函数结束时执行清理。 */
			if _, err := admin.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil { /* 判断条件并选择处理分支。 */
				t.Error(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
		u, err := url.Parse(dsn) /* 更新 err 的值。 */
		if err != nil {          /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		q := u.Query()                             /* 更新 q 的值。 */
		q.Set("search_path", schema)               /* 执行当前语句并推进处理流程。 */
		u.RawQuery = q.Encode()                    /* 更新 u.RawQuery 的值。 */
		repo, err := postgres.New(ctx, u.String()) /* 更新 err 的值。 */
		if err != nil {                            /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer repo.Close()      /* 安排函数结束时执行清理。 */
		checkDashboard(t, repo) /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func checkDashboard(t *testing.T, repo ports.Repository) { /* 定义 checkDashboard 函数。 */
	t.Helper()                  /* 执行当前语句并推进处理流程。 */
	ctx := context.Background() /* 更新 ctx 的值。 */
	must := func(err error) {   /* 更新 must 的值。 */
		t.Helper()      /* 执行当前语句并推进处理流程。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	location := time.FixedZone("test", 8*3600)                                                                 /* 更新 location 的值。 */
	now := time.Now().In(location)                                                                             /* 更新 now 的值。 */
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -6).UnixMilli() /* 更新 start 的值。 */
	for _, tenant := range []string{"tenant", "other"} {                                                       /* 循环处理当前数据。 */
		must(repo.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "p", Name: "烟感", Status: "ENABLED"})) /* 执行当前语句并推进处理流程。 */
		for i, status := range []string{"ONLINE", "ALARM", "OFFLINE", "SUSPECTED_OFFLINE", ""} {             /* 循环处理当前数据。 */
			id := fmt.Sprint(i)                                                                                                                         /* 更新 id 的值。 */
			must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: id, ProductID: "p", Status: "ENABLED", AccessKey: tenant + id})) /* 执行当前语句并推进处理流程。 */
			if status != "" {                                                                                                                           /* 判断条件并选择处理分支。 */
				connection, dataStatus := "CONNECTED", "ACTIVE"
				if i >= 2 {
					connection, dataStatus = "DISCONNECTED", "SILENT"
				}
				must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: id, ProductID: "p", BusinessStatus: status, ConnectionStatus: connection, DataStatus: dataStatus})) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: "unregistered", ProductID: "p", BusinessStatus: "ONLINE"})) /* 执行当前语句并推进处理流程。 */
		for i := 0; i < 125; i++ {                                                                                                                 /* 循环处理当前数据。 */
			first := start     /* 更新 first 的值。 */
			level := "HIGH"    /* 更新 level 的值。 */
			status := "ACTIVE" /* 更新 status 的值。 */
			if i == 0 {        /* 判断条件并选择处理分支。 */
				first = start - 1 /* 更新 first 的值。 */
			} /* 结束当前表达式或代码块。 */
			if i == 1 { /* 判断条件并选择处理分支。 */
				first = start + 86400000 /* 更新 first 的值。 */
			} /* 结束当前表达式或代码块。 */
			if i == 2 { /* 判断条件并选择处理分支。 */
				first = now.UnixMilli() + 86400000 /* 更新 first 的值。 */
			} /* 结束当前表达式或代码块。 */
			if i == 3 { /* 判断条件并选择处理分支。 */
				status = "ACKED" /* 更新 status 的值。 */
			} /* 结束当前表达式或代码块。 */
			if i == 4 { /* 判断条件并选择处理分支。 */
				status = "RECOVERED" /* 更新 status 的值。 */
			} /* 结束当前表达式或代码块。 */
			if i == 5 { /* 判断条件并选择处理分支。 */
				level = "CRITICAL" /* 更新 level 的值。 */
			} /* 结束当前表达式或代码块。 */
			alarmType := "FIRE"
			if i%2 == 1 {
				alarmType = "SMOKE_DETECTED"
			}
			_, _, err := repo.UpsertAlarm(ctx, model.Alarm{AlarmType: alarmType, TenantID: tenant, ID: fmt.Sprint(i), DeviceID: fmt.Sprint(i), RuleID: fmt.Sprint(i), AlarmLevel: level, Status: status, FirstTriggeredAt: first, LastTriggeredAt: now.UnixMilli(), TriggerCount: 99}) /* 更新 err 的值。 */
			must(err)                                                                                                                                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	scoped, err := repo.DashboardCountsForDevices(ctx, "tenant", start, now.UnixMilli(), []string{"0", "1"}) /* 更新 err 的值。 */
	must(err)                                                                                                /* 执行当前语句并推进处理流程。 */
	counts := map[string]int{}                                                                               /* 更新 counts 的值。 */
	for _, item := range scoped {                                                                            /* 循环处理当前数据。 */
		counts[item.Kind] += item.Count /* 更新 counts[item.Kind] 的值。 */
	} /* 结束当前表达式或代码块。 */
	if counts["state"] != 2 || counts["product"] != 2 || counts["level"] != 2 || counts["day"] != 1 || counts["connection"] != 2 || counts["dataStatus"] != 2 || counts["alarmStatus"] != 1 || counts["alarmType"] != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("scoped dashboard counts: %+v", scoped) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	emptyScope, err := repo.DashboardCountsForDevices(ctx, "tenant", start, now.UnixMilli(), []string{})
	must(err)
	if len(emptyScope) != 0 {
		t.Fatalf("empty device scope leaked counts: %+v", emptyScope)
	}
	productsByID, err := repo.GetProductsByIDs(ctx, "tenant", []string{"p", "missing"})               /* 更新 err 的值。 */
	must(err)                                                                                         /* 执行当前语句并推进处理流程。 */
	statesByID, err := repo.GetDeviceStatesByIDs(ctx, "tenant", []string{"0", "missing"})             /* 更新 err 的值。 */
	must(err)                                                                                         /* 执行当前语句并推进处理流程。 */
	if len(productsByID) != 1 || len(statesByID) != 1 || statesByID["0"].BusinessStatus != "ONLINE" { /* 判断条件并选择处理分支。 */
		t.Fatalf("batch device lookups: products=%+v states=%+v", productsByID, statesByID) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	must(repo.SaveStandardMessage(ctx, model.StandardMessage{TenantID: "tenant", MessageID: "batch-standard", RawMessageID: "batch-raw", DeviceID: "0", Parser: "batch-test"})) /* 执行当前语句并推进处理流程。 */
	parsedByRaw, err := repo.GetStandardMessagesByRawIDs(ctx, "tenant", []string{"batch-raw", "missing"})                                                                       /* 更新 err 的值。 */
	must(err)                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	if len(parsedByRaw) != 1 || parsedByRaw["batch-raw"].Parser != "batch-test" {                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatalf("batch parsed messages: %+v", parsedByRaw) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	must(repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "tenant", CameraID: "batch-camera", CameraName: "批量测试摄像头", DeviceID: "0", Enabled: true})) /* 执行当前语句并推进处理流程。 */
	camerasByDevice, err := repo.ListVideoCameraMappingsByDeviceIDs(ctx, "tenant", []string{"0", "missing"})                                                            /* 更新 err 的值。 */
	must(err)                                                                                                                                                           /* 执行当前语句并推进处理流程。 */
	if len(camerasByDevice) != 1 || len(camerasByDevice["0"]) != 1 || camerasByDevice["0"][0].CameraID != "batch-camera" {                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("batch camera mappings: %+v", camerasByDevice) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                                                    /* 更新 log 的值。 */
	cfg := config.Load()                                                                                                                                     /* 更新 cfg 的值。 */
	cfg.JWTSecret = "dashboard-test-secret-32-characters"                                                                                                    /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), log)                                                                                            /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                              /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                     /* 安排函数结束时执行清理。 */
	token, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)                                                                                 /* 更新 _ 的值。 */
	result := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?days=7&offset=480", token, nil, 200)                                      /* 更新 result 的值。 */
	if result["devices"] != float64(5) || result["online"] != float64(2) || result["activeAlarms"] != float64(123) || result["highAlarms"] != float64(123) { /* 判断条件并选择处理分支。 */
		t.Fatalf("wrong totals: %v", result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	states := result["states"].(map[string]any)                                          /* 更新 states 的值。 */
	if states["SUSPECTED_OFFLINE"] != float64(1) || states["NEVER_SEEN"] != float64(1) { /* 判断条件并选择处理分支。 */
		t.Fatal(states) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	expectedGroups := map[string]map[string]float64{
		"connections":   {"CONNECTED": 2, "DISCONNECTED": 2, "UNKNOWN": 1},
		"dataStatuses":  {"ACTIVE": 2, "SILENT": 2, "UNKNOWN": 1},
		"alarmStatuses": {"ACTIVE": 121, "ACKED": 1, "RECOVERED": 1},
		"alarmTypes":    {"FIRE": 61, "SMOKE_DETECTED": 62},
	}
	for field, expected := range expectedGroups {
		actual := result[field].(map[string]any)
		if len(actual) != len(expected) {
			t.Fatalf("%s unexpected groups: %v", field, actual)
		}
		for key, value := range expected {
			if actual[key] != value {
				t.Fatalf("%s[%s] = %v, want %v", field, key, actual[key], value)
			}
		}
	}
	trend := result["trend"].([]any)                                                                                                                                                   /* 更新 trend 的值。 */
	if len(trend) != 7 || trend[0].(map[string]any)["count"] != float64(122) || trend[1].(map[string]any)["count"] != float64(1) || trend[2].(map[string]any)["count"] != float64(0) { /* 判断条件并选择处理分支。 */
		t.Fatal(trend) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	products := result["products"].([]any)                                         /* 更新 products 的值。 */
	if len(products) != 1 || products[0].(map[string]any)["count"] != float64(5) { /* 判断条件并选择处理分支。 */
		t.Fatal(products) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	result = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?days=30&offset=-720", token, nil, 200) /* 更新 result 的值。 */
	if len(result["trend"].([]any)) != 30 {                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	emptyToken, _ := api.auth.Issue("viewer", "empty", "viewer", nil, time.Hour)                                         /* 更新 _ 的值。 */
	result = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard", emptyToken, nil, 200)                /* 更新 result 的值。 */
	if result["devices"] != float64(0) || result["activeAlarms"] != float64(0) || len(result["products"].([]any)) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal(result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for field := range expectedGroups {
		if len(result[field].(map[string]any)) != 0 {
			t.Fatalf("empty tenant leaked %s: %v", field, result[field])
		}
	}
	for _, query := range []string{"days=10000", "days=abc", "offset=841", "offset=-721", "offset=abc"} { /* 循环处理当前数据。 */
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?"+query, token, nil, 400) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard", "", nil, 401) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
