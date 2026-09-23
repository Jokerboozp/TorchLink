package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
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

func TestIndependentProductAndDeviceRegistration(t *testing.T) { /* 定义 TestIndependentProductAndDeviceRegistration 函数。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                          /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir())           /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log) /* 更新 engine 的值。 */
	if err := engine.Start(ctx); err != nil {                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                  /* 更新 cfg 的值。 */
	cfg.JWTSecret = "independent-product-test-key-32-chars"                                                                               /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, metrics.New(), log)                                                                                           /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                           /* 更新 server 的值。 */
	defer server.Close()                                                                                                                  /* 安排函数结束时执行清理。 */
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Hour)                                                               /* 更新 _ 的值。 */
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)                                                             /* 更新 _ 的值。 */
	product := map[string]any{"id": "standard-product", "name": "独立标准产品", "protocolPackageId": "iot-standard@1.0.0", "transport": "HTTP"} /* 更新 product 的值。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", viewer, product, 403)                                          /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, product, 201)                                           /* 执行当前语句并推进处理流程。 */
	release, err := repo.GetProtocolRelease(ctx, "tenant", parser.StandardProtocolID, "1.0.0")                                            /* 更新 err 的值。 */
	if err != nil || release.ParserType != parser.StandardParserName {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("missing standard release: %+v %v", release, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := repo.GetProtocolRelease(ctx, "other", parser.StandardProtocolID, "1.0.0"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("release crossed tenant boundary") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	result := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/device-registry", token, map[string]any{"id": "independent-device", "name": "独立设备", "productId": "standard-product"}, 201) /* 更新 result 的值。 */
	credential, ok := result["credential"].(map[string]any)                                                                                                                                          /* 更新 ok 的值。 */
	if !ok || credential["secret"] == "" {                                                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("device registration did not issue credential") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Product creation is repeatable and never rewrites the immutable release.
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/products/standard-product", token, product, 201) /* 执行当前语句并推进处理流程。 */
	again, _ := repo.GetProtocolRelease(ctx, "tenant", parser.StandardProtocolID, "1.0.0")                      /* 更新 _ 的值。 */
	if again.CreatedAt != release.CreatedAt {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal("standard release was replaced") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw, err := api.onboarding.PrepareStandard(ctx, "tenant", "standard-product", "independent-device", "property", "HTTP", []byte(`{"version":"1.0","id":"first","timestamp":1789315000000,"data":{"temperature":26}}`)) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	deadline := time.Now().Add(3 * time.Second) /* 更新 deadline 的值。 */
	for {                                       /* 循环处理当前数据。 */
		state, err := repo.GetDeviceState(ctx, "tenant", "independent-device") /* 更新 err 的值。 */
		if err == nil && state.LastSeenAt > 0 {                                /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if time.Now().After(deadline) { /* 判断条件并选择处理分支。 */
			t.Fatalf("first report was not parsed: %+v %v", state, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	// A published Go release is selectable before a compatibility package exists.
	goRelease := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "fire-go", Version: "1.0.0", Status: "PUBLISHED", ParserType: parser.GoProtocolParserName, Transport: "TCP", PayloadFormat: "hex"} /* 更新 goRelease 的值。 */
	if err := repo.CreateProtocolRelease(ctx, goRelease); err != nil {                                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "go-product", "name": "Go 产品", "protocolPackageId": "fire-go@1.0.0"}, 201) /* 执行当前语句并推进处理流程。 */
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "go-product")                                                                                                   /* 更新 err 的值。 */
	if err != nil || binding.ProtocolID != "fire-go" {                                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("new product is not bound: %+v %v", binding, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, status := range []string{"VALIDATED", "REVOKED"} { /* 循环处理当前数据。 */
		blocked := goRelease                                             /* 更新 blocked 的值。 */
		blocked.Version = status                                         /* 更新 blocked.Version 的值。 */
		blocked.Status = status                                          /* 更新 blocked.Status 的值。 */
		if err := repo.CreateProtocolRelease(ctx, blocked); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/products", token, map[string]any{"id": status, "name": "不可绑定", "protocolPackageId": "fire-go@" + status}, 422) /* 执行当前语句并推进处理流程。 */
		if _, err := repo.GetProduct(ctx, "tenant", status); err == nil {                                                                                                                   /* 判断条件并选择处理分支。 */
			t.Fatal("invalid release created a product") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
