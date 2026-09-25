package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	aiadapter "iot-platform/internal/adapters/ai" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/knowledge"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"        /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"                  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"                /* 执行当前语句并推进处理流程。 */

	"github.com/gin-gonic/gin" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestHTTPWorkflow(t *testing.T) { /* 定义 TestHTTPWorkflow 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}, parser.JavaScriptParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	installEndpointWorkflows(engine)
	engine.AIPlugins = aiadapter.NewProviderRegistry() /* 更新 engine.AIPlugins 的值。 */
	engine.KB = knowledge.NewLocal()                   /* 更新 engine.KB 的值。 */
	engine.Metrics = metrics.New()                     /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(ctx); err != nil {           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                        /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                                          /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                        /* 更新 cfg.JWTSecret 的值。 */
	cfg.CORSAllowedOrigins = []string{"http://localhost:5173"}                                                  /* 更新 cfg.CORSAllowedOrigins 的值。 */
	cfg.AdminTenants = []string{"tenant_001", "tenant_002", "tenant_video_other"}                               /* 更新 cfg.AdminTenants 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 api 的值。 */
	if _, ok := api.Handler().(*gin.Engine); !ok {                                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("HTTP server is not backed by Gin: %T", api.Handler()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	api.router.GET("/__panic_test", func(c *gin.Context) { panic("test panic") }) /* 执行当前语句并推进处理流程。 */
	server := httptest.NewServer(api.Handler())                                   /* 更新 server 的值。 */
	defer server.Close()                                                          /* 安排函数结束时执行清理。 */
	healthResp, err := server.Client().Get(server.URL + "/health/live")           /* 更新 err 的值。 */
	if err != nil {                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	healthResp.Body.Close()                                                                                     /* 执行当前语句并推进处理流程。 */
	if healthResp.StatusCode != http.StatusOK || healthResp.Header.Get("X-Content-Type-Options") != "nosniff" { /* 判断条件并选择处理分支。 */
		t.Fatalf("Gin security middleware failed: status=%d headers=%v", healthResp.StatusCode, healthResp.Header) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	preflight, _ := http.NewRequest(http.MethodOptions, server.URL+"/api/v1/products", nil) /* 更新 _ 的值。 */
	preflight.Header.Set("Origin", "http://localhost:5173")                                 /* 执行当前语句并推进处理流程。 */
	preflightResp, err := server.Client().Do(preflight)                                     /* 更新 err 的值。 */
	if err != nil {                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	preflightResp.Body.Close()                                                                                                                  /* 执行当前语句并推进处理流程。 */
	if preflightResp.StatusCode != http.StatusNoContent || preflightResp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" { /* 判断条件并选择处理分支。 */
		t.Fatalf("CORS preflight failed: status=%d headers=%v", preflightResp.StatusCode, preflightResp.Header) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	methodReq, _ := http.NewRequest(http.MethodPost, server.URL+"/health/live", nil) /* 更新 _ 的值。 */
	methodResp, err := server.Client().Do(methodReq)                                 /* 更新 err 的值。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	methodResp.Body.Close()                                   /* 执行当前语句并推进处理流程。 */
	if methodResp.StatusCode != http.StatusMethodNotAllowed { /* 判断条件并选择处理分支。 */
		t.Fatalf("Gin method handling status=%d want=%d", methodResp.StatusCode, http.StatusMethodNotAllowed) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	panicResp, err := server.Client().Get(server.URL + "/__panic_test") /* 更新 err 的值。 */
	if err != nil {                                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	panicResp.Body.Close()                                      /* 执行当前语句并推进处理流程。 */
	if panicResp.StatusCode != http.StatusInternalServerError { /* 判断条件并选择处理分支。 */
		t.Fatalf("Gin recovery status=%d want=%d", panicResp.StatusCode, http.StatusInternalServerError) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/products", "", nil, http.StatusUnauthorized)                                                                           /* 执行当前语句并推进处理流程。 */
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, 200) /* 更新 login 的值。 */
	token := login["accessToken"].(string)                                                                                                                                                     /* 更新 token 的值。 */
	providers := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/providers", token, nil, 200)                                                                           /* 更新 providers 的值。 */
	if providers["mode"] != "plugin-harness" || len(providers["items"].([]any)) < 4 {                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected AI provider registry %#v", providers) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	viewerToken, err := api.auth.Issue("viewer", "tenant_001", "viewer", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	viewerProviders := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/providers", viewerToken, nil, 200) /* 更新 viewerProviders 的值。 */
	for _, item := range viewerProviders["items"].([]any) {                                                                      /* 循环处理当前数据。 */
		if _, exposed := item.(map[string]any)["defaultBaseUrl"]; exposed { /* 判断条件并选择处理分支。 */
			t.Fatalf("viewer received provider endpoint %#v", item) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 providerServer 的值。 */
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "AI 插件测试成功"}}}}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer providerServer.Close()                                                                                                                                                                                                                    /* 安排函数结束时执行清理。 */
	providerTest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{"provider": "openai-compatible", "baseUrl": providerServer.URL, "model": "test-model", "question": "连接测试"}, 200) /* 更新 providerTest 的值。 */
	if providerTest["success"] != true || providerTest["answer"] != "AI 插件测试成功" || !strings.HasPrefix(providerTest["traceId"].(string), "ai_trace_") {                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected AI provider test %#v", providerTest) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 ollamaServer 的值。 */
		if r.URL.Path != "/api/chat" { /* 判断条件并选择处理分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
			return              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": "Ollama 沙箱地址生效"}}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer ollamaServer.Close()                                                                                                                                                                          /* 安排函数结束时执行清理。 */
	api.cfg.AITestOllamaURL = ollamaServer.URL                                                                                                                                                          /* 更新 api.cfg.AITestOllamaURL 的值。 */
	ollamaTest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{"provider": "ollama", "model": "test-model", "question": "连接测试"}, 200) /* 更新 ollamaTest 的值。 */
	if ollamaTest["success"] != true || ollamaTest["answer"] != "Ollama 沙箱地址生效" {                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected Ollama provider test %#v", ollamaTest) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras", token, map[string]any{"cameraId": "camera_metadata", "cameraName": "园区入口", "brand": "海康", "cameraPoint": "东门", "building": "A", "floor": "1", "room": "大厅", "enabled": true}, 201) /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/cameras/camera_metadata/preview", token, map[string]any{}, http.StatusNotFound)                                                                                                              /* 执行当前语句并推进处理流程。 */
	viewerCameras := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/integrations/video/cameras", viewerToken, nil, 200)                                                                                                                                             /* 更新 viewerCameras 的值。 */
	viewerCamera := viewerCameras["items"].([]any)[0].(map[string]any)                                                                                                                                                                                                                   /* 更新 viewerCamera 的值。 */
	if _, exposed := viewerCamera["streamUrl"]; exposed || viewerCamera["cameraName"] != "园区入口" || viewerCamera["brand"] != "海康" {                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("viewer camera metadata is incomplete or contains stream data %#v", viewerCamera) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages", token, map[string]any{"id": "protocol_json", "name": "JSON 通用协议", "version": "1.0.0", "protocol": "json", "transport": "HTTP", "payloadFormat": "json", "parserType": "custom_json_parser", "status": "PUBLISHED"}, 201) /* 执行当前语句并推进处理流程。 */
	protocolTest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages/protocol_json/test", token, map[string]any{"payload": map[string]any{"properties": map[string]any{"temperature": 22.5}}}, 200)                                                                             /* 更新 protocolTest 的值。 */
	if protocolTest["success"] != true {                                                                                                                                                                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("protocol test failed: %#v", protocolTest) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages", token, map[string]any{"id": "protocol_javascript", "name": "JavaScript 解析协议", "version": "1.0.0", "protocol": "javascript", "transport": "HTTP", "payloadFormat": "hex", "parserType": parser.JavaScriptParserName, "status": "PUBLISHED", "config": map[string]any{"source": "function parse(raw) { const b = hexToBytes(raw.payload); return {properties: {temperature: b[0] / 10}} }"}}, 422) /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/products", token, map[string]any{"id": "product_json", "name": "JSON 传感器", "category": "sensor", "protocolPackageId": "protocol_json", "status": "ENABLED"}, 201)                                                                                                                                                                                                                                                    /* 执行当前语句并推进处理流程。 */
	managed := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry", token, map[string]any{"id": "device_managed", "name": "受管测试设备", "productId": "product_json", "status": "ENABLED"}, 201)                                                                                                                                                                                                                                                                 /* 更新 managed 的值。 */
	credential := managed["credential"].(map[string]any)                                                                                                                                                                                                                                                                                                                                                                                                                                      /* 更新 credential 的值。 */
	publicBody, _ := json.Marshal(map[string]any{"messageId": "raw_managed", "payload": map[string]any{"properties": map[string]any{"temperature": 22.5}}})                                                                                                                                                                                                                                                                                                                                   /* 更新 _ 的值。 */
	publicReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/device-ingest/device_managed", bytes.NewReader(publicBody))                                                                                                                                                                                                                                                                                                                                                          /* 更新 _ 的值。 */
	publicReq.Header.Set("Content-Type", "application/json")                                                                                                                                                                                                                                                                                                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	publicReq.Header.Set("X-Device-Key", credential["accessKey"].(string))                                                                                                                                                                                                                                                                                                                                                                                                                    /* 执行当前语句并推进处理流程。 */
	publicReq.Header.Set("X-Device-Secret", credential["secret"].(string))                                                                                                                                                                                                                                                                                                                                                                                                                    /* 执行当前语句并推进处理流程。 */
	publicResp, err := server.Client().Do(publicReq)                                                                                                                                                                                                                                                                                                                                                                                                                                          /* 更新 err 的值。 */
	if err != nil || publicResp.StatusCode != 201 {                                                                                                                                                                                                                                                                                                                                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatalf("managed ingest status=%v err=%v", publicResp, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	publicResp.Body.Close()                                                                                            /* 执行当前语句并推进处理流程。 */
	registry := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/device-registry", token, nil, 200) /* 更新 registry 的值。 */
	if registry["count"].(float64) != 1 {                                                                              /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected registry: %#v", registry) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/rules", token, map[string]any{"id": "rule_e2e", "name": "高温烟雾", "alarmType": "FIRE_RISK", "level": "HIGH", "match": "all", "enabled": true, "conditions": []map[string]any{{"field": "temperature", "operator": ">", "value": 80}, {"field": "smoke", "operator": "eq", "value": true}}}, 201)                                                                                                                                                              /* 执行当前语句并推进处理流程。 */
	now := time.Now().UnixMilli()                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    /* 更新 now 的值。 */
	ingest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/raw-messages", token, map[string]any{"messageId": "raw_http_e2e", "tenantId": "tenant_001", "productId": "json_smoke", "deviceId": "device_http_e2e", "protocol": "json", "payloadFormat": "json", "receivedAt": now, "payload": map[string]any{"properties": map[string]any{"temperature": 88.5, "smoke": true}, "tags": map[string]any{"cityCode": "city_001", "districtCode": "district_01", "buildingId": "A", "deviceType": "smoke"}}}, 201) /* 更新 ingest 的值。 */
	if ingest["created"] != true {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("raw not created: %#v", ingest) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	alarms := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?status=ACTIVE", token, nil, 200) /* 更新 alarms 的值。 */
	if alarms["count"].(float64) != 1 {                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected alarms %#v", alarms) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	items := alarms["items"].([]any)                                                                                              /* 更新 items 的值。 */
	alarmID := items[0].(map[string]any)["alarmId"].(string)                                                                      /* 更新 alarmID 的值。 */
	rawDetail := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/raw-messages/raw_http_e2e", token, nil, 200) /* 更新 rawDetail 的值。 */
	if rawDetail["message"].(map[string]any)["messageId"] != "raw_http_e2e" {                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected raw detail %#v", rawDetail) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if rawDetail["parseStatus"] != "PARSED" || rawDetail["standardMessage"].(map[string]any)["messageType"] != "PROPERTY_REPORT" { /* 判断条件并选择处理分支。 */
		t.Fatalf("raw detail did not expose parsed standard message %#v", rawDetail) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	downloadReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/raw-messages/raw_http_e2e/download", nil) /* 更新 _ 的值。 */
	downloadReq.Header.Set("Authorization", "Bearer "+token)                                                        /* 执行当前语句并推进处理流程。 */
	downloadResp, err := server.Client().Do(downloadReq)                                                            /* 更新 err 的值。 */
	if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	downloadBody, _ := io.ReadAll(downloadResp.Body)                                                                                                                                                      /* 更新 _ 的值。 */
	downloadResp.Body.Close()                                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	if downloadResp.StatusCode != 200 || !strings.Contains(downloadResp.Header.Get("Content-Disposition"), "raw_http_e2e.json") || !bytes.Contains(downloadBody, []byte(`"messageId": "raw_http_e2e"`)) { /* 判断条件并选择处理分支。 */
		t.Fatalf("raw download status=%d disposition=%q body=%s", downloadResp.StatusCode, downloadResp.Header.Get("Content-Disposition"), downloadBody) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	batchRequestBody, _ := json.Marshal(map[string]any{"messageIds": []string{"raw_managed", "raw_http_e2e"}})                     /* 更新 _ 的值。 */
	batchReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/raw-messages/download", bytes.NewReader(batchRequestBody)) /* 更新 _ 的值。 */
	batchReq.Header.Set("Authorization", "Bearer "+token)                                                                          /* 执行当前语句并推进处理流程。 */
	batchReq.Header.Set("Content-Type", "application/json")                                                                        /* 执行当前语句并推进处理流程。 */
	batchResp, err := server.Client().Do(batchReq)                                                                                 /* 更新 err 的值。 */
	if err != nil {                                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	batchBody, _ := io.ReadAll(batchResp.Body)                                                                                                      /* 更新 _ 的值。 */
	batchResp.Body.Close()                                                                                                                          /* 执行当前语句并推进处理流程。 */
	if batchResp.StatusCode != 200 || batchResp.Header.Get("Content-Type") != "application/zip" || batchResp.Header.Get("X-Archive-Count") != "2" { /* 判断条件并选择处理分支。 */
		t.Fatalf("batch raw download status=%d type=%q count=%q body=%s", batchResp.StatusCode, batchResp.Header.Get("Content-Type"), batchResp.Header.Get("X-Archive-Count"), batchBody) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	zr, err := zip.NewReader(bytes.NewReader(batchBody), int64(len(batchBody))) /* 更新 err 的值。 */
	if err != nil {                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	zipNames := map[string]bool{}  /* 更新 zipNames 的值。 */
	for _, file := range zr.File { /* 循环处理当前数据。 */
		zipNames[file.Name] = true /* 更新 zipNames[file.Name] 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !zipNames["清单.json"] || !zipNames["报文/001_raw_managed.json"] || !zipNames["报文/002_raw_http_e2e.json"] { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected ZIP entries %#v", zipNames) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	updatedRule := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/rules/rule_e2e", token, map[string]any{"name": "更新后的高温规则", "alarmType": "FIRE_RISK", "level": "CRITICAL", "match": "all", "enabled": true, "conditions": []map[string]any{{"field": "temperature", "operator": ">", "value": 90}}}, 200) /* 更新 updatedRule 的值。 */
	if updatedRule["id"] != "rule_e2e" || updatedRule["name"] != "更新后的高温规则" {                                                                                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("rule not updated %#v", updatedRule) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/rules/rule_e2e", token, nil, 200) /* 执行当前语句并推进处理流程。 */
	rules := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/rules", token, nil, 200)    /* 更新 rules 的值。 */
	if len(rules["items"].([]any)) != 0 {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("rule not deleted %#v", rules) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/alarm-analysis/"+alarmID, token, nil, 200)                                                                               /* 执行当前语句并推进处理流程。 */
	otherLogin := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_002"}, 200) /* 更新 otherLogin 的值。 */
	otherToken := otherLogin["accessToken"].(string)                                                                                                                                                /* 更新 otherToken 的值。 */
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/alarm-analysis/"+alarmID, otherToken, nil, http.StatusNotFound)                                                          /* 执行当前语句并推进处理流程。 */
	devices := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices", token, nil, 200)                                                                                       /* 更新 devices 的值。 */
	if devices["online"].(float64) != 2 {                                                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected devices %#v", devices) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	video := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/integrations/video/alarm", "", map[string]any{"eventId": "video_http_e2e", "source": "video", "tenantId": "tenant_001", "cameraId": "camera_1", "alarmType": "FLAME_DETECTED", "alarmLevel": "HIGH", "confidence": 0.92, "eventTime": now, "cityCode": "city_001", "districtCode": "district_01", "buildingId": "A"}, 201) /* 更新 video 的值。 */
	if video["created"] != true {                                                                                                                                                                                                                                                                                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("video not created %#v", video) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	replay := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/raw-messages/replay", token, map[string]any{"start": now - 1000, "end": now + 1000, "mode": "DRY_RUN", "ratePerSecond": 1000}, 202) /* 更新 replay 的值。 */
	replayID := replay["id"].(string)                                                                                                                                                                                  /* 更新 replayID 的值。 */
	deadline := time.Now().Add(2 * time.Second)                                                                                                                                                                        /* 更新 deadline 的值。 */
	for {                                                                                                                                                                                                              /* 循环处理当前数据。 */
		state := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/replays/"+replayID, token, nil, 200) /* 更新 state 的值。 */
		if state["status"] == "COMPLETED" {                                                                               /* 判断条件并选择处理分支。 */
			if state["processed"].(float64) != 2 { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected replay %#v", state) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if time.Now().After(deadline) { /* 判断条件并选择处理分支。 */
			t.Fatalf("replay timeout %#v", state) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/raw-messages", token, map[string]any{"messageId": "raw_discovered_e2e", "tenantId": "tenant_001", "productId": "product_json", "deviceId": "discovered_1", "protocol": "json", "payloadFormat": "json", "payload": map[string]any{"properties": map[string]any{"temperature": 23.5}}}, 201) /* 执行当前语句并推进处理流程。 */
	discovered := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/discovered-devices/discovered_1/register", token, map[string]any{}, 201)                                                                                                                                                                                                      /* 更新 discovered 的值。 */
	if discovered["device"].(map[string]any)["deviceRole"] != "DIRECT" {                                                                                                                                                                                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected discovered registration %#v", discovered) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	gateway := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry", token, map[string]any{"id": "gateway_1", "name": "一号网关", "productId": "product_json", "status": "ENABLED", "deviceRole": "GATEWAY"}, 201) /* 更新 gateway 的值。 */
	gatewayCredential := gateway["credential"].(map[string]any)                                                                                                                                                                                 /* 更新 gatewayCredential 的值。 */
	childBody, _ := json.Marshal(map[string]any{"messageId": "raw_gateway_child_e2e", "deviceId": "child_1", "deviceName": "一号子设备", "productId": "product_json", "payload": map[string]any{"properties": map[string]any{"temperature": 24.5}}}) /* 更新 _ 的值。 */
	childReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/device-ingest/gateway_1", bytes.NewReader(childBody))                                                                                                                   /* 更新 _ 的值。 */
	childReq.Header.Set("Content-Type", "application/json")                                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
	childReq.Header.Set("X-Device-Key", gatewayCredential["accessKey"].(string))                                                                                                                                                                /* 执行当前语句并推进处理流程。 */
	childReq.Header.Set("X-Device-Secret", gatewayCredential["secret"].(string))                                                                                                                                                                /* 执行当前语句并推进处理流程。 */
	childResp, err := server.Client().Do(childReq)                                                                                                                                                                                              /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	childResp.Body.Close()           /* 执行当前语句并推进处理流程。 */
	if childResp.StatusCode != 201 { /* 判断条件并选择处理分支。 */
		t.Fatalf("gateway child ingest status=%d", childResp.StatusCode) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	registryAfterGateway := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/device-registry", token, nil, 200) /* 更新 registryAfterGateway 的值。 */
	foundChild, gatewayChildCount := false, float64(0)                                                                             /* 更新 gatewayChildCount 的值。 */
	for _, item := range registryAfterGateway["items"].([]any) {                                                                   /* 循环处理当前数据。 */
		row := item.(map[string]any)             /* 更新 row 的值。 */
		device := row["device"].(map[string]any) /* 更新 device 的值。 */
		if device["id"] == "child_1" {           /* 判断条件并选择处理分支。 */
			foundChild = device["deviceRole"] == "CHILD" && device["gatewayId"] == "gateway_1" && device["autoRegistered"] == true /* 更新 foundChild 的值。 */
		} /* 结束当前表达式或代码块。 */
		if device["id"] == "gateway_1" { /* 判断条件并选择处理分支。 */
			gatewayChildCount = row["childCount"].(float64) /* 更新 gatewayChildCount 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !foundChild || gatewayChildCount != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("gateway relation missing %#v", registryAfterGateway) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	testFixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated) /* 更新 testFixture 的值。 */
	testDevice := testFixture["device"].(map[string]any)                                                                                                      /* 更新 testDevice 的值。 */
	testDeviceID := testDevice["id"].(string)                                                                                                                 /* 更新 testDeviceID 的值。 */
	testProduct := testFixture["product"].(map[string]any)                                                                                                    /* 更新 testProduct 的值。 */
	if testProduct["status"] != "ENABLED" || testFixture["protocolPackage"].(map[string]any)["status"] != "PUBLISHED" {                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("test fixture is not ready %#v", testFixture) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	testRules := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/rules", token, nil, http.StatusOK) /* 更新 testRules 的值。 */
	if len(testRules["items"].([]any)) != 0 {                                                                           /* 判断条件并选择处理分支。 */
		t.Fatalf("test fixture unexpectedly created an alarm rule %#v", testRules) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	fixtureAgain := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusOK) /* 更新 fixtureAgain 的值。 */
	if fixtureAgain["device"].(map[string]any)["id"] != testDeviceID {                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("test fixture was not repeatable %#v", fixtureAgain) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/rules", token, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"id":        "rule_test_device_navigation", /* 执行当前语句并推进处理流程。 */
		"name":      "测试设备报警后打开设备管理",               /* 执行当前语句并推进处理流程。 */
		"productId": testProduct["id"],             /* 执行当前语句并推进处理流程。 */
		"alarmType": "FIRE_RISK",                   /* 执行当前语句并推进处理流程。 */
		"level":     "HIGH",                        /* 执行当前语句并推进处理流程。 */
		"match":     "all",                         /* 执行当前语句并推进处理流程。 */
		"enabled":   true,                          /* 执行当前语句并推进处理流程。 */
		"conditions": []map[string]any{ /* 执行当前语句并推进处理流程。 */
			{"field": "temperature", "operator": ">", "value": 80}, /* 执行当前语句并推进处理流程。 */
			{"field": "smoke", "operator": "eq", "value": true},    /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		"actions": []map[string]any{{"type": "OPEN_PAGE", "page": "devices"}}, /* 执行当前语句并推进处理流程。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */
	testIngest := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+testDeviceID+"/debug", token, map[string]any{ /* 更新 testIngest 的值。 */
		"messageId": "raw_test_device_alarm_e2e", /* 执行当前语句并推进处理流程。 */
		"payload": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"alarm":      true,                                                                                                               /* 执行当前语句并推进处理流程。 */
			"properties": map[string]any{"temperature": 88.5, "smoke": true, "battery": 92},                                                  /* 执行当前语句并推进处理流程。 */
			"tags":       map[string]any{"cityCode": "city_001", "districtCode": "district_01", "buildingId": "A-01", "deviceType": "smoke"}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	}, http.StatusCreated) /* 结束当前表达式或代码块。 */
	if testIngest["created"] != true { /* 判断条件并选择处理分支。 */
		t.Fatalf("test fixture alarm raw was not created %#v", testIngest) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	testRaw := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/raw-messages/raw_test_device_alarm_e2e", token, nil, http.StatusOK) /* 更新 testRaw 的值。 */
	if testRaw["parseStatus"] != "PARSED" || testRaw["standardMessage"].(map[string]any)["messageType"] != "ALARM_REPORT" {                            /* 判断条件并选择处理分支。 */
		t.Fatalf("test fixture alarm was not parsed %#v", testRaw) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	testAlarms := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+testDeviceID+"&status=ACTIVE", token, nil, http.StatusOK) /* 更新 testAlarms 的值。 */
	if testAlarms["count"].(float64) < 1 {                                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatalf("test fixture alarm rule did not trigger %#v", testAlarms) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := server.Client().Get(server.URL + "/") /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	resp.Body.Close()                           /* 执行当前语句并推进处理流程。 */
	if resp.StatusCode != http.StatusNotFound { /* 判断条件并选择处理分支。 */
		t.Fatalf("backend root status=%d, want=%d", resp.StatusCode, http.StatusNotFound) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func requestJSON(t *testing.T, client *http.Client, method, url, token string, body any, want int) map[string]any { /* 定义 requestJSON 函数。 */
	t.Helper()           /* 执行当前语句并推进处理流程。 */
	var reader io.Reader /* 声明 reader。 */
	if body != nil {     /* 判断条件并选择处理分支。 */
		b, _ := json.Marshal(body)  /* 更新 _ 的值。 */
		reader = bytes.NewReader(b) /* 更新 reader 的值。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequest(method, url, reader) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if body != nil { /* 判断条件并选择处理分支。 */
		req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if token != "" { /* 判断条件并选择处理分支。 */
		req.Header.Set("Authorization", "Bearer "+token) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := client.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                        /* 安排函数结束时执行清理。 */
	var out map[string]any                                         /* 声明 out。 */
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("decode %s: %v", url, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode != want { /* 判断条件并选择处理分支。 */
		t.Fatalf("%s %s status=%d want=%d body=%#v", method, url, resp.StatusCode, want, out) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
