package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
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

func TestProtocolDevicesHaveNoPlatformCredentials(t *testing.T) { /* 定义 TestProtocolDevicesHaveNoPlatformCredentials 函数。 */
	ctx := context.Background()            /* 更新 ctx 的值。 */
	repo := memory.NewRepository()         /* 更新 repo 的值。 */
	root := t.TempDir()                    /* 更新 root 的值。 */
	archive, err := local.NewArchive(root) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                                                      /* 更新 log 的值。 */
	cfg := config.Load()                                                                                                                                       /* 更新 cfg 的值。 */
	cfg.DataDir, cfg.JWTSecret = root, "credential-scope-test-key-32-characters"                                                                               /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log), metrics.New(), log) /* 更新 api 的值。 */
	call := func(method, path, body, tenant, role, key, secret string) *httptest.ResponseRecorder {                                                            /* 更新 call 的值。 */
		token, err := api.auth.Issue("tester", tenant, role, nil, time.Minute) /* 检查错误并决定后续处理。 */
		if err != nil {                                                        /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		req := httptest.NewRequest(method, path, strings.NewReader(body)) /* 更新 req 的值。 */
		req.Header.Set("Authorization", "Bearer "+token)                  /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Content-Type", "application/json")                /* 执行当前语句并推进处理流程。 */
		req.Header.Set("X-Device-Key", key)                               /* 执行当前语句并推进处理流程。 */
		req.Header.Set("X-Device-Secret", secret)                         /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                                       /* 更新 w 的值。 */
		api.Handler().ServeHTTP(w, req)                                   /* 执行当前语句并推进处理流程。 */
		return w                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, transport := range []string{"TCP", "UDP", "TCP_UDP", "MODBUS_TCP", "MODBUS_RTU_TCP"} { /* 循环处理当前数据。 */
		t.Run(transport, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			id := strings.ToLower(transport)                                                        /* 更新 id 的值。 */
			p := model.Product{TenantID: "tenant", ID: id, Status: "ENABLED", Transport: transport} /* 更新 p 的值。 */
			if err := repo.SaveProduct(ctx, p); err != nil {                                        /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			w := call("POST", "/api/v1/device-registry", `{"id":"`+id+`","name":"设备","productId":"`+id+`"}`, "tenant", "admin", "", "")           /* 更新 w 的值。 */
			if w.Code != 201 || bytes.Contains(w.Body.Bytes(), []byte(`"credential"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected creation: %d %s", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			discoveredID := "discovered-" + id                                                                                                /* 更新 discoveredID 的值。 */
			if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant", DeviceID: discoveredID, ProductID: id}); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			w = call("POST", "/api/v1/discovered-devices/"+discoveredID+"/register", "{}", "tenant", "admin", "", "")                             /* 更新 w 的值。 */
			if w.Code != 201 || bytes.Contains(w.Body.Bytes(), []byte(`"credential"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) { /* 判断条件并选择处理分支。 */
				t.Fatalf("discovery generated credentials: %d %s", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			d, err := repo.GetManagedDevice(ctx, "tenant", id)                                                  /* 更新 err 的值。 */
			if err != nil || d.SecretHash != "" || d.AccessKey != model.ProtocolDeviceAccessKey("tenant", id) { /* 判断条件并选择处理分支。 */
				t.Fatal("invalid stored identity", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			// Existing records may still contain formerly generated secrets. They
			// must not expose or accept those as an alternate HTTP/MQTT identity.
			d.SecretHash, d.SecretHint = onboarding.Hash("old-secret"), "secret" /* 更新 d.SecretHint 的值。 */
			if err = repo.SaveManagedDevice(ctx, d); err != nil {                /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			w = call("GET", "/api/v1/device-registry/"+id+"/connection", "", "tenant", "admin", "", "")                                                           /* 更新 w 的值。 */
			if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"credentialSupported":false`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected detail: %d %s", w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			for _, method := range []string{"POST", "DELETE"} { /* 循环处理当前数据。 */
				path := "/api/v1/device-registry/" + id + "/credentials" /* 更新 path 的值。 */
				for _, scope := range []struct {                         /* 循环处理当前数据。 */
					tenant, role string /* 执行当前语句并推进处理流程。 */
					status       int    /* 执行当前语句并推进处理流程。 */
				}{{"tenant", "admin", 422}, {"tenant", "viewer", 403}, {"other", "admin", 404}} { /* 结束当前表达式或代码块。 */
					w = call(method, path, "{}", scope.tenant, scope.role, "", "") /* 更新 w 的值。 */
					if w.Code != scope.status {                                    /* 判断条件并选择处理分支。 */
						t.Fatalf("%s %s: %d want %d", method, path, w.Code, scope.status) /* 验证实际结果符合预期。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			for _, path := range []string{"/api/v1/device-mqtt/token", "/api/v1/device-ingest/" + id, "/api/v1/device-ingest/standard/tenant/" + id + "/" + id + "/property"} { /* 循环处理当前数据。 */
				w = call("POST", path, "{}", "tenant", "admin", d.AccessKey, "old-secret") /* 更新 w 的值。 */
				if w.Code != 401 {                                                         /* 判断条件并选择处理分支。 */
					t.Fatalf("native device authenticated on %s: %d", path, w.Code) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			after, _ := repo.GetManagedDevice(ctx, "tenant", id)                    /* 更新 _ 的值。 */
			if after.AccessKey != d.AccessKey || after.SecretHash != d.SecretHash { /* 判断条件并选择处理分支。 */
				t.Fatal("rejected operation mutated device") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			revocations, err := repo.ListCredentialRevocations(ctx, "tenant", id, false) /* 更新 err 的值。 */
			if err != nil || len(revocations) != 0 {                                     /* 判断条件并选择处理分支。 */
				t.Fatal("rejected operation generated revocations", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	w := call("GET", "/api/v1/device-registry", "", "tenant", "admin", "", "")  /* 更新 w 的值。 */
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) { /* 判断条件并选择处理分支。 */
		t.Fatal("registry exposes protocol keys", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
