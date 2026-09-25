package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                                           /* 执行当前语句并推进处理流程。 */
	"context"                                         /* 执行当前语句并推进处理流程。 */
	"io"                                              /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"           /* 执行当前语句并推进处理流程。 */
	mqttadapter "iot-platform/internal/adapters/mqtt" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"                   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"                    /* 执行当前语句并推进处理流程。 */
	"log/slog"                                        /* 执行当前语句并推进处理流程。 */
	"net"                                             /* 执行当前语句并推进处理流程。 */
	"net/http"                                        /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                               /* 执行当前语句并推进处理流程。 */
	"os"                                              /* 执行当前语句并推进处理流程。 */
	"os/exec"                                         /* 执行当前语句并推进处理流程。 */
	"path/filepath"                                   /* 执行当前语句并推进处理流程。 */
	"strconv"                                         /* 执行当前语句并推进处理流程。 */
	"strings"                                         /* 执行当前语句并推进处理流程。 */
	"testing"                                         /* 执行当前语句并推进处理流程。 */
	"time"                                            /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestOnboardingBrowser(t *testing.T) { /* 定义 TestOnboardingBrowser 函数。 */
	if os.Getenv("IOT_TEST_BROWSER") == "" { /* 判断条件并选择处理分支。 */
		t.Skip("set IOT_TEST_BROWSER to Chromium executable after frontend build") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	outageSeconds := 0                                                      /* 更新 outageSeconds 的值。 */
	if value := os.Getenv("IOT_TEST_BROWSER_OUTAGE_SECONDS"); value != "" { /* 判断条件并选择处理分支。 */
		var err error                                                                                             /* 声明 err。 */
		outageSeconds, err = strconv.Atoi(value)                                                                  /* 更新 err 的值。 */
		if err != nil || outageSeconds < 1 || outageSeconds > 600 || os.Getenv("IOT_TEST_MQTT_WEBSOCKET") == "" { /* 判断条件并选择处理分支。 */
			t.Fatal("outage test requires MQTT WebSocket configuration and a duration from 1 to 600 seconds") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(outageSeconds+120)*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                                                         /* 安排函数结束时执行清理。 */
	root := t.TempDir()                                                                                    /* 更新 root 的值。 */
	repo := memory.NewRepository()                                                                         /* 更新 repo 的值。 */
	archive, err := local.NewArchive(root)                                                                 /* 更新 err 的值。 */
	if err != nil {                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                         /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	if err = engine.Start(ctx); err != nil {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                      /* 更新 cfg 的值。 */
	cfg.DataDir = root                                        /* 更新 cfg.DataDir 的值。 */
	cfg.JWTSecret = "browser-isolated-test-key-32-characters" /* 更新 cfg.JWTSecret 的值。 */
	cfg.AdminTenants = []string{"tenant"}                     /* 更新 cfg.AdminTenants 的值。 */
	if os.Getenv("IOT_TEST_MQTT_WEBSOCKET") != "" {           /* 判断条件并选择处理分支。 */
		if os.Getenv("IOT_TEST_MQTT_JWT_SECRET") == "" || os.Getenv("IOT_TEST_MQTT_BROKER") == "" { /* 判断条件并选择处理分支。 */
			t.Fatal("live browser MQTT requires Broker and JWT test configuration") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		cfg.JWTSecret = os.Getenv("IOT_TEST_MQTT_JWT_SECRET")       /* 更新 cfg.JWTSecret 的值。 */
		cfg.MQTTWebSocketURL = os.Getenv("IOT_TEST_MQTT_WEBSOCKET") /* 更新 cfg.MQTTWebSocketURL 的值。 */
	} /* 结束当前表达式或代码块。 */

	api := New(cfg, engine, metrics.New(), log)                                   /* 更新 api 的值。 */
	if cfg.MQTTWebSocketURL != "" && os.Getenv("IOT_TEST_MQTT_WEBSOCKET") != "" { /* 判断条件并选择处理分支。 */
		username := "browser-probe-" + randomHex(8)                                                                                                                                                                      /* 更新 username 的值。 */
		jwt, e := auth.New(cfg.JWTSecret).IssueWithACL(username, "tenant", "service", nil, []auth.ACLRule{{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"}}, time.Duration(outageSeconds+120)*time.Second) /* 更新 e 的值。 */
		if e != nil {                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		broker, e := mqttadapter.New(os.Getenv("IOT_TEST_MQTT_BROKER"), username, jwt, username) /* 更新 e 的值。 */
		if e != nil {                                                                            /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer broker.Close()                                                                                                  /* 安排函数结束时执行清理。 */
		if e = broker.SubscribeStandard(func(c context.Context, tenant, product, device, kind string, payload []byte) error { /* 判断条件并选择处理分支。 */
			if tenant != "tenant" || device != "browser-device" { /* 判断条件并选择处理分支。 */
				return nil /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			raw, e := api.onboarding.PrepareStandard(c, tenant, product, device, kind, "MQTT", payload) /* 更新 e 的值。 */
			if e == nil {                                                                               /* 判断条件并选择处理分支。 */
				_, _, e = engine.IngestRaw(c, raw) /* 更新 e 的值。 */
			} /* 结束当前表达式或代码块。 */
			return e /* 返回当前处理结果。 */
		}); e != nil { /* 结束当前表达式或代码块。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	api.SetProtocolListeners(browserConnectionSnapshot{})                                                                                   /* 执行当前语句并推进处理流程。 */
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "legacy-product", Name: "历史产品", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "legacy", AccessKey: "legacy-key", ProductID: "legacy-product", Name: "历史设备", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Modbus point tables are published protocol versions; the fixture device is added through onboarding.
	table, _, err := core.ParseModbusPointTable("points.csv", []byte("name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n"), 10)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := core.CompileModbusReadBlocks(table.Points)
	if err != nil {
		t.Fatal(err)
	}
	published := time.Now().UnixMilli()
	table.TenantID, table.ProtocolID, table.Version, table.CreatedAt = "tenant", "browser-modbus", "1", published
	if err = repo.CreatePointTableRelease(ctx, table); err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "browser-modbus", Version: "1", Transport: "MODBUS_TCP", PayloadFormat: "hex", ParserType: parser.ModbusTCPParserName, Status: "PUBLISHED", PointTableVersion: "1", CreatedAt: published, PublishedAt: published, Config: map[string]any{"points": table.Points, "blocks": blocks}}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "browser-modbus-product", Name: "Modbus 测试产品", Status: "ENABLED", ProtocolPackageID: "browser-modbus@1"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"listener-a", "listener-b"} { /* 循环处理当前数据。 */
		if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: id, ProductID: "legacy-product", Mode: "listener", Network: "tcp", Enabled: true}); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Hour)                /* 更新 _ 的值。 */
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))          /* 更新 assets 的值。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if strings.HasPrefix(r.URL.Path, "/api/") { /* 判断条件并选择处理分支。 */
			api.Handler().ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			assets.ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close()                            /* 安排函数结束时执行清理。 */
	modbus, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer modbus.Close() /* 安排函数结束时执行清理。 */
	go func() {          /* 执行当前语句并推进处理流程。 */
		for { /* 循环处理当前数据。 */
			conn, err := modbus.Accept() /* 更新 err 的值。 */
			if err != nil {              /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second)) /* 更新 _ 的值。 */
			request := make([]byte, 12)                           /* 更新 request 的值。 */
			if _, err := io.ReadFull(conn, request); err == nil { /* 判断条件并选择处理分支。 */
				_, _ = conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42}) /* 更新 _ 的值。 */
			} /* 结束当前表达式或代码块。 */
			_ = conn.Close() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "onboarding-check.mjs"))                                     /* 更新 command 的值。 */
	command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_MODBUS_PORT="+strconv.Itoa(modbus.Addr().(*net.TCPAddr).Port)) /* 更新 command.Env 的值。 */
	var output bytes.Buffer                                                                                                                                             /* 声明 output。 */
	writer := io.MultiWriter(&output, os.Stdout)                                                                                                                        /* 更新 writer 的值。 */
	command.Stdout, command.Stderr = writer, writer                                                                                                                     /* 更新 command.Stderr 的值。 */
	err = command.Run()                                                                                                                                                 /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("browser: %v\n%s", err, output.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	devices, err := repo.ListManagedDevices(ctx, "tenant")                 /* 更新 err 的值。 */
	if err != nil || len(devices) != 4 || devices[0].Status != "ENABLED" { /* 判断条件并选择处理分支。 */
		t.Fatalf("browser did not persist enabled device: %+v %v", devices, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	modbusDevice, err := repo.GetManagedDevice(ctx, "tenant", "browser-modbus-preview") /* 更新 err 的值。 */
	if err != nil || modbusDevice.SecretHash != "" {                                    /* 判断条件并选择处理分支。 */
		t.Fatal("browser Modbus onboarding generated a secret", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, propertyCount, err := repo.ListDeviceMessages(ctx, "tenant", "browser-device", model.PropertyReport, 20, 0) /* 更新 err 的值。 */
	expectedCount := 1                                                                                             /* 更新 expectedCount 的值。 */
	if os.Getenv("IOT_TEST_MQTT_WEBSOCKET") != "" {                                                                /* 判断条件并选择处理分支。 */
		expectedCount = 2 /* 更新 expectedCount 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil || propertyCount != expectedCount { /* 判断条件并选择处理分支。 */
		t.Fatalf("retransmission was not deduplicated: count=%d expected=%d error=%v", propertyCount, expectedCount, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// The browser uses a runtime snapshot; no physical listener or device is required.
type browserConnectionSnapshot struct{ connectionSnapshot } /* 定义 browserConnectionSnapshot 类型。 */

func (browserConnectionSnapshot) Sessions(tenant, id string) []map[string]any { /* 定义 Sessions 函数。 */
	if tenant == "tenant" { /* 判断条件并选择处理分支。 */
		return (connectionSnapshot{}).Sessions("t", id) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
