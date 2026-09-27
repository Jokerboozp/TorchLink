package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
)

func TestStandardOnboardingHTTPChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "onboarding-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"
	cfg.MQTTPublicURL = "mqtts://devices.example.test:8883"
	srv := New(cfg, engine, metrics.New(), log)
	token, _ := srv.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"})
	payload := json.RawMessage(`{"id":"a","timestamp":1788850000000,"data":{"temperature":26.5}}`)
	q := onboarding.EnrollRequest{RequestID: "req-1", ProductID: "product", Device: onboarding.EnrollDevice{ID: "device", Name: "传感器"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}}
	call := func(method, path string, body []byte, credential model.DeviceCredential, admin bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Device-Key", credential.AccessKey)
		r.Header.Set("X-Device-Secret", credential.Secret)
		if admin {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	w := call("GET", "/api/v1/onboarding/preflight?productId=product", nil, model.DeviceCredential{}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"standard"`) || !strings.Contains(w.Body.String(), `"ready":true`) {
		t.Fatal("preflight", w.Code, w.Body.String())
	}
	data, _ := json.Marshal(q)
	w = call("POST", "/api/v1/onboarding", data, model.DeviceCredential{}, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created struct {
		onboarding.EnrollResult
		AccessInfo map[string]any `json:"accessInfo"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Credential.Secret == "" || created.AccessInfo["httpUrl"] != "https://devices.example.test/api/v1/device-ingest/standard/tenant/product/device/property" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("device access information", w.Body.String())
	}
	if guide := call("GET", "/api/v1/device-registry/device/connection-guide", nil, model.DeviceCredential{}, true); guide.Code != 404 {
		t.Fatalf("removed connection guide endpoint: %d", guide.Code)
	}
	connection := call("GET", "/api/v1/device-registry/device/connection", nil, model.DeviceCredential{}, true)
	if connection.Code != 200 || !strings.Contains(connection.Body.String(), "WAITING_FOR_DATA") || diagnosisStage(connection) != "WAITING" {
		t.Fatal("configuration falsely reported receipt", connection.Code)
	}
	if replay := call("POST", "/api/v1/onboarding", data, model.DeviceCredential{}, true); replay.Code != 200 || !strings.Contains(replay.Body.String(), `"reused":true`) || strings.Contains(replay.Body.String(), created.Credential.Secret) {
		t.Fatal("lost response retry failed or leaked secret", replay.Code)
	}
	if legacy := call("POST", "/api/v1/device-ingest/device", []byte(`{"protocolId":"untrusted","payload":{"properties":{"x":1}}}`), created.Credential, false); legacy.Code != 422 {
		t.Fatal("standard device bypassed standard ingress", legacy.Code)
	}
	path := "/api/v1/device-ingest/standard/tenant/product/device/property"
	if w = call("POST", path, payload, model.DeviceCredential{}, false); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	if w = call("POST", strings.Replace(path, "/device/", "/other-device/", 1), payload, created.Credential, false); w.Code != 401 {
		t.Fatal("device boundary", w.Code)
	}
	if w = call("POST", strings.Replace(path, "/tenant/", "/other/", 1), payload, created.Credential, false); w.Code != 401 {
		t.Fatal("tenant boundary", w.Code)
	}
	if w = call("POST", path, []byte(strings.Repeat("x", 65537)), created.Credential, false); w.Code != 413 {
		t.Fatal("body limit", w.Code)
	}
	if w = call("POST", path, payload, created.Credential, false); w.Code != http.StatusAccepted {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted struct {
		MessageID string `json:"messageId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	idx, err := repo.GetRawIndex(ctx, "tenant", accepted.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := engine.GetRaw(ctx, idx)
	if err != nil || !bytes.Equal(raw.Payload, payload) {
		t.Fatal("raw archive missing", err)
	}
	m, err := repo.GetStandardMessageByRaw(ctx, "tenant", idx.MessageID)
	if err != nil || m.Properties["temperature"] != 26.5 {
		t.Fatal("standard message missing", err, m)
	}
	if w = call("POST", path, payload, created.Credential, false); w.Code != 202 || !strings.Contains(w.Body.String(), `"created":false`) {
		t.Fatal("duplicate", w.Code, w.Body.String())
	}
	conflict := bytes.Replace(payload, []byte("26.5"), []byte("99.9"), 1)
	if w = call("POST", path, conflict, created.Credential, false); w.Code != 409 || !strings.Contains(w.Body.String(), "MESSAGE_CONFLICT") {
		t.Fatal("conflicting duplicate", w.Code, w.Body.String())
	}
	preserved, err := engine.GetRaw(ctx, idx)
	if err != nil || !bytes.Equal(preserved.Payload, payload) {
		t.Fatal("conflict overwrote archived raw", err)
	}
	connection = call("GET", "/api/v1/device-registry/device/connection", nil, model.DeviceCredential{}, true)
	if connection.Code != 200 || !strings.Contains(connection.Body.String(), `"stage":"PARSED"`) || diagnosisStage(connection) != "PARSED" || !strings.Contains(connection.Body.String(), idx.MessageID) {
		t.Fatal("parsed receipt missing", connection.Code)
	}
	// State still traverses raw archival and parsing before updating connectivity.
	statePayload := []byte(`{"id":"state-1","timestamp":1788850000001,"data":{"connectionStatus":"CONNECTED"}}`)
	w = call("POST", strings.TrimSuffix(path, "property")+"state", statePayload, created.Credential, false)
	if w.Code != 202 {
		t.Fatal("state ingest", w.Code, w.Body.String())
	}
	state, e := repo.GetDeviceState(ctx, "tenant", "device")
	if e != nil || state.ConnectionStatus != "CONNECTED" || state.LastConnectAt != 1788850000001 {
		t.Fatal("state projection", state, e)
	}
	w = call("POST", "/api/v1/device-mqtt/token", nil, created.Credential, false)
	if w.Code != 200 {
		t.Fatal("MQTT credential exchange", w.Code, w.Body.String())
	}
	var mqttToken struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expiresIn"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &mqttToken)
	mqttClaims, e := srv.auth.Parse(mqttToken.Token)
	if e != nil || mqttToken.ExpiresIn != 300 {
		t.Fatal("invalid device MQTT token", e)
	}
	if strings.Contains(w.Body.String(), "shadow") {
		t.Fatal("device token response still advertises shadow topics")
	}
	claimsJSON, _ := json.Marshal(mqttClaims)
	if strings.Contains(string(claimsJSON), "shadow") {
		t.Fatal("device token still authorizes shadow topics")
	}
	if strings.Contains(string(claimsJSON), "/external/raw/") || !strings.Contains(string(claimsJSON), "/iot/up/tenant/product/device/property") {
		t.Fatal("standard device ACL includes raw bypass or misses topic")
	}
	w = call("POST", "/api/v1/device-registry/device/credentials", []byte(`{}`), model.DeviceCredential{}, true)
	if w.Code != 200 {
		t.Fatal("rotate credential", w.Code, w.Body.String())
	}
	var rotated struct {
		Credential model.DeviceCredential `json:"credential"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rotated)
	if w = call("POST", path, payload, created.Credential, false); w.Code != 401 {
		t.Fatal("rotated old credential accepted", w.Code)
	}
	if w = call("POST", path, payload, rotated.Credential, false); w.Code != 202 {
		t.Fatal("new credential rejected", w.Code, w.Body.String())
	}
	created.Credential = rotated.Credential
	w = call("DELETE", "/api/v1/device-registry/device/credentials", nil, model.DeviceCredential{}, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("POST", path, payload, created.Credential, false); w.Code != 401 {
		t.Fatal("disabled", w.Code)
	}
}

func diagnosisStage(w *httptest.ResponseRecorder) string {
	var body struct {
		Diagnosis struct {
			Stage string `json:"stage"`
		} `json:"diagnosis"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Diagnosis.Stage
}

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

func TestDeviceOnboardingBrowser(t *testing.T) {
	if os.Getenv("IOT_TEST_BROWSER") == "" {
		t.Skip("set IOT_TEST_BROWSER to Chrome or Edge after frontend build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	// Parsing runs so the wizard can confirm a real device report.
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "browser-device-onboarding-test-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"
	cfg.MQTTPublicURL = "mqtts://devices.example.test:8883"
	api := New(cfg, engine, metrics.New(), log)
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
	command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "device-onboarding-check.mjs"))
	command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		t.Fatalf("browser: %v\n%s", err, output.String())
	}
	devices, err := repo.ListManagedDevices(ctx, "tenant")
	if err != nil || len(devices) != 2 {
		t.Fatalf("browser device count: %d %v", len(devices), err)
	}
	for _, device := range devices {
		if device.SecretHash == "" {
			t.Fatal("standard device has no credential")
		}
	}
	products, err := repo.ListProducts(ctx, "tenant")
	if err != nil || len(products) != 1 {
		t.Fatalf("browser template count: %d %v", len(products), err)
	}
}

func TestDeviceConnectionBrowser(t *testing.T) {
	if os.Getenv("IOT_TEST_BROWSER") == "" {
		t.Skip("set IOT_TEST_BROWSER after frontend build")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "device-detail-isolated-test-key-32-characters"
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"
	api := New(cfg, engine, metrics.New(), log)
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "long-product-1788991005167", Name: "本地联调产品 1788991005167", Status: "ENABLED", Transport: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "local-check-1788991005167", ProductID: "long-product-1788991005167", Name: "本地联调传感器", DeviceRole: "DIRECT", Status: "ENABLED", AccessKey: "fixture-key", SecretHash: "fixture-hash", Connector: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveStandardMessage(ctx, model.StandardMessage{MessageID: "msg_detail", RawMessageID: "raw_detail", TenantID: "tenant", ProductID: "long-product-1788991005167", DeviceID: "local-check-1788991005167", MessageType: model.PropertyReport, Timestamp: time.Now().UnixMilli(), Properties: map[string]any{"temperature": 42, "location": strings.Repeat("long-device-location/", 12)}}); err != nil {
		t.Fatal(err)
	}
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute)
	viewer, _ := api.auth.Issue("reader", "tenant", "viewer", nil, time.Minute)
	cmd := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "device-connection-check.mjs"))
	cmd.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_VIEWER_TOKEN="+viewer)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}
