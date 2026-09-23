package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net"               /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestModbusOnboardingRuntimeChain(t *testing.T) { /* 定义 TestModbusOnboardingRuntimeChain 函数。 */
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                           /* 安排函数结束时执行清理。 */
	listener, err := net.Listen("tcp", "127.0.0.1:0")                        /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer listener.Close() /* 安排函数结束时执行清理。 */
	go func() {            /* 执行当前语句并推进处理流程。 */
		for { /* 循环处理当前数据。 */
			conn, err := listener.Accept() /* 更新 err 的值。 */
			if err != nil {                /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			_ = conn.SetDeadline(time.Now().Add(time.Second))    /* 更新 _ 的值。 */
			request := make([]byte, 12)                          /* 更新 request 的值。 */
			if _, err = io.ReadFull(conn, request); err == nil { /* 判断条件并选择处理分支。 */
				_, _ = conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42}) /* 更新 _ 的值。 */
			} /* 结束当前表达式或代码块。 */
			_ = conn.Close() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	repo := memory.NewRepository()         /* 更新 repo 的值。 */
	root := t.TempDir()                    /* 更新 root 的值。 */
	archive, err := local.NewArchive(root) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                         /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	if err = engine.Start(ctx); err != nil {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                    /* 更新 cfg 的值。 */
	cfg.DataDir, cfg.JWTSecret, cfg.ModbusAllowedCIDRs = root, "isolated-modbus-chain-signing-key", []string{"127.0.0.0/8"} /* 更新 cfg.ModbusAllowedCIDRs 的值。 */
	srv := New(cfg, engine, metrics.New(), log)                                                                             /* 更新 srv 的值。 */
	token, err := srv.auth.Issue("tester", "tenant", "admin", nil, time.Minute)                                             /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	call := func(method, path string, body any) *httptest.ResponseRecorder { /* 更新 call 的值。 */
		payload, err := json.Marshal(body) /* 更新 err 的值。 */
		if err != nil {                    /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		req := httptest.NewRequest(method, path, bytes.NewReader(payload)) /* 更新 req 的值。 */
		req.Header.Set("Authorization", "Bearer "+token)                   /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Content-Type", "application/json")                 /* 执行当前语句并推进处理流程。 */
		out := httptest.NewRecorder()                                      /* 更新 out 的值。 */
		srv.Handler().ServeHTTP(out, req)                                  /* 执行当前语句并推进处理流程。 */
		return out                                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q := onboarding.Request{ProductID: "modbus-product", ProductName: "模拟温度产品", DeviceID: "modbus-device", Name: "模拟温度设备", Type: connector.ModbusTCP, PollIntervalSec: 1, Profile: model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500}, PointTableCSV: "name,functionCode,address,addressNotation,dataType,scale,bit\ntemperature,3,0,zero_based,uint16,1,\ninput6,3,0,zero_based,bits,1,5\n"} /* 更新 q 的值。 */
	preview := call("POST", "/api/v1/onboarding/test", q)                                                                                                                                                                                                                                                                                                                                                                                                     /* 更新 preview 的值。 */
	var tested connector.Result                                                                                                                                                                                                                                                                                                                                                                                                                               /* 声明 tested。 */
	if err = json.Unmarshal(preview.Body.Bytes(), &tested); err != nil || preview.Code != 200 || !tested.Success || tested.Source != "network-read" {                                                                                                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal("preview", preview.Code, preview.Body.String(), err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.TestToken = tested.TestToken                   /* 更新 q.TestToken 的值。 */
	created := call("POST", "/api/v1/onboarding", q) /* 更新 created 的值。 */
	if created.Code != 201 {                         /* 判断条件并选择处理分支。 */
		t.Fatal("save", created.Code, created.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	recovered := call("POST", "/api/v1/onboarding", q)                                             /* 更新 recovered 的值。 */
	if recovered.Code != 200 || !bytes.Contains(recovered.Body.Bytes(), []byte(`"reused":true`)) { /* 判断条件并选择处理分支。 */
		t.Fatal("Modbus onboarding recovery failed", recovered.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, response := range []*httptest.ResponseRecorder{created, recovered} { /* 循环处理当前数据。 */
		for _, field := range []string{`"credential"`, `"accessKey"`, `"clientId"`, `"username"`} { /* 循环处理当前数据。 */
			if bytes.Contains(response.Body.Bytes(), []byte(field)) { /* 判断条件并选择处理分支。 */
				t.Fatalf("Modbus onboarding returned %s", field) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	stored, err := repo.GetManagedDevice(ctx, "tenant", q.DeviceID)      /* 更新 err 的值。 */
	if err != nil || stored.SecretHash != "" || stored.AccessKey == "" { /* 判断条件并选择处理分支。 */
		t.Fatal("Modbus must persist an internal identity without a secret", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	connection := func() map[string]any { /* 更新 connection 的值。 */
		result := call("GET", "/api/v1/device-registry/modbus-device/connection", nil) /* 更新 result 的值。 */
		var data map[string]any                                                        /* 声明 data。 */
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &data) != nil {   /* 判断条件并选择处理分支。 */
			t.Fatal("connection", result.Code) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return data /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if connection()["ingest"].(map[string]any)["rawReceived"] != false { /* 判断条件并选择处理分支。 */
		t.Fatal("preview persisted business data") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	runtime := protocolruntime.New(repo, func(ctx context.Context, raw model.RawMessage) error { /* 更新 runtime 的值。 */
		_, _, err := engine.IngestRaw(ctx, raw) /* 更新 err 的值。 */
		return err                              /* 返回当前处理结果。 */
	}, log, "127.0.0.0/8") /* 结束当前表达式或代码块。 */
	runtime.Start(ctx)                /* 执行当前语句并推进处理流程。 */
	wait := func(check func() bool) { /* 更新 wait 的值。 */
		t.Helper()     /* 执行当前语句并推进处理流程。 */
		for !check() { /* 循环处理当前数据。 */
			select { /* 根据条件选择处理路径。 */
			case <-ctx.Done(): /* 处理当前分支。 */
				t.Fatal("runtime chain timed out") /* 验证实际结果符合预期。 */
			case <-time.After(20 * time.Millisecond): /* 处理当前分支。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wait(func() bool { return connection()["ingest"].(map[string]any)["parsed"] == true })                                         /* 执行当前语句并推进处理流程。 */
	records, count, err := repo.ListDeviceMessages(ctx, "tenant", "modbus-device", model.PropertyReport, 10, 0)                    /* 更新 err 的值。 */
	if err != nil || count < 1 || records[0].Properties["temperature"] != float64(42) || records[0].Properties["input6"] != true { /* 判断条件并选择处理分支。 */
		t.Fatal("missing parsed simulator value", count, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	index, err := repo.GetRawIndex(ctx, "tenant", records[0].RawMessageID)   /* 更新 err 的值。 */
	if err != nil || index.ParseAttemptedAt == 0 || index.ParseError != "" { /* 判断条件并选择处理分支。 */
		t.Fatal("missing raw parse evidence", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_ = listener.Close()                                                                              /* 更新 _ 的值。 */
	wait(func() bool { return connection()["profile"].(map[string]any)["runtimeStatus"] == "ERROR" }) /* 执行当前语句并推进处理流程。 */
	if connection()["ingest"].(map[string]any)["parsed"] != true {                                    /* 判断条件并选择处理分支。 */
		t.Fatal("failed polling removed previous evidence") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
