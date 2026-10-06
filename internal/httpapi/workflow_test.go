package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func TestHTTPWorkflow(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}, parser.JavaScriptParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	installEndpointWorkflows(engine)
	engine.AIPlugins = aiadapter.NewProviderRegistry()
	engine.KB = knowledge.NewLocal()
	engine.Metrics = metrics.New()
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	cfg.CORSAllowedOrigins = []string{"http://localhost:5173"}
	cfg.AdminTenants = []string{"tenant_001", "tenant_002", "tenant_video_other"}
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, ok := api.Handler().(*gin.Engine); !ok {
		t.Fatalf("HTTP server is not backed by Gin: %T", api.Handler())
	}
	api.router.GET("/__panic_test", func(c *gin.Context) { panic("test panic") })
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	healthResp, err := server.Client().Get(server.URL + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK || healthResp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("Gin security middleware failed: status=%d headers=%v", healthResp.StatusCode, healthResp.Header)
	}
	preflight, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/v1/products", nil)
	preflight.Header.Set("Origin", "http://localhost:5173")
	preflightResp, err := server.Client().Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	preflightResp.Body.Close()
	if preflightResp.StatusCode != http.StatusNoContent || preflightResp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("CORS preflight failed: status=%d headers=%v", preflightResp.StatusCode, preflightResp.Header)
	}
	methodReq, _ := http.NewRequest(http.MethodPost, server.URL+"/health/live", nil)
	methodResp, err := server.Client().Do(methodReq)
	if err != nil {
		t.Fatal(err)
	}
	methodResp.Body.Close()
	if methodResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("Gin method handling status=%d want=%d", methodResp.StatusCode, http.StatusMethodNotAllowed)
	}
	panicResp, err := server.Client().Get(server.URL + "/__panic_test")
	if err != nil {
		t.Fatal(err)
	}
	panicResp.Body.Close()
	if panicResp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("Gin recovery status=%d want=%d", panicResp.StatusCode, http.StatusInternalServerError)
	}
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/products", "", nil, http.StatusUnauthorized)
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, 200)
	token := login["accessToken"].(string)
	providers := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/providers", token, nil, 200)
	if providers["mode"] != "plugin-harness" || len(providers["items"].([]any)) != 3 {
		t.Fatalf("unexpected AI provider registry %#v", providers)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant_001", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewerProviders := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/providers", viewerToken, nil, 200)
	for _, item := range viewerProviders["items"].([]any) {
		if _, exposed := item.(map[string]any)["defaultBaseUrl"]; exposed {
			t.Fatalf("viewer received provider endpoint %#v", item)
		}
	}
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "AI 插件测试成功"}}}})
	}))
	defer providerServer.Close()
	providerTest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{"provider": "openai-compatible", "baseUrl": providerServer.URL, "model": "test-model", "question": "连接测试"}, 200)
	if providerTest["success"] != true || providerTest["answer"] != "AI 插件测试成功" || !strings.HasPrefix(providerTest["traceId"].(string), "ai_trace_") {
		t.Fatalf("unexpected AI provider test %#v", providerTest)
	}
	privateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected private model request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "私有化模型连接成功"}}}})
	}))
	defer privateServer.Close()
	privateTest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{"provider": "openai-compatible", "baseUrl": privateServer.URL + "/v1", "model": "Qwen/Qwen3-8B", "question": "连接测试"}, 200)
	if privateTest["success"] != true || privateTest["answer"] != "私有化模型连接成功" {
		t.Fatalf("keyless private vLLM provider test failed %#v", privateTest)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{"provider": "ollama", "baseUrl": "http://localhost:11434", "model": "qwen3:1.7b"}, http.StatusUnprocessableEntity)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{"cameraId": "camera_metadata", "cameraName": "园区入口", "brand": "海康", "cameraPoint": "东门", "building": "A", "floor": "1", "room": "大厅", "enabled": true}, 201)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras/camera_metadata/preview", token, map[string]any{}, http.StatusNotFound)
	viewerCameras := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/cameras", viewerToken, nil, 200)
	viewerCamera := viewerCameras["items"].([]any)[0].(map[string]any)
	if _, exposed := viewerCamera["streamUrl"]; exposed || viewerCamera["cameraName"] != "园区入口" || viewerCamera["brand"] != "海康" {
		t.Fatalf("viewer camera metadata is incomplete or contains stream data %#v", viewerCamera)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages", token, map[string]any{"id": "protocol_json", "name": "JSON 通用协议", "version": "1.0.0", "protocol": "json", "transport": "HTTP", "payloadFormat": "json", "parserType": "custom_json_parser", "status": "PUBLISHED"}, 201)
	protocolTest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages/protocol_json/test", token, map[string]any{"payload": map[string]any{"properties": map[string]any{"temperature": 22.5}}}, 200)
	if protocolTest["success"] != true {
		t.Fatalf("protocol test failed: %#v", protocolTest)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages", token, map[string]any{"id": "protocol_javascript", "name": "JavaScript 解析协议", "version": "1.0.0", "protocol": "javascript", "transport": "HTTP", "payloadFormat": "hex", "parserType": parser.JavaScriptParserName, "status": "PUBLISHED", "config": map[string]any{"source": "function parse(raw) { const b = hexToBytes(raw.payload); return {properties: {temperature: b[0] / 10}} }"}}, 422)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/products", token, map[string]any{"id": "product_json", "name": "JSON 传感器", "category": "sensor", "protocolPackageId": "protocol_json", "status": "ENABLED"}, 201)
	managed := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry", token, map[string]any{"id": "device_managed", "name": "受管测试设备", "productId": "product_json", "status": "ENABLED", "trial": true}, 201)
	credential := managed["credential"].(map[string]any)
	publicBody, _ := json.Marshal(map[string]any{"messageId": "raw_managed", "payload": map[string]any{"properties": map[string]any{"temperature": 22.5}}})
	publicReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/device-ingest/device_managed", bytes.NewReader(publicBody))
	publicReq.Header.Set("Content-Type", "application/json")
	publicReq.Header.Set("X-Device-Key", credential["accessKey"].(string))
	publicReq.Header.Set("X-Device-Secret", credential["secret"].(string))
	publicResp, err := server.Client().Do(publicReq)
	if err != nil || publicResp.StatusCode != 201 {
		t.Fatalf("managed ingest status=%v err=%v", publicResp, err)
	}
	publicResp.Body.Close()
	registry := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/device-registry", token, nil, 200)
	if registry["total"].(float64) != 1 {
		t.Fatalf("unexpected registry: %#v", registry)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/rules", token, map[string]any{"id": "rule_e2e", "name": "高温烟雾", "alarmType": "FIRE_RISK", "level": "HIGH", "match": "all", "enabled": true, "conditions": []map[string]any{{"field": "temperature", "operator": ">", "value": 80}, {"field": "smoke", "operator": "eq", "value": true}}}, 201)
	now := time.Now().UnixMilli()
	ingest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/raw-messages", token, map[string]any{"messageId": "raw_http_e2e", "tenantId": "tenant_001", "productId": "json_smoke", "deviceId": "device_http_e2e", "protocol": "json", "payloadFormat": "json", "receivedAt": now, "payload": map[string]any{"properties": map[string]any{"temperature": 88.5, "smoke": true}, "tags": map[string]any{"cityCode": "city_001", "districtCode": "district_01", "buildingId": "A", "deviceType": "smoke"}}}, 201)
	if ingest["created"] != true {
		t.Fatalf("raw not created: %#v", ingest)
	}
	alarms := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?status=ACTIVE", token, nil, 200)
	if alarms["total"].(float64) != 1 {
		t.Fatalf("unexpected alarms %#v", alarms)
	}
	items := alarms["items"].([]any)
	alarmID := items[0].(map[string]any)["alarmId"].(string)
	rawDetail := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/raw-messages/raw_http_e2e", token, nil, 200)
	if rawDetail["message"].(map[string]any)["messageId"] != "raw_http_e2e" {
		t.Fatalf("unexpected raw detail %#v", rawDetail)
	}
	if rawDetail["parseStatus"] != "PARSED" || rawDetail["standardMessage"].(map[string]any)["messageType"] != "PROPERTY_REPORT" {
		t.Fatalf("raw detail did not expose parsed standard message %#v", rawDetail)
	}
	downloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/raw-messages/raw_http_e2e/download", nil)
	downloadReq.Header.Set("Authorization", "Bearer "+token)
	downloadResp, err := server.Client().Do(downloadReq)
	if err != nil {
		t.Fatal(err)
	}
	downloadBody, _ := io.ReadAll(downloadResp.Body)
	downloadResp.Body.Close()
	if downloadResp.StatusCode != 200 || !strings.Contains(downloadResp.Header.Get("Content-Disposition"), "raw_http_e2e.json") || !bytes.Contains(downloadBody, []byte(`"messageId": "raw_http_e2e"`)) {
		t.Fatalf("raw download status=%d disposition=%q body=%s", downloadResp.StatusCode, downloadResp.Header.Get("Content-Disposition"), downloadBody)
	}
	batchRequestBody, _ := json.Marshal(map[string]any{"messageIds": []string{"raw_managed", "raw_http_e2e"}})
	batchReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/raw-messages/download", bytes.NewReader(batchRequestBody))
	batchReq.Header.Set("Authorization", "Bearer "+token)
	batchReq.Header.Set("Content-Type", "application/json")
	batchResp, err := server.Client().Do(batchReq)
	if err != nil {
		t.Fatal(err)
	}
	batchBody, _ := io.ReadAll(batchResp.Body)
	batchResp.Body.Close()
	if batchResp.StatusCode != 200 || batchResp.Header.Get("Content-Type") != "application/zip" || batchResp.Header.Get("X-Archive-Count") != "2" {
		t.Fatalf("batch raw download status=%d type=%q count=%q body=%s", batchResp.StatusCode, batchResp.Header.Get("Content-Type"), batchResp.Header.Get("X-Archive-Count"), batchBody)
	}
	zr, err := zip.NewReader(bytes.NewReader(batchBody), int64(len(batchBody)))
	if err != nil {
		t.Fatal(err)
	}
	zipNames := map[string]bool{}
	for _, file := range zr.File {
		zipNames[file.Name] = true
	}
	if !zipNames["清单.json"] || !zipNames["报文/001_raw_managed.json"] || !zipNames["报文/002_raw_http_e2e.json"] {
		t.Fatalf("unexpected ZIP entries %#v", zipNames)
	}
	updatedRule := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/rules/rule_e2e", token, map[string]any{"name": "更新后的高温规则", "alarmType": "FIRE_RISK", "level": "CRITICAL", "match": "all", "enabled": true, "conditions": []map[string]any{{"field": "temperature", "operator": ">", "value": 90}}}, 200)
	if updatedRule["id"] != "rule_e2e" || updatedRule["name"] != "更新后的高温规则" {
		t.Fatalf("rule not updated %#v", updatedRule)
	}
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/rules/rule_e2e", token, nil, 200)
	rules := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/rules", token, nil, 200)
	if len(rules["items"].([]any)) != 0 {
		t.Fatalf("rule not deleted %#v", rules)
	}
	analysisURL := server.URL + "/api/v1/ai/alarm-analysis/" + alarmID
	requestJSON(t, server.Client(), http.MethodGet, analysisURL, token, nil, http.StatusNotFound)
	requestJSON(t, server.Client(), http.MethodGet, analysisURL+"/progress", token, nil, http.StatusNotFound)
	job := requestJSON(t, server.Client(), http.MethodPost, analysisURL+"/run", token, map[string]any{}, http.StatusAccepted)
	analysisDeadline := time.Now().Add(2 * time.Second)
	for {
		progress := requestJSON(t, server.Client(), http.MethodGet, analysisURL+"/progress/"+job["jobId"].(string), token, nil, http.StatusOK)
		if progress["status"] == "succeeded" {
			break
		}
		if progress["status"] == "failed" || time.Now().After(analysisDeadline) {
			t.Fatalf("manual analysis did not finish: %#v", progress)
		}
		time.Sleep(10 * time.Millisecond)
	}
	requestJSON(t, server.Client(), http.MethodGet, analysisURL, token, nil, http.StatusOK)
	otherLogin := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_002"}, 200)
	otherToken := otherLogin["accessToken"].(string)
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/alarm-analysis/"+alarmID, otherToken, nil, http.StatusNotFound)
	devices := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices", token, nil, 200)
	if devices["online"].(float64) != 2 {
		t.Fatalf("unexpected devices %#v", devices)
	}
	replay := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/raw-messages/replay", token, map[string]any{"start": now - 1000, "end": now + 1000, "mode": "DRY_RUN", "ratePerSecond": 1000}, 202)
	replayID := replay["id"].(string)
	deadline := time.Now().Add(2 * time.Second)
	for {
		state := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/replays/"+replayID, token, nil, 200)
		if state["status"] == "COMPLETED" {
			if state["processed"].(float64) != 2 {
				t.Fatalf("unexpected replay %#v", state)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("replay timeout %#v", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/raw-messages", token, map[string]any{"messageId": "raw_discovered_e2e", "tenantId": "tenant_001", "productId": "product_json", "deviceId": "discovered_1", "protocol": "json", "payloadFormat": "json", "payload": map[string]any{"properties": map[string]any{"temperature": 23.5}}}, 201)
	// Daily registration reuses a prepared template. Actual field evidence and
	// the first-device acceptance transition are covered by the preparation test.
	readyTemplateFixture(t, api, "tenant_001", "product_json")
	discovered := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/discovered-devices/discovered_1/register", token, map[string]any{}, 201)
	if discovered["device"].(map[string]any)["deviceRole"] != "DIRECT" {
		t.Fatalf("unexpected discovered registration %#v", discovered)
	}
	gateway := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry", token, map[string]any{"id": "gateway_1", "name": "一号网关", "productId": "product_json", "status": "ENABLED", "deviceRole": "GATEWAY"}, 201)
	gatewayCredential := gateway["credential"].(map[string]any)
	childBody, _ := json.Marshal(map[string]any{"messageId": "raw_gateway_child_e2e", "deviceId": "child_1", "deviceName": "一号子设备", "productId": "product_json", "payload": map[string]any{"properties": map[string]any{"temperature": 24.5}}})
	childReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/device-ingest/gateway_1", bytes.NewReader(childBody))
	childReq.Header.Set("Content-Type", "application/json")
	childReq.Header.Set("X-Device-Key", gatewayCredential["accessKey"].(string))
	childReq.Header.Set("X-Device-Secret", gatewayCredential["secret"].(string))
	childResp, err := server.Client().Do(childReq)
	if err != nil {
		t.Fatal(err)
	}
	childResp.Body.Close()
	if childResp.StatusCode != 201 {
		t.Fatalf("gateway child ingest status=%d", childResp.StatusCode)
	}
	registryAfterGateway := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/device-registry", token, nil, 200)
	foundChild, gatewayChildCount := false, float64(0)
	for _, item := range registryAfterGateway["items"].([]any) {
		row := item.(map[string]any)
		device := row["device"].(map[string]any)
		if device["id"] == "child_1" {
			foundChild = device["deviceRole"] == "CHILD" && device["gatewayId"] == "gateway_1" && device["autoRegistered"] == true
		}
		if device["id"] == "gateway_1" {
			gatewayChildCount = row["childCount"].(float64)
		}
	}
	if !foundChild || gatewayChildCount != 1 {
		t.Fatalf("gateway relation missing %#v", registryAfterGateway)
	}
	testFixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated)
	testDevice := testFixture["device"].(map[string]any)
	testDeviceID := testDevice["id"].(string)
	testProduct := testFixture["product"].(map[string]any)
	if testProduct["status"] != "ENABLED" || testFixture["protocolPackage"].(map[string]any)["status"] != "PUBLISHED" {
		t.Fatalf("test fixture is not ready %#v", testFixture)
	}
	testRules := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/rules", token, nil, http.StatusOK)
	if len(testRules["items"].([]any)) != 0 {
		t.Fatalf("test fixture unexpectedly created an alarm rule %#v", testRules)
	}
	fixtureAgain := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusOK)
	if fixtureAgain["device"].(map[string]any)["id"] != testDeviceID {
		t.Fatalf("test fixture was not repeatable %#v", fixtureAgain)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/rules", token, map[string]any{
		"id":        "rule_test_device_navigation",
		"name":      "测试设备报警后打开设备管理",
		"productId": testProduct["id"],
		"alarmType": "FIRE_RISK",
		"level":     "HIGH",
		"match":     "all",
		"enabled":   true,
		"conditions": []map[string]any{
			{"field": "temperature", "operator": ">", "value": 80},
			{"field": "smoke", "operator": "eq", "value": true},
		},
		"actions": []map[string]any{{"type": "OPEN_PAGE", "page": "devices"}},
	}, http.StatusCreated)
	testIngest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+testDeviceID+"/debug", token, map[string]any{
		"messageId": "raw_test_device_alarm_e2e",
		"payload": map[string]any{
			"alarm":      true,
			"properties": map[string]any{"temperature": 88.5, "smoke": true, "battery": 92},
			"tags":       map[string]any{"cityCode": "city_001", "districtCode": "district_01", "buildingId": "A-01", "deviceType": "smoke"},
		},
	}, http.StatusCreated)
	if testIngest["created"] != true {
		t.Fatalf("test fixture alarm raw was not created %#v", testIngest)
	}
	testRaw := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/raw-messages/raw_test_device_alarm_e2e", token, nil, http.StatusOK)
	if testRaw["parseStatus"] != "PARSED" || testRaw["standardMessage"].(map[string]any)["messageType"] != "ALARM_REPORT" {
		t.Fatalf("test fixture alarm was not parsed %#v", testRaw)
	}
	testAlarms := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+testDeviceID+"&status=ACTIVE", token, nil, http.StatusOK)
	if testAlarms["total"].(float64) < 1 {
		t.Fatalf("test fixture alarm rule did not trigger %#v", testAlarms)
	}
	resp, err := server.Client().Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("backend root status=%d, want=%d", resp.StatusCode, http.StatusNotFound)
	}
}

func requestJSON(t *testing.T, client *http.Client, method, url, token string, body any, want int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s status=%d want=%d body=%#v", method, url, resp.StatusCode, want, out)
	}
	return out
}

func TestComponentAlarmAPIAndBrowser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", ParserType: parser.StandardParserName, Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ProductID: "p", ID: "controller", Name: "一号消防控制器", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	send := func(id string, at int64, part string, fire bool) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"id": id, "timestamp": at, "data": map[string]any{"components": []model.ComponentStatus{{ID: part, Name: "烟感探测器", Location: "二楼走廊", Alarms: map[string]bool{"FIRE": fire, "DEVICE_FAULT": fire}}}}})
		raw, err := onboarding.StandardRaw("tenant", "p", "controller", "event", "HTTP", payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = engine.IngestRaw(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}
	send("fire-B", 1000, "loop-1/node-7", true)
	send("normal-A", 2000, "loop-1/node-8", false)
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant", Status: "ACTIVE", Limit: 100})
	if err != nil || len(alarms) != 2 {
		t.Fatal(alarms, err)
	}
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)
	detail := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/alarms/"+alarms[0].ID, token, nil, 200)
	if detail["componentId"] != "loop-1/node-7" || detail["componentLocation"] != "二楼走廊" {
		t.Fatal(detail)
	}
	other, _ := api.auth.Issue("other", "other-tenant", "operator", nil, time.Hour)
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/alarms/"+alarms[0].ID, other, nil, 404)
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("Chrome not configured")
		}
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "component-alarm-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("browser: %v %s", err, out)
		}
		t.Log(string(out))
	})
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/alarms/"+alarms[0].ID+"/actions", token, map[string]any{"action": "ACKED"}, 200)
	send("recover-B", 3000, "loop-1/node-7", false)
	saved, err := repo.GetAlarm(ctx, "tenant", alarms[0].ID)
	if err != nil || saved.Status != "RECOVERED" {
		t.Fatal(saved, err)
	}
}

func TestDashboard(t *testing.T) {
	forEachStore(t, checkDashboard)
}
func checkDashboard(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	location := time.FixedZone("test", 8*3600)
	now := time.Now().In(location)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -6).UnixMilli()
	for _, tenant := range []string{"tenant", "other"} {
		must(repo.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "p", Name: "烟感", Status: "ENABLED"}))
		for i, status := range []string{"ONLINE", "ALARM", "OFFLINE", "SUSPECTED_OFFLINE", ""} {
			id := fmt.Sprint(i)
			must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: id, ProductID: "p", Status: "ENABLED", AccessKey: tenant + id}))
			if status != "" {
				connection, dataStatus := "CONNECTED", "ACTIVE"
				if i >= 2 {
					connection, dataStatus = "DISCONNECTED", "SILENT"
				}
				must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: id, ProductID: "p", BusinessStatus: status, ConnectionStatus: connection, DataStatus: dataStatus}))
			}
		}
		must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: "unregistered", ProductID: "p", BusinessStatus: "ONLINE"}))
		for i := 0; i < 125; i++ {
			first := start
			level := "HIGH"
			status := "ACTIVE"
			if i == 0 {
				first = start - 1
			}
			if i == 1 {
				first = start + 86400000
			}
			if i == 2 {
				first = now.UnixMilli() + 86400000
			}
			if i == 3 {
				status = "ACKED"
			}
			if i == 4 {
				status = "RECOVERED"
			}
			if i == 5 {
				level = "CRITICAL"
			}
			alarmType := "FIRE"
			if i%2 == 1 {
				alarmType = "SMOKE_DETECTED"
			}
			_, _, err := repo.UpsertAlarm(ctx, model.Alarm{AlarmType: alarmType, TenantID: tenant, ID: fmt.Sprint(i), DeviceID: fmt.Sprint(i), RuleID: fmt.Sprint(i), AlarmLevel: level, Status: status, FirstTriggeredAt: first, LastTriggeredAt: now.UnixMilli(), TriggerCount: 99})
			must(err)
		}
	}
	scoped, err := repo.DashboardCountsForDevices(ctx, "tenant", start, now.UnixMilli(), []string{"0", "1"})
	must(err)
	counts := map[string]int{}
	for _, item := range scoped {
		counts[item.Kind] += item.Count
	}
	if counts["state"] != 2 || counts["product"] != 2 || counts["level"] != 2 || counts["day"] != 1 || counts["connection"] != 2 || counts["dataStatus"] != 2 || counts["alarmStatus"] != 1 || counts["alarmType"] != 1 {
		t.Fatalf("scoped dashboard counts: %+v", scoped)
	}
	emptyScope, err := repo.DashboardCountsForDevices(ctx, "tenant", start, now.UnixMilli(), []string{})
	must(err)
	if len(emptyScope) != 0 {
		t.Fatalf("empty device scope leaked counts: %+v", emptyScope)
	}
	productsByID, err := repo.GetProductsByIDs(ctx, "tenant", []string{"p", "missing"})
	must(err)
	statesByID, err := repo.GetDeviceStatesByIDs(ctx, "tenant", []string{"0", "missing"})
	must(err)
	if len(productsByID) != 1 || len(statesByID) != 1 || statesByID["0"].BusinessStatus != "ONLINE" {
		t.Fatalf("batch device lookups: products=%+v states=%+v", productsByID, statesByID)
	}
	must(repo.SaveStandardMessage(ctx, model.StandardMessage{TenantID: "tenant", MessageID: "batch-standard", RawMessageID: "batch-raw", DeviceID: "0", Parser: "batch-test"}))
	parsedByRaw, err := repo.GetStandardMessagesByRawIDs(ctx, "tenant", []string{"batch-raw", "missing"})
	must(err)
	if len(parsedByRaw) != 1 || parsedByRaw["batch-raw"].Parser != "batch-test" {
		t.Fatalf("batch parsed messages: %+v", parsedByRaw)
	}
	must(repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "tenant", CameraID: "batch-camera", CameraName: "批量测试摄像头", DeviceID: "0", Enabled: true}))
	camerasByDevice, err := repo.ListVideoCameraMappingsByDeviceIDs(ctx, "tenant", []string{"0", "missing"})
	must(err)
	if len(camerasByDevice) != 1 || len(camerasByDevice["0"]) != 1 || camerasByDevice["0"][0].CameraID != "batch-camera" {
		t.Fatalf("batch camera mappings: %+v", camerasByDevice)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "dashboard-test-secret-32-characters"
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(repo)}, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	result := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?days=7&offset=480", token, nil, 200)
	if result["devices"] != float64(5) || result["online"] != float64(2) || result["activeAlarms"] != float64(123) || result["highAlarms"] != float64(123) {
		t.Fatalf("wrong totals: %v", result)
	}
	states := result["states"].(map[string]any)
	if states["SUSPECTED_OFFLINE"] != float64(1) || states["NEVER_SEEN"] != float64(1) {
		t.Fatal(states)
	}
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
	trend := result["trend"].([]any)
	if len(trend) != 7 || trend[0].(map[string]any)["count"] != float64(122) || trend[1].(map[string]any)["count"] != float64(1) || trend[2].(map[string]any)["count"] != float64(0) {
		t.Fatal(trend)
	}
	products := result["products"].([]any)
	if len(products) != 1 || products[0].(map[string]any)["count"] != float64(5) {
		t.Fatal(products)
	}
	result = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?days=30&offset=-720", token, nil, 200)
	if len(result["trend"].([]any)) != 30 {
		t.Fatal(result)
	}
	emptyToken, _ := api.auth.Issue("viewer", "empty", "viewer", nil, time.Hour)
	result = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard", emptyToken, nil, 200)
	if result["devices"] != float64(0) || result["activeAlarms"] != float64(0) || len(result["products"].([]any)) != 0 {
		t.Fatal(result)
	}
	for field := range expectedGroups {
		if len(result[field].(map[string]any)) != 0 {
			t.Fatalf("empty tenant leaked %s: %v", field, result[field])
		}
	}
	for _, query := range []string{"days=10000", "days=abc", "offset=841", "offset=-721", "offset=abc"} {
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?"+query, token, nil, 400)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard", "", nil, 401)
}

type countingEventRepo struct {
	*memory.Repository
	alarmReads, stateReads atomic.Int32
}

func (r *countingEventRepo) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	r.alarmReads.Add(1)
	return r.Repository.ListAlarms(ctx, f)
}

func (r *countingEventRepo) ListDeviceStatesPage(ctx context.Context, tenant string, l, o int) ([]model.DeviceState, int, error) {
	r.stateReads.Add(1)
	return r.Repository.ListDeviceStatesPage(ctx, tenant, l, o)
}

// Concurrent polls of one tenant share a single read, and the snapshot is read
// again once it expires.
func TestEventSnapshotIsSharedPerTenantWindow(t *testing.T) {
	repo := &countingEventRepo{Repository: memory.NewRepository()}
	cache := newEventSnapshots()
	now := time.Now()
	cache.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := cache.get(context.Background(), repo, "t1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if repo.alarmReads.Load() != 1 || repo.stateReads.Load() != 1 {
		t.Fatalf("20 polls read alarms %d and states %d times, want once", repo.alarmReads.Load(), repo.stateReads.Load())
	}
	if _, _, err := cache.get(context.Background(), repo, "t2"); err != nil || repo.stateReads.Load() != 2 {
		t.Fatalf("tenants must not share snapshots: reads=%d err=%v", repo.stateReads.Load(), err)
	}
	now = now.Add(eventSnapshotTTL)
	if _, _, err := cache.get(context.Background(), repo, "t1"); err != nil || repo.stateReads.Load() != 3 {
		t.Fatalf("expired snapshot must be reloaded: reads=%d err=%v", repo.stateReads.Load(), err)
	}
}

// Users who see every device share one snapshot read per tenant window;
// only restricted views keep their own.
func TestAllScopeUsersShareEventSnapshot(t *testing.T) {
	repo := &countingEventRepo{Repository: memory.NewRepository()}
	engine := &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "events-share-password"
	cfg.AdminTenants = []string{"tenant-a"}
	cfg.JWTSecret = "user-events-share-secret-at-least-32-bytes"
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	root := requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant-a"}, 200)["accessToken"].(string)
	tokens := []string{root}
	for _, name := range []string{"watcher-a", "watcher-b"} {
		requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/access/users", root, map[string]any{"username": name, "password": "events-share-user", "enabled": true, "permissions": []string{"menu:alarms", "menu:devices"}, "deviceScope": "all"}, 200)
		tokens = append(tokens, requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/auth/login", "", map[string]any{"username": name, "password": "events-share-user", "tenantId": "tenant-a"}, 200)["accessToken"].(string))
	}
	before := repo.alarmReads.Load()
	for _, token := range tokens {
		requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/events", token, nil, 200)
	}
	if reads := repo.alarmReads.Load() - before; reads != 1 {
		t.Fatalf("three all-scope users read the tenant snapshot %d times, want once", reads)
	}
}

// An unchanged event view answers 304 to the ETag the page already holds.
func TestUserEventsAnswerNotModifiedForSameView(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-a", DeviceID: "device-a", BusinessStatus: "ONLINE"}); err != nil {
		t.Fatal(err)
	}
	engine := &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "events-root-password"
	cfg.AdminTenants = []string{"tenant-a"}
	cfg.JWTSecret = "user-events-etag-secret-at-least-32-bytes"
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	token := requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant-a"}, 200)["accessToken"].(string)

	poll := func(etag string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}
	first := poll("")
	tag := first.Header.Get("ETag")
	if first.StatusCode != 200 || tag == "" {
		t.Fatalf("first poll status=%d etag=%q", first.StatusCode, tag)
	}
	if resp := poll(tag); resp.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged view must answer 304, got %d", resp.StatusCode)
	}
	if resp := poll(`"stale"`); resp.StatusCode != 200 {
		t.Fatalf("a different ETag must receive the full view, got %d", resp.StatusCode)
	}
}

// A page holding the snapshot receives only rows changed since its cursor;
// an unknown cursor (another replica or an old view) gets the full snapshot.
func TestUserEventsReturnOnlyChangesSinceCursor(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	for _, id := range []string{"device-a", "device-b"} {
		if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-a", DeviceID: id, BusinessStatus: "ONLINE"}); err != nil {
			t.Fatal(err)
		}
	}
	engine := &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "events-root-password"
	cfg.AdminTenants = []string{"tenant-a"}
	cfg.JWTSecret = "user-events-delta-secret-at-least-32-bytes"
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	api.events.now = func() time.Time { return now }
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	token := requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant-a"}, 200)["accessToken"].(string)
	poll := func(since string) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/events?since="+url.QueryEscape(since), token, nil, 200)
	}
	full := poll("")
	if full["delta"] != false || len(full["devices"].([]any)) != 2 {
		t.Fatalf("first poll must be the full snapshot: %v", full)
	}
	cursor := full["cursor"].(string)
	if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-a", DeviceID: "device-b", BusinessStatus: "ALARM"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(eventSnapshotTTL)
	changed := poll(cursor)
	devices := changed["devices"].([]any)
	if changed["delta"] != true || len(devices) != 1 || devices[0].(map[string]any)["deviceId"] != "device-b" {
		t.Fatalf("delta must hold only the changed device: %v", changed)
	}
	if unchanged := poll(changed["cursor"].(string)); unchanged["delta"] != true || len(unchanged["devices"].([]any)) != 0 {
		t.Fatalf("no change must return an empty delta: %v", unchanged)
	}
	if foreign := poll("otherprocess.1.abc"); foreign["delta"] != false || len(foreign["devices"].([]any)) != 2 {
		t.Fatalf("an unknown cursor must return the full snapshot: %v", foreign)
	}
}

func TestRawFilterValidation(t *testing.T) {
	for _, query := range []string{"start=abc", "end=-1", "start=20&end=10", "parseStatus=ANY", "messageType=unknown"} {
		q, _ := url.ParseQuery(query)
		if _, err := parseRawFilter(q); err == nil {
			t.Fatalf("accepted invalid query %s", query)
		}
	}
	q, _ := url.ParseQuery("deviceId=+d+&parseStatus=parsed&messageType=alarm_report&start=10&end=20")
	f, err := parseRawFilter(q)
	if err != nil || f.DeviceID != "d" || f.ParseStatus != "PARSED" || f.MessageType != "ALARM_REPORT" || f.Start != 10 || f.End != 20 {
		t.Fatalf("bad normalized filter %+v %v", f, err)
	}
}

func TestRawFiltersHTTP(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	for _, row := range []model.RawArchiveIndex{
		{TenantID: "t", MessageID: "failed", DeviceID: "d", ProductID: "p", Protocol: "tcp", PayloadFormat: "hex", ReceivedAt: 2000, ParseError: "invalid"},
		{TenantID: "t", MessageID: "parsed", DeviceID: "d", ProductID: "p", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000},
		{TenantID: "other", MessageID: "foreign", DeviceID: "d", ProductID: "p", Protocol: "json", PayloadFormat: "json", ReceivedAt: 3000},
	} {
		if _, err := repo.SaveRawIndex(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveStandardMessage(ctx, model.StandardMessage{TenantID: "t", MessageID: "s", RawMessageID: "parsed", DeviceID: "d", MessageType: model.AlarmReport, Parser: "json_parser"}); err != nil {
		t.Fatal(err)
	}
	api := newTestAPI(t, repo, func(cfg *config.Config) {
		cfg.JWTSecret = "raw-filter-http-test"
		cfg.AdminTenants = []string{"t"}
	})
	srv := api.server
	token := api.adminToken(t, "t")
	for _, query := range []string{"deviceId=d&productId=p&messageId=parsed", "parseStatus=PARSED&messageType=ALARM_REPORT&parser=json_parser&protocol=JSON&payloadFormat=JSON&start=1000&end=1000"} {
		result := requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/raw-messages?"+query, token, nil, 200)
		items := result["items"].([]any)
		if result["total"].(float64) != 1 || len(items) != 1 || items[0].(map[string]any)["messageId"] != "parsed" || items[0].(map[string]any)["parsed"] != true {
			t.Fatalf("filtered response %+v", result)
		}
	}
	requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/raw-messages?start=no", token, nil, 400)

	// Without a time, device or message filter only the last 7 days are read.
	recent := time.Now().UnixMilli()
	if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", MessageID: "recent", DeviceID: "d", ProductID: "p", ReceivedAt: recent}); err != nil {
		t.Fatal(err)
	}
	result := requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/raw-messages", token, nil, 200)
	window, _ := result["window"].(map[string]any)
	if result["total"].(float64) != 1 || window["defaulted"] != true || result["totalCapped"] != false {
		t.Fatalf("unfiltered listing must default to the recent window: %+v", result)
	}
	if device := requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/raw-messages?deviceId=d", token, nil, 200); device["total"].(float64) != 3 || device["window"] != nil {
		t.Fatalf("a device listing keeps its full history: %+v", device)
	}
}

func TestDeleteResourceRoutesReturnConflictAndNotFound(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant_a", ID: "product"})
	_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: "device", ProductID: "product"})
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret = "root", "root-password-test", "delete-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.DevMode = true
	server := httptest.NewServer(New(cfg, &core.Engine{Repo: devicescope.Wrap(repo)}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	request := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	token := request("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	request("DELETE", "/api/v1/products/product", token, nil, 409)
	request("DELETE", "/api/v1/device-registry/device", token, nil, 200)
	request("DELETE", "/api/v1/products/product", token, nil, 200)
	request("DELETE", "/api/v1/products/product", token, nil, 404)
}

func TestDeleteProtocolReleaseOnlyRemovesSelectedVersionAndArtifacts(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	_ = repo.SaveProtocolDefinition(ctx, model.ProtocolDefinition{TenantID: "tenant_a", ID: "protocol", Name: "测试协议"})
	for _, version := range []string{"1.0.0", "2.0.0"} {
		if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant_a", ProtocolID: "protocol", Version: version}); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant_a", ProductID: "product", ProtocolID: "protocol", Version: "2.0.0", PreviousVersion: "1.0.0"})
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret = "root", "root-password-test", "delete-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.DevMode = true
	cfg.DataDir = t.TempDir()
	for _, version := range []string{"1.0.0", "2.0.0"} {
		path := filepath.Join(cfg.DataDir, "protocol-releases", "tenant_a", "protocol", version)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "package.zip"), []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(New(cfg, &core.Engine{Repo: devicescope.Wrap(repo)}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	token := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	path := server.URL + "/api/v2/protocols/protocol/releases/1.0.0"
	requestJSON(t, server.Client(), "DELETE", path, token, nil, 409)
	_ = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant_a", ProductID: "product", ProtocolID: "protocol", Version: "2.0.0"})
	requestJSON(t, server.Client(), "DELETE", path, token, nil, 200)
	requestJSON(t, server.Client(), "DELETE", path, token, nil, 404)
	if _, err := repo.GetProtocolDefinition(ctx, "tenant_a", "protocol"); err != nil {
		t.Fatalf("protocol family was deleted: %v", err)
	}
	if _, err := repo.GetProtocolRelease(ctx, "tenant_a", "protocol", "2.0.0"); err != nil {
		t.Fatalf("other version was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "protocol-releases", "tenant_a", "protocol", "1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("deleted version artifacts remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "protocol-releases", "tenant_a", "protocol", "2.0.0", "package.zip")); err != nil {
		t.Fatalf("other version artifacts were removed: %v", err)
	}
}

func TestDeleteKnowledgeDocumentClearsIndexObjectAndRecord(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	index := knowledge.NewLocal()
	if _, err = archive.PutObject(ctx, "iot-knowledge-docs", "tenant/doc.txt", bytes.NewBufferString("sample"), 6, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err = index.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: "tenant_a", DocumentID: "doc", ChunkID: "chunk", Content: []byte("sample")}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveKnowledgeDoc(ctx, model.KnowledgeDoc{TenantID: "tenant_a", ID: "doc", ObjectBucket: "iot-knowledge-docs", ObjectKey: "tenant/doc.txt"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret = "root", "root-password-test", "delete-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.DevMode = true
	server := httptest.NewServer(New(cfg, &core.Engine{Repo: devicescope.Wrap(repo), Archive: archive, KB: index}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	request := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	token := request("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	request("DELETE", "/api/v1/knowledge/documents/doc", token, nil, 200)
	if docs, err := repo.ListKnowledgeDocs(ctx, "tenant_a"); err != nil || len(docs) != 0 {
		t.Fatalf("metadata remains: %v %v", docs, err)
	}
	if chunks, err := index.ListKnowledgeChunks(ctx, "tenant_a", "doc"); err != nil || len(chunks) != 0 {
		t.Fatalf("index remains: %v %v", chunks, err)
	}
	if object, err := archive.GetObject(ctx, "iot-knowledge-docs", "tenant/doc.txt"); err == nil {
		object.Close()
		t.Fatal("object remains")
	}
}

func TestBackupEndpointsProxyRecordsFilesAndAdminActions(t *testing.T) {
	var calls []string
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal-backup-token" {
			http.Error(w, "missing internal authorization", http.StatusUnauthorized)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backups":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "backup_full_1", "type": "FULL", "status": "COMPLETED"}}, "total": 1, "limit": 50, "offset": 0})
		case "/backups/backup_full_1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_1", "type": "FULL", "status": "COMPLETED", "checksum": "abc"})
		case "/backups/backup_full_1/files":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_1", "type": "FULL", "artifacts": []map[string]any{{"filename": "manifest.json", "size": 12, "sha256": "abc"}}})
		case "/backups/backup_full_1/files/manifest.json":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="manifest.json"`)
			w.Header().Set("X-Checksum-SHA256", "abc")
			_, _ = io.WriteString(w, "manifest-body")
		case "/backup":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "backup_full_2", "type": r.URL.Query().Get("type")})
		case "/restore/drill":
			_ = json.NewEncoder(w).Encode(map[string]any{"drillId": "drill_1", "backupId": r.URL.Query().Get("backupId"), "status": "COMPLETED", "artifactsChecked": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backupServer.Close()

	cfg := config.Config{BackupURL: backupServer.URL, BackupToken: "internal-backup-token", JWTSecret: "test-backup-secret-at-least-32-characters", CORSAllowedOrigins: []string{}}
	engine := &core.Engine{Repo: devicescope.Wrap(memory.NewRepository())}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	adminToken, err := api.auth.IssueWithVersion("admin", "tenant_001", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant_001", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	list := backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups?type=FULL", adminToken, nil, http.StatusOK)
	if list["total"] != float64(1) {
		t.Fatalf("unexpected backup list: %#v", list)
	}
	detail := backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1", adminToken, nil, http.StatusOK)
	if detail["id"] != "backup_full_1" {
		t.Fatalf("unexpected backup detail: %#v", detail)
	}
	backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files", adminToken, nil, http.StatusOK)
	backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1", viewerToken, nil, http.StatusForbidden)
	backupJSONRequest(t, server.Client(), http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files", viewerToken, nil, http.StatusForbidden)
	viewerDownloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files/manifest.json", nil)
	viewerDownloadReq.Header.Set("Authorization", "Bearer "+viewerToken)
	viewerDownloadResp, err := server.Client().Do(viewerDownloadReq)
	if err != nil {
		t.Fatal(err)
	}
	viewerDownloadResp.Body.Close()
	if viewerDownloadResp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer download status=%d want=%d", viewerDownloadResp.StatusCode, http.StatusForbidden)
	}
	downloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/backups/backup_full_1/files/manifest.json", nil)
	downloadReq.Header.Set("Authorization", "Bearer "+adminToken)
	downloadResp, err := server.Client().Do(downloadReq)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(downloadResp.Body)
	downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK || string(body) != "manifest-body" || downloadResp.Header.Get("X-Checksum-SHA256") != "abc" {
		t.Fatalf("unexpected backup download status=%d headers=%v body=%q", downloadResp.StatusCode, downloadResp.Header, body)
	}

	run := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", adminToken, map[string]any{"type": "INCREMENTAL"}, http.StatusOK)
	if run["type"] != "INCREMENTAL" {
		t.Fatalf("unexpected manual backup response: %#v", run)
	}
	drill := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups/backup_full_1/restore-drill", adminToken, nil, http.StatusOK)
	if drill["backupId"] != "backup_full_1" {
		t.Fatalf("unexpected restore drill response: %#v", drill)
	}
	backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", viewerToken, map[string]any{"type": "FULL"}, http.StatusForbidden)

	joined := ""
	for _, call := range calls {
		joined += call + "\n"
	}
	if !bytes.Contains([]byte(joined), []byte("GET /backups?limit=20&offset=0&type=FULL")) || !bytes.Contains([]byte(joined), []byte("GET /backups/backup_full_1/files?limit=20&offset=0")) || !bytes.Contains([]byte(joined), []byte("POST /backup?type=INCREMENTAL")) || !bytes.Contains([]byte(joined), []byte("POST /restore/drill?backupId=backup_full_1")) {
		t.Fatalf("unexpected backup-service calls: %s", joined)
	}
}

func TestBackupPlatformTenantBoundary(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-backup-token" {
			http.Error(w, "missing service credential", http.StatusUnauthorized)
			return
		}
		write(w, 200, map[string]any{"tenantId": "other-tenant", "deviceId": "private-device"})
	}))
	defer upstream.Close()
	repo := memory.NewRepository()
	cfg := config.Config{AdminUser: "root", AdminPassword: "synthetic-root-password", AdminTenants: []string{"business", "ops"}, JWTSecret: "synthetic-backup-jwt-secret-32-characters", BackupURL: upstream.URL, BackupToken: "synthetic-backup-token"}
	cfg.Ops.Tenants = []string{"ops"}
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	routes := []struct{ method, path, permission string }{
		{"GET", "/api/v1/backups", ""},
		{"GET", "/api/v1/backups/global", ""},
		{"GET", "/api/v1/backups/global/files", ""},
		{"GET", "/api/v1/backups/global/files/raw.gz", "GET /api/v1/backups/:id/files/:filename"},
		{"POST", "/api/v1/backups", "POST /api/v1/backups"},
		{"POST", "/api/v1/backups/global/restore-drill", "POST /api/v1/backups/:id/restore-drill"},
		{"DELETE", "/api/v1/backups/global", "DELETE /api/v1/backups/:id"},
	}
	permissions := []string{"menu:devices", "menu:backups"}
	for _, route := range routes {
		if route.permission != "" {
			permissions = append(permissions, route.permission)
		}
	}
	// Seed old grants directly: changing policy must revoke existing tokens and
	// grants as well as prevent new assignments in business tenants.
	hash, err := hashPassword("synthetic-user-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"business", "ops"} {
		state := model.AccessState{Users: []model.PlatformUser{{Username: "backup-user", PasswordHash: hash, Enabled: true, DeviceScope: "all", Permissions: permissions}}}
		if ok, err := repo.SaveAccessState(context.Background(), tenant, state); !ok || err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	login := func(tenant string) map[string]any {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": "backup-user", "password": "synthetic-user-password", "tenantId": tenant}, 200)
	}
	biz := login("business")
	bizToken := biz["accessToken"].(string)
	for _, route := range routes {
		req(route.method, route.path, bizToken, map[string]any{"type": "FULL"}, 403)
	}
	if calls.Load() != 0 {
		t.Fatal("business tenant reached the global backup service")
	}
	for _, view := range []map[string]any{biz, req("GET", "/api/v1/auth/me", bizToken, nil, 200)} {
		for _, p := range view["permissions"].([]any) {
			if strings.Contains(p.(string), "backups") {
				t.Fatalf("business user was offered backup permission %v", p)
			}
		}
	}
	root, _ := api.auth.IssueWithVersion("root", "business", "admin", api.adminSessionVersion(), time.Hour)
	for _, item := range req("GET", "/api/v1/access/permissions", root, nil, 200)["items"].([]any) {
		if strings.Contains(item.(map[string]any)["id"].(string), "backups") {
			t.Fatal("business tenant can grant platform backup access")
		}
	}
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "backup-role", "name": "backup", "permissions": permissions, "deviceScope": "all"}, 422)
	opsToken := login("ops")["accessToken"].(string)
	for _, route := range routes {
		req(route.method, route.path, opsToken, map[string]any{"type": "FULL"}, 200)
	}
	// An ops tenant is necessary but not sufficient: retain action grants and
	// the existing all-device prerequisite for this global artifact.
	state, _ := repo.LoadAccessState(context.Background(), "ops")
	state.Users[0].Permissions = []string{"menu:devices", "menu:backups"}
	if ok, err := repo.SaveAccessState(context.Background(), "ops", state); !ok || err != nil {
		t.Fatal("update ops permissions", err)
	}
	req("GET", "/api/v1/backups", opsToken, nil, 200)
	req("GET", "/api/v1/backups/global/files/raw.gz", opsToken, nil, 403)
	req("DELETE", "/api/v1/backups/global", opsToken, nil, 403)
	state, _ = repo.LoadAccessState(context.Background(), "ops")
	state.Users[0].DeviceScope = "none"
	if ok, err := repo.SaveAccessState(context.Background(), "ops", state); !ok || err != nil {
		t.Fatal("update ops scope", err)
	}
	req("GET", "/api/v1/backups", opsToken, nil, 403)
	state, _ = repo.LoadAccessState(context.Background(), "ops")
	state.Users[0].DeviceScope = "all"
	if ok, err := repo.SaveAccessState(context.Background(), "ops", state); !ok || err != nil {
		t.Fatal("restore ops scope", err)
	}
	// Removal from the operations tenant allowlist takes effect on the next
	// request, without issuing a new login token.
	api.cfg.Ops.Tenants = nil
	req("GET", "/api/v1/backups", opsToken, nil, 403)
	req("GET", "/api/v1/backups", root, nil, 200)
}

func TestReplayRateValidationHTTP(t *testing.T) {
	cfg := config.Config{JWTSecret: "replay-test-secret-at-least-32-characters"}
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(memory.NewRepository()), Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	token, err := api.auth.IssueWithVersion("admin", "t", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/raw-messages/replay", token, map[string]any{"start": 1, "end": 2, "mode": "REINGEST", "ratePerSecond": 10001}, 422)
}

func TestBackupEndpointSurfacesUpstreamFailureDetail(t *testing.T) {
	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal-backup-token" {
			http.Error(w, "missing internal authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "postgres: pg_dump exited with status 1"})
	}))
	defer backupServer.Close()

	cfg := config.Config{BackupURL: backupServer.URL, BackupToken: "internal-backup-token", JWTSecret: "test-backup-secret-at-least-32-characters", CORSAllowedOrigins: []string{}}
	engine := &core.Engine{Repo: devicescope.Wrap(memory.NewRepository())}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	adminToken, err := api.auth.IssueWithVersion("admin", "tenant_001", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	result := backupJSONRequest(t, server.Client(), http.MethodPost, server.URL+"/api/v1/backups", adminToken, map[string]any{"type": "FULL"}, http.StatusBadGateway)
	detail, _ := result["detail"].(string)
	if !bytes.Contains([]byte(detail), []byte("pg_dump exited with status 1")) {
		t.Fatalf("backup upstream detail was lost: %q", detail)
	}
}

func backupJSONRequest(t *testing.T, client *http.Client, method, endpoint, token string, body any, wantStatus int) map[string]any {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode %s: %v", endpoint, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s status=%d want=%d body=%#v", endpoint, response.StatusCode, wantStatus, result)
	}
	return result
}

func TestEventSnapshotBoundsPopulationAndDetails(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	for i := 0; i < 600; i++ {
		id := fmt.Sprint(i)
		_ = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: id, LastSeenAt: int64(i)})
		_, _, _ = repo.UpsertAlarm(ctx, model.Alarm{ID: id, TenantID: "t", DeviceID: id, RuleID: id, Status: "ACTIVE", Details: map[string]any{"raw": strings.Repeat("x", 1024)}})
	}
	alarms, states, total, err := loadEventSnapshot(ctx, repo, "t")
	body, _ := json.Marshal(map[string]any{"alarms": alarms, "states": states})
	if err != nil || total != 600 || len(alarms) != eventSnapshotLimit || len(states) != eventSnapshotLimit || len(body) > 256<<10 {
		t.Fatalf("unbounded snapshot alarms=%d states=%d bytes=%d err=%v", len(alarms), len(states), len(body), err)
	}
}

func TestDashboardCacheSharesOnlyTheSameAuthorizedView(t *testing.T) {
	var cache dashboardCache
	var calls atomic.Int32
	load := func(context.Context) ([]model.DashboardCount, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return []model.DashboardCount{{Kind: "state", Key: "ONLINE", Count: 1}}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.get(context.Background(), "tenant/user/version1", load); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("identical view not coalesced", calls.Load())
	}
	for _, key := range []string{"tenant/other/version1", "tenant/user/version2", "other/user/version1"} {
		if _, err := cache.get(context.Background(), key, load); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal("users, tenants or permission versions shared data", calls.Load())
	}
}
