package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                                 /* 执行当前语句并推进处理流程。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"encoding/json"                         /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net/http"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestSplitGatewayHTTPFlow(t *testing.T) { /* 定义 TestSplitGatewayHTTPFlow 函数。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                          /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir())           /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	bus := local.NewBus()                                 /* 更新 bus 的值。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil)) /* 更新 log 的值。 */
	// Separate engines, shared test repository and queue. Only the API consumes Raw.
	gatewayEngine := core.New(repo, archive, bus, local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log) /* 更新 gatewayEngine 的值。 */
	apiEngine := core.New(repo, archive, bus, local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)     /* 更新 apiEngine 的值。 */
	if err = apiEngine.Start(ctx); err != nil {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                        /* 更新 cfg 的值。 */
	cfg.JWTSecret = "split-gateway-test-signing-secret"                         /* 更新 cfg.JWTSecret 的值。 */
	cfg.ProcessRole = "gateway"                                                 /* 更新 cfg.ProcessRole 的值。 */
	gateway := New(cfg, gatewayEngine, metrics.New(), log)                      /* 更新 gateway 的值。 */
	upstream := httptest.NewServer(gateway.Handler())                           /* 更新 upstream 的值。 */
	defer upstream.Close()                                                      /* 安排函数结束时执行清理。 */
	cfg.ProcessRole, cfg.AccessGatewayURL = "api", upstream.URL                 /* 更新 cfg.AccessGatewayURL 的值。 */
	api := New(cfg, apiEngine, metrics.New(), log)                              /* 更新 api 的值。 */
	token, err := api.auth.Issue("tester", "tenant", "admin", nil, time.Minute) /* 检查错误并决定后续处理。 */
	if err != nil {                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	call := func(handler http.Handler, method, path string, body any, auth string, credential model.DeviceCredential) *httptest.ResponseRecorder { /* 更新 call 的值。 */
		b, err := json.Marshal(body) /* 更新 err 的值。 */
		if err != nil {              /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		r := httptest.NewRequest(method, path, bytes.NewReader(b)) /* 更新 r 的值。 */
		r.Header.Set("Content-Type", "application/json")           /* 执行当前语句并推进处理流程。 */
		if auth != "" {                                            /* 判断条件并选择处理分支。 */
			r.Header.Set("Authorization", "Bearer "+auth) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		r.Header.Set("X-Device-Key", credential.AccessKey) /* 执行当前语句并推进处理流程。 */
		r.Header.Set("X-Device-Secret", credential.Secret) /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                        /* 更新 w 的值。 */
		handler.ServeHTTP(w, r)                            /* 执行当前语句并推进处理流程。 */
		return w                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q := onboarding.Request{ProductID: "product", ProductName: "产品", DeviceID: "device", Name: "设备", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"sample","timestamp":1788850000000,"data":{"temperature":42}}`)} /* 更新 q 的值。 */
	if r := call(gateway.Handler(), "POST", "/api/v1/auth/login", nil, "", model.DeviceCredential{}); r.Code != 404 {                                                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatal("gateway exposed login", r.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if r := call(api.Handler(), "POST", "/api/v1/onboarding/test", q, "", model.DeviceCredential{}); r.Code != 401 { /* 判断条件并选择处理分支。 */
		t.Fatal("forward bypassed auth", r.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	test := call(api.Handler(), "POST", "/api/v1/onboarding/test", q, token, model.DeviceCredential{}) /* 更新 test 的值。 */
	var preview connector.Result                                                                       /* 声明 preview。 */
	if test.Code != 200 || json.Unmarshal(test.Body.Bytes(), &preview) != nil || !preview.Success {    /* 判断条件并选择处理分支。 */
		t.Fatal("preview", test.Code, test.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.TestToken = preview.TestToken                                                                /* 更新 q.TestToken 的值。 */
	saved := call(api.Handler(), "POST", "/api/v1/onboarding", q, token, model.DeviceCredential{}) /* 更新 saved 的值。 */
	var result onboarding.Result                                                                   /* 声明 result。 */
	if saved.Code != 201 || json.Unmarshal(saved.Body.Bytes(), &result) != nil {                   /* 判断条件并选择处理分支。 */
		t.Fatal("save", saved.Code, saved.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ingest := call(api.Handler(), "POST", "/api/v1/device-ingest/standard/tenant/product/device/property", q.Payload, "", result.Credential) /* 更新 ingest 的值。 */
	if ingest.Code != 202 {                                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal("ingest", ingest.Code, ingest.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var accepted struct { /* 声明 accepted。 */
		MessageID string `json:"messageId"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_ = json.Unmarshal(ingest.Body.Bytes(), &accepted) /* 更新 _ 的值。 */
	deadline := time.Now().Add(3 * time.Second)        /* 更新 deadline 的值。 */
	for {                                              /* 循环处理当前数据。 */
		msg, err := repo.GetStandardMessageByRaw(ctx, "tenant", accepted.MessageID) /* 更新 err 的值。 */
		if err == nil {                                                             /* 判断条件并选择处理分支。 */
			if msg.Properties["temperature"] != float64(42) { /* 判断条件并选择处理分支。 */
				t.Fatal("decoded wrong value") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if time.Now().After(deadline) { /* 判断条件并选择处理分支。 */
			t.Fatal("API consumer did not parse gateway raw", err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if r := call(api.Handler(), "GET", "/api/v1/device-registry/device/connection", nil, token, model.DeviceCredential{}); r.Code != 200 { /* 判断条件并选择处理分支。 */
		t.Fatal("connection forwarding", r.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	upstream.Close()                                                                                                    /* 执行当前语句并推进处理流程。 */
	if r := call(api.Handler(), "POST", "/api/v1/onboarding/test", q, token, model.DeviceCredential{}); r.Code != 503 { /* 判断条件并选择处理分支。 */
		t.Fatal("unavailable gateway falsely succeeded", r.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
