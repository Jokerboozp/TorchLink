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
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"strings"                               /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type connectionSnapshot struct{} /* 定义 connectionSnapshot 类型。 */

func TestDeviceProfileRequiresIdentityEvidence(t *testing.T) { /* 定义 TestDeviceProfileRequiresIdentityEvidence 函数。 */
	d := model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Tags: map[string]string{"connectorProfileId": "listener"}} /* 更新 d 的值。 */
	p := model.DeviceAccessProfile{TenantID: "t", ID: "listener", ProductID: "p"}                                               /* 更新 p 的值。 */
	if !deviceUsesProfile(d, p, nil) {                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal("explicit binding missing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, other := range []model.DeviceAccessProfile{{TenantID: "other", ID: "listener", ProductID: "p", DeviceID: "d"}, {TenantID: "t", ID: "listener", ProductID: "other", DeviceID: "d"}} { /* 循环处理当前数据。 */
		if deviceUsesProfile(d, other, []map[string]any{{"deviceId": "d"}}) { /* 判断条件并选择处理分支。 */
			t.Fatal("identity scope bypass") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	d.Tags = nil                                                                   /* 更新 d.Tags 的值。 */
	if deviceUsesProfile(d, p, []map[string]any{{"remoteAddress": "127.0.0.1"}}) { /* 判断条件并选择处理分支。 */
		t.Fatal("unidentified session matched") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (connectionSnapshot) Status(string, string) (string, string, int64) { return "LISTENING", "", 123 } /* 定义 Status 函数。 */
func (connectionSnapshot) Sessions(tenant, id string) []map[string]any { /* 定义 Sessions 函数。 */
	if tenant == "t" && (id == "listener-a" || id == "listener-b") { /* 判断条件并选择处理分支。 */
		return []map[string]any{{"deviceId": "legacy", "remoteAddress": "127.0.0.1:1234", "lastSeenAt": int64(123)}} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (connectionSnapshot) Command(context.Context, string, string, string, map[string]any) (map[string]any, error) { /* 定义 Command 函数。 */
	return nil, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestLegacyDeviceConnectionResolution(t *testing.T) { /* 定义 TestLegacyDeviceConnectionResolution 函数。 */
	ctx := context.Background()            /* 更新 ctx 的值。 */
	repo := memory.NewRepository()         /* 更新 repo 的值。 */
	root := t.TempDir()                    /* 更新 root 的值。 */
	archive, err := local.NewArchive(root) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                         /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	cfg := config.Load()                                                                                          /* 更新 cfg 的值。 */
	cfg.JWTSecret = "legacy-test-key-32-characters"                                                               /* 更新 cfg.JWTSecret 的值。 */
	srv := New(cfg, engine, metrics.New(), log)                                                                   /* 更新 srv 的值。 */
	srv.SetProtocolListeners(connectionSnapshot{})                                                                /* 执行当前语句并推进处理流程。 */
	must := func(err error) {                                                                                     /* 更新 must 的值。 */
		t.Helper()      /* 执行当前语句并推进处理流程。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	must(repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"})) /* 执行当前语句并推进处理流程。 */
	for _, id := range []string{"legacy", "poll-device", "unrelated"} {                   /* 循环处理当前数据。 */
		must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id, ProductID: "p", Status: "ENABLED"})) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, p := range []model.DeviceAccessProfile{ /* 循环处理当前数据。 */
		{TenantID: "t", ID: "listener-a", ProductID: "p", Mode: "listener", Network: "tcp", Enabled: true},             /* 执行当前语句并推进处理流程。 */
		{TenantID: "t", ID: "listener-b", ProductID: "p", Mode: "listener", Network: "udp", Enabled: false},            /* 执行当前语句并推进处理流程。 */
		{TenantID: "t", ID: "poll", ProductID: "p", DeviceID: "poll-device", Mode: "poll", Enabled: true},              /* 执行当前语句并推进处理流程。 */
		{TenantID: "other", ID: "listener-other", ProductID: "p", DeviceID: "legacy", Mode: "listener", Enabled: true}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		must(repo.SaveDeviceAccessProfile(ctx, p)) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	call := func(method, path, body string) (int, map[string]any) { /* 更新 call 的值。 */
		t.Helper()                                                       /* 执行当前语句并推进处理流程。 */
		token, _ := srv.auth.Issue("test", "t", "admin", nil, time.Hour) /* 更新 _ 的值。 */
		r := httptest.NewRequest(method, path, strings.NewReader(body))  /* 更新 r 的值。 */
		r.Header.Set("Authorization", "Bearer "+token)                   /* 执行当前语句并推进处理流程。 */
		r.Header.Set("Content-Type", "application/json")                 /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                                      /* 更新 w 的值。 */
		srv.Handler().ServeHTTP(w, r)                                    /* 执行当前语句并推进处理流程。 */
		var v map[string]any                                             /* 声明 v。 */
		must(json.Unmarshal(w.Body.Bytes(), &v))                         /* 执行当前语句并推进处理流程。 */
		return w.Code, v                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	code, v := call("GET", "/api/v1/device-registry/legacy/connection", "")                                       /* 更新 v 的值。 */
	if code != 200 || v["profile"] != nil || len(v["profiles"].([]any)) != 2 || len(v["sessions"].([]any)) != 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("ambiguous: %d %+v", code, v) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	code, v = call("GET", "/api/v1/device-registry/legacy/connection?profileId=listener-b", "")                                                    /* 更新 v 的值。 */
	if code != 200 || v["connector"] != "UDP" || v["profile"].(map[string]any)["runtimeStatus"] != "DISABLED" || len(v["sessions"].([]any)) != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("selected: %d %+v", code, v) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, id := range []string{"poll", "listener-other"} { /* 循环处理当前数据。 */
		code, _ = call("GET", "/api/v1/device-registry/legacy/connection?profileId="+id, "") /* 更新 _ 的值。 */
		if code != 422 {                                                                     /* 判断条件并选择处理分支。 */
			t.Fatal(code) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	code, v = call("GET", "/api/v1/device-registry/poll-device/connection", "")                         /* 更新 v 的值。 */
	if code != 200 || v["connector"] != "MODBUS_TCP" || v["profile"].(map[string]any)["id"] != "poll" { /* 判断条件并选择处理分支。 */
		t.Fatalf("poll: %+v", v) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, v = call("GET", "/api/v1/device-registry/unrelated/connection", "") /* 更新 v 的值。 */
	if v["profile"] != nil || len(v["profiles"].([]any)) != 0 {            /* 判断条件并选择处理分支。 */
		t.Fatalf("product-only match: %+v", v) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, v = call("GET", "/api/v1/connectors", "") /* 更新 v 的值。 */
	for _, item := range v["items"].([]any) {    /* 循环处理当前数据。 */
		x := item.(map[string]any)                /* 更新 x 的值。 */
		if len(x["recentDevices"].([]any)) != 1 { /* 判断条件并选择处理分支。 */
			t.Fatalf("legacy device missing: %+v", x) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	device, err := repo.GetManagedDevice(ctx, "t", "legacy") /* 更新 err 的值。 */
	must(err)                                                /* 执行当前语句并推进处理流程。 */
	if len(device.Tags) != 0 {                               /* 判断条件并选择处理分支。 */
		t.Fatal("read mutated legacy device") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	code, v = call("POST", "/api/v1/onboarding", `{"requestId":"r1","productId":"missing","device":{"id":"test","name":"test"},"connection":{"mode":"standard"}}`)
	if code != 422 || v["detail"] != "设备模板不存在或当前账号无权查看" {
		t.Fatalf("validation result: %d %+v", code, v)
	}
	if _, err = repo.GetManagedDevice(ctx, "t", "test"); err == nil {
		t.Fatal("rejected onboarding saved a device")
	}
} /* 结束当前表达式或代码块。 */
