package httpapi

import (
	"bytes"
	"context"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	mqttadapter "iot-platform/internal/adapters/mqtt"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOnboardingBrowser(t *testing.T) {
	if os.Getenv("IOT_TEST_BROWSER") == "" {
		t.Skip("set IOT_TEST_BROWSER to Chromium executable after frontend build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	// 模板默认使用标准协议；租户的标准协议版本由部署初始化，这里直接准备。
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", ParserType: parser.StandardParserName, Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "browser-isolated-test-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	if os.Getenv("IOT_TEST_MQTT_WEBSOCKET") != "" {
		if os.Getenv("IOT_TEST_MQTT_JWT_SECRET") == "" || os.Getenv("IOT_TEST_MQTT_BROKER") == "" {
			t.Fatal("live browser MQTT requires Broker and JWT test configuration")
		}
		cfg.JWTSecret = os.Getenv("IOT_TEST_MQTT_JWT_SECRET")
		cfg.MQTTWebSocketURL = os.Getenv("IOT_TEST_MQTT_WEBSOCKET")
	}

	api := New(cfg, engine, metrics.New(), log)
	if cfg.MQTTWebSocketURL != "" && os.Getenv("IOT_TEST_MQTT_WEBSOCKET") != "" {
		username := "browser-probe-" + randomHex(8)
		jwt, e := auth.New(cfg.JWTSecret).IssueWithACL(username, "tenant", "service", nil, []auth.ACLRule{{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"}}, 2*time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		broker, e := mqttadapter.New(os.Getenv("IOT_TEST_MQTT_BROKER"), username, jwt, username)
		if e != nil {
			t.Fatal(e)
		}
		defer broker.Close()
		if e = broker.SubscribeStandard(func(c context.Context, tenant, product, device, kind string, payload []byte) error {
			if tenant != "tenant" || device != "browser-device" {
				return nil
			}
			raw, e := api.onboarding.PrepareStandard(c, tenant, product, device, kind, "MQTT", payload)
			if e == nil {
				_, _, e = engine.IngestRaw(c, raw)
			}
			return e
		}); e != nil {
			t.Fatal(e)
		}
	}
	api.SetProtocolListeners(browserConnectionSnapshot{})
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "legacy-product", Name: "历史产品", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "legacy", AccessKey: "legacy-key", ProductID: "legacy-product", Name: "历史设备", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
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
	for _, id := range []string{"listener-a", "listener-b"} {
		if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: id, ProductID: "legacy-product", Mode: "listener", Network: "tcp", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Hour)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	modbus, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer modbus.Close()
	go func() {
		for {
			conn, err := modbus.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			request := make([]byte, 12)
			if _, err := io.ReadFull(conn, request); err == nil {
				_, _ = conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42})
			}
			_ = conn.Close()
		}
	}()
	command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "onboarding-check.mjs"))
	command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_MODBUS_PORT="+strconv.Itoa(modbus.Addr().(*net.TCPAddr).Port))
	var output bytes.Buffer
	writer := io.MultiWriter(&output, os.Stdout)
	command.Stdout, command.Stderr = writer, writer
	err = command.Run()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, output.String())
	}

	product, err := repo.GetProduct(ctx, "tenant", "browser-standard-product")
	if err != nil || product.ProtocolPackageID != onboarding.StandardPackageID || product.Status != "ENABLED" {
		t.Fatalf("browser did not persist the standard template: %+v %v", product, err)
	}
	device, err := repo.GetManagedDevice(ctx, "tenant", "browser-device")
	if err != nil || device.Status != "ENABLED" || device.ProductID != product.ID || device.Connector != "HTTP" || device.SecretHash == "" {
		t.Fatalf("browser did not persist the HTTP device: %+v %v", device, err)
	}
	modbusDevice, err := repo.GetManagedDevice(ctx, "tenant", "browser-modbus-preview")
	if err != nil || modbusDevice.SecretHash != "" {
		t.Fatal("browser Modbus onboarding generated a secret", err)
	}
	_, propertyCount, err := repo.ListDeviceMessages(ctx, "tenant", "browser-device", model.PropertyReport, 20, 0)
	expectedCount := 1
	if os.Getenv("IOT_TEST_MQTT_WEBSOCKET") != "" {
		expectedCount = 2
	}
	if err != nil || propertyCount != expectedCount {
		t.Fatalf("retransmission was not deduplicated: count=%d expected=%d error=%v", propertyCount, expectedCount, err)
	}
}

// The browser uses a runtime snapshot; no physical listener or device is required.
type browserConnectionSnapshot struct{ connectionSnapshot }

func (browserConnectionSnapshot) Sessions(tenant, id string) []map[string]any {
	if tenant == "tenant" {
		return (connectionSnapshot{}).Sessions("t", id)
	}
	return nil
}
