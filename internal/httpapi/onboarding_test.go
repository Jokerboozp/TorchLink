package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
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

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestStandardOnboardingHTTPChain(t *testing.T) { /* 定义 TestStandardOnboardingHTTPChain 函数。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                          /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir())           /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                                  /* 更新 log 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log) /* 更新 engine 的值。 */
	if err = engine.Start(ctx); err != nil {                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                               /* 更新 cfg 的值。 */
	cfg.JWTSecret = "onboarding-test-signing-key-32-characters"                                                                                                        /* 更新 cfg.JWTSecret 的值。 */
	cfg.AdminTenants = []string{"tenant"}                                                                                                                              /* 更新 cfg.AdminTenants 的值。 */
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"                                                                                                           /* 更新 cfg.DeviceHTTPPublicURL 的值。 */
	cfg.MQTTPublicURL = "mqtts://devices.example.test:8883"                                                                                                            /* 更新 cfg.MQTTPublicURL 的值。 */
	srv := New(cfg, engine, metrics.New(), log)                                                                                                                        /* 更新 srv 的值。 */
	token, _ := srv.auth.Issue("tester", "tenant", "admin", nil, time.Hour)                                                                                            /* 更新 _ 的值。 */
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"}) /* 更新 _ 的值。 */
	payload := json.RawMessage(`{"id":"a","timestamp":1788850000000,"data":{"temperature":26.5}}`)                                                                     /* 更新 payload 的值。 */
	q := onboarding.EnrollRequest{RequestID: "req-1", ProductID: "product", Device: onboarding.EnrollDevice{ID: "device", Name: "传感器"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}}
	call := func(method, path string, body []byte, credential model.DeviceCredential, admin bool) *httptest.ResponseRecorder { /* 更新 call 的值。 */
		r := httptest.NewRequest(method, path, bytes.NewReader(body)) /* 更新 r 的值。 */
		r.Header.Set("Content-Type", "application/json")              /* 执行当前语句并推进处理流程。 */
		r.Header.Set("X-Device-Key", credential.AccessKey)            /* 执行当前语句并推进处理流程。 */
		r.Header.Set("X-Device-Secret", credential.Secret)            /* 执行当前语句并推进处理流程。 */
		if admin {                                                    /* 判断条件并选择处理分支。 */
			r.Header.Set("Authorization", "Bearer "+token) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		w := httptest.NewRecorder()   /* 更新 w 的值。 */
		srv.Handler().ServeHTTP(w, r) /* 执行当前语句并推进处理流程。 */
		return w                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	w := call("GET", "/api/v1/onboarding/preflight?productId=product", nil, model.DeviceCredential{}, true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"standard"`) || !strings.Contains(w.Body.String(), `"ready":true`) {
		t.Fatal("preflight", w.Code, w.Body.String())
	}
	data, _ := json.Marshal(q)
	w = call("POST", "/api/v1/onboarding", data, model.DeviceCredential{}, true) /* 更新 w 的值。 */
	if w.Code != 201 {                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var created struct {
		onboarding.EnrollResult
		AccessInfo map[string]any `json:"accessInfo"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created) /* 更新 _ 的值。 */
	if created.Credential.Secret == "" || created.AccessInfo["httpUrl"] != "https://devices.example.test/api/v1/device-ingest/standard/tenant/product/device/property" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("device access information", w.Body.String())
	}
	if guide := call("GET", "/api/v1/device-registry/device/connection-guide", nil, model.DeviceCredential{}, true); guide.Code != 404 { /* 判断条件并选择处理分支。 */
		t.Fatalf("removed connection guide endpoint: %d", guide.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	connection := call("GET", "/api/v1/device-registry/device/connection", nil, model.DeviceCredential{}, true)                               /* 更新 connection 的值。 */
	if connection.Code != 200 || !strings.Contains(connection.Body.String(), "WAITING_FOR_DATA") || diagnosisStage(connection) != "WAITING" { /* 判断条件并选择处理分支。 */
		t.Fatal("configuration falsely reported receipt", connection.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if replay := call("POST", "/api/v1/onboarding", data, model.DeviceCredential{}, true); replay.Code != 200 || !strings.Contains(replay.Body.String(), `"reused":true`) || strings.Contains(replay.Body.String(), created.Credential.Secret) { /* 判断条件并选择处理分支。 */
		t.Fatal("lost response retry failed or leaked secret", replay.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if legacy := call("POST", "/api/v1/device-ingest/device", []byte(`{"protocolId":"untrusted","payload":{"properties":{"x":1}}}`), created.Credential, false); legacy.Code != 422 { /* 判断条件并选择处理分支。 */
		t.Fatal("standard device bypassed standard ingress", legacy.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	path := "/api/v1/device-ingest/standard/tenant/product/device/property"              /* 更新 path 的值。 */
	if w = call("POST", path, payload, model.DeviceCredential{}, false); w.Code != 401 { /* 判断条件并选择处理分支。 */
		t.Fatal("anonymous", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w = call("POST", strings.Replace(path, "/device/", "/other-device/", 1), payload, created.Credential, false); w.Code != 401 { /* 判断条件并选择处理分支。 */
		t.Fatal("device boundary", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w = call("POST", strings.Replace(path, "/tenant/", "/other/", 1), payload, created.Credential, false); w.Code != 401 { /* 判断条件并选择处理分支。 */
		t.Fatal("tenant boundary", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w = call("POST", path, []byte(strings.Repeat("x", 65537)), created.Credential, false); w.Code != 413 { /* 判断条件并选择处理分支。 */
		t.Fatal("body limit", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w = call("POST", path, payload, created.Credential, false); w.Code != http.StatusAccepted { /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var accepted struct { /* 声明 accepted。 */
		MessageID string `json:"messageId"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)                   /* 更新 _ 的值。 */
	idx, err := repo.GetRawIndex(ctx, "tenant", accepted.MessageID) /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw, err := engine.GetRaw(ctx, idx)                   /* 更新 err 的值。 */
	if err != nil || !bytes.Equal(raw.Payload, payload) { /* 判断条件并选择处理分支。 */
		t.Fatal("raw archive missing", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	m, err := repo.GetStandardMessageByRaw(ctx, "tenant", idx.MessageID) /* 更新 err 的值。 */
	if err != nil || m.Properties["temperature"] != 26.5 {               /* 判断条件并选择处理分支。 */
		t.Fatal("standard message missing", err, m) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w = call("POST", path, payload, created.Credential, false); w.Code != 202 || !strings.Contains(w.Body.String(), `"created":false`) { /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	conflict := bytes.Replace(payload, []byte("26.5"), []byte("99.9"), 1)                                                                     /* 更新 conflict 的值。 */
	if w = call("POST", path, conflict, created.Credential, false); w.Code != 409 || !strings.Contains(w.Body.String(), "MESSAGE_CONFLICT") { /* 判断条件并选择处理分支。 */
		t.Fatal("conflicting duplicate", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	preserved, err := engine.GetRaw(ctx, idx)                   /* 更新 err 的值。 */
	if err != nil || !bytes.Equal(preserved.Payload, payload) { /* 判断条件并选择处理分支。 */
		t.Fatal("conflict overwrote archived raw", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	connection = call("GET", "/api/v1/device-registry/device/connection", nil, model.DeviceCredential{}, true)                                                                                             /* 更新 connection 的值。 */
	if connection.Code != 200 || !strings.Contains(connection.Body.String(), `"stage":"PARSED"`) || diagnosisStage(connection) != "PARSED" || !strings.Contains(connection.Body.String(), idx.MessageID) { /* 判断条件并选择处理分支。 */
		t.Fatal("parsed receipt missing", connection.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// State still traverses raw archival and parsing before updating connectivity.
	statePayload := []byte(`{"id":"state-1","timestamp":1788850000001,"data":{"connectionStatus":"CONNECTED"}}`) /* 更新 statePayload 的值。 */
	w = call("POST", strings.TrimSuffix(path, "property")+"state", statePayload, created.Credential, false)      /* 更新 w 的值。 */
	if w.Code != 202 {                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("state ingest", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	state, e := repo.GetDeviceState(ctx, "tenant", "device")                                       /* 更新 e 的值。 */
	if e != nil || state.ConnectionStatus != "CONNECTED" || state.LastConnectAt != 1788850000001 { /* 判断条件并选择处理分支。 */
		t.Fatal("state projection", state, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	w = call("POST", "/api/v1/device-mqtt/token", nil, created.Credential, false) /* 更新 w 的值。 */
	if w.Code != 200 {                                                            /* 判断条件并选择处理分支。 */
		t.Fatal("MQTT credential exchange", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var mqttToken struct { /* 声明 mqttToken。 */
		Token     string `json:"token"`     /* 执行当前语句并推进处理流程。 */
		ExpiresIn int    `json:"expiresIn"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_ = json.Unmarshal(w.Body.Bytes(), &mqttToken)   /* 更新 _ 的值。 */
	mqttClaims, e := srv.auth.Parse(mqttToken.Token) /* 更新 e 的值。 */
	if e != nil || mqttToken.ExpiresIn != 300 {      /* 判断条件并选择处理分支。 */
		t.Fatal("invalid device MQTT token", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(w.Body.String(), "shadow") { /* 判断条件并选择处理分支。 */
		t.Fatal("device token response still advertises shadow topics") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	claimsJSON, _ := json.Marshal(mqttClaims)           /* 更新 _ 的值。 */
	if strings.Contains(string(claimsJSON), "shadow") { /* 判断条件并选择处理分支。 */
		t.Fatal("device token still authorizes shadow topics") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(string(claimsJSON), "/external/raw/") || !strings.Contains(string(claimsJSON), "/iot/up/tenant/product/device/property") { /* 判断条件并选择处理分支。 */
		t.Fatal("standard device ACL includes raw bypass or misses topic") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	w = call("POST", "/api/v1/device-registry/device/credentials", []byte(`{}`), model.DeviceCredential{}, true) /* 更新 w 的值。 */
	if w.Code != 200 {                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("rotate credential", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var rotated struct { /* 声明 rotated。 */
		Credential model.DeviceCredential `json:"credential"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_ = json.Unmarshal(w.Body.Bytes(), &rotated)                                   /* 更新 _ 的值。 */
	if w = call("POST", path, payload, created.Credential, false); w.Code != 401 { /* 判断条件并选择处理分支。 */
		t.Fatal("rotated old credential accepted", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w = call("POST", path, payload, rotated.Credential, false); w.Code != 202 { /* 判断条件并选择处理分支。 */
		t.Fatal("new credential rejected", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	created.Credential = rotated.Credential                                                               /* 更新 created.Credential 的值。 */
	w = call("DELETE", "/api/v1/device-registry/device/credentials", nil, model.DeviceCredential{}, true) /* 更新 w 的值。 */
	if w.Code != 200 {                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w = call("POST", path, payload, created.Credential, false); w.Code != 401 { /* 判断条件并选择处理分支。 */
		t.Fatal("disabled", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func diagnosisStage(w *httptest.ResponseRecorder) string {
	var body struct {
		Diagnosis struct {
			Stage string `json:"stage"`
		} `json:"diagnosis"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Diagnosis.Stage
}
