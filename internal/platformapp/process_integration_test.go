package platformapp /* 声明 platformapp 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net"           /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"os/exec"       /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"github.com/jackc/pgx/v5"                 /* 执行当前语句并推进处理流程。 */
	"github.com/jackc/pgx/v5/pgxpool"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/postgres" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestPlatformProcessHelper(t *testing.T) { /* 定义 TestPlatformProcessHelper 函数。 */
	if role := os.Getenv("IOT_TEST_APP_ROLE"); role != "" { /* 判断条件并选择处理分支。 */
		Run(role) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// The Kafka address MUST point to a disposable broker: production topic and
// consumer-group names are deliberately exercised by the actual startup code.
func TestSplitProcessesPostgresKafkaRecovery(t *testing.T) { /* 定义 TestSplitProcessesPostgresKafkaRecovery 函数。 */
	dsn, broker := os.Getenv("IOT_TEST_POSTGRES_DSN"), os.Getenv("IOT_TEST_DISPOSABLE_KAFKA") /* 更新 broker 的值。 */
	if dsn == "" || broker == "" {                                                            /* 判断条件并选择处理分支。 */
		t.Skip("requires PostgreSQL and an explicitly disposable Kafka broker") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                            /* 安排函数结束时执行清理。 */
	admin, err := pgxpool.New(ctx, dsn)                                       /* 更新 err 的值。 */
	if err != nil {                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("initialize test database") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer admin.Close()                                               /* 安排函数结束时执行清理。 */
	schema := fmt.Sprintf("gateway_test_%d", time.Now().UnixNano())   /* 更新 schema 的值。 */
	ident := pgx.Identifier{schema}.Sanitize()                        /* 更新 ident 的值。 */
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("create isolated schema") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }() /* 安排函数结束时执行清理。 */
	u, err := url.Parse(dsn)                                                                    /* 更新 err 的值。 */
	if err != nil || u.Scheme == "" {                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("test requires URL PostgreSQL DSN") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	query := u.Query()               /* 更新 query 的值。 */
	query.Set("search_path", schema) /* 执行当前语句并推进处理流程。 */
	u.RawQuery = query.Encode()      /* 更新 u.RawQuery 的值。 */
	isolatedDSN := u.String()        /* 更新 isolatedDSN 的值。 */
	address := func() string {       /* 更新 address 的值。 */
		listener, e := net.Listen("tcp", "127.0.0.1:0") /* 更新 e 的值。 */
		if e != nil {                                   /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		value := listener.Addr().String() /* 更新 value 的值。 */
		listener.Close()                  /* 执行当前语句并推进处理流程。 */
		return value                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	apiAddr, gatewayAddr := address(), address() /* 更新 gatewayAddr 的值。 */
	root := t.TempDir()                          /* 更新 root 的值。 */
	baseEnv := []string{}                        /* 更新 baseEnv 的值。 */
	for _, entry := range os.Environ() {         /* 循环处理当前数据。 */
		if !strings.HasPrefix(entry, "IOT_") && !strings.HasPrefix(entry, "DEEPSEEK_") { /* 判断条件并选择处理分支。 */
			baseEnv = append(baseEnv, entry) /* 更新 baseEnv 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	baseEnv = append(baseEnv, "IOT_POSTGRES_DSN="+isolatedDSN, "IOT_KAFKA_BROKERS="+broker, "IOT_JWT_SECRET=isolated-split-process-jwt-key-32", "IOT_ADMIN_USER=test-admin", "IOT_ADMIN_PASSWORD=isolated-process-password", "IOT_ADMIN_TENANTS=t", "IOT_DATA_DIR="+root, "IOT_ACCESS_GATEWAY_URL=http://"+gatewayAddr) /* 更新 baseEnv 的值。 */
	start := func(role, addr string) func() {                                                                                                                                                                                                                                                                           /* 更新 start 的值。 */
		executable, e := os.Executable() /* 更新 e 的值。 */
		if e != nil {                    /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		command := exec.CommandContext(ctx, executable, "-test.run=^TestPlatformProcessHelper$")               /* 更新 command 的值。 */
		command.Env = append(append([]string{}, baseEnv...), "IOT_TEST_APP_ROLE="+role, "IOT_HTTP_ADDR="+addr) /* 更新 command.Env 的值。 */
		if role == "gateway" {                                                                                 /* 判断条件并选择处理分支。 */
			command.Env = append(command.Env, "IOT_AI_PROVIDER=invalid-unused-provider", "IOT_WEAVIATE_URL=http://127.0.0.1:1") /* 更新 command.Env 的值。 */
		} /* 结束当前表达式或代码块。 */
		log, e := os.OpenFile(filepath.Join(root, role+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600) /* 更新 e 的值。 */
		if e != nil {                                                                                      /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		command.Stdout, command.Stderr = log, log /* 更新 command.Stderr 的值。 */
		if e = command.Start(); e != nil {        /* 判断条件并选择处理分支。 */
			log.Close() /* 执行当前语句并推进处理流程。 */
			t.Fatal(e)  /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		stopped := false /* 更新 stopped 的值。 */
		stop := func() { /* 更新 stop 的值。 */
			if stopped { /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			stopped = true             /* 更新 stopped 的值。 */
			_ = command.Process.Kill() /* 更新 _ 的值。 */
			_ = command.Wait()         /* 更新 _ 的值。 */
			_ = log.Close()            /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		t.Cleanup(stop)                             /* 执行当前语句并推进处理流程。 */
		client := http.Client{Timeout: time.Second} /* 更新 client 的值。 */
		for i := 0; i < 100; i++ {                  /* 循环处理当前数据。 */
			response, e := client.Get("http://" + addr + "/health/live") /* 更新 e 的值。 */
			if e == nil {                                                /* 判断条件并选择处理分支。 */
				response.Body.Close()           /* 执行当前语句并推进处理流程。 */
				if response.StatusCode == 200 { /* 判断条件并选择处理分支。 */
					return stop /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			select { /* 根据条件选择处理路径。 */
			case <-ctx.Done(): /* 处理当前分支。 */
				t.Fatal("process startup deadline") /* 验证实际结果符合预期。 */
			case <-time.After(100 * time.Millisecond): /* 处理当前分支。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		t.Fatal("process startup failed; inspect isolated process log") /* 验证实际结果符合预期。 */
		return stop                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	stopGateway := start("gateway", gatewayAddr)                                                              /* 更新 stopGateway 的值。 */
	stopAPI := start("api", apiAddr)                                                                          /* 更新 stopAPI 的值。 */
	client := http.Client{Timeout: 15 * time.Second}                                                          /* 更新 client 的值。 */
	request := func(address, path, token string, body any, headers map[string]string, want int, output any) { /* 更新 request 的值。 */
		t.Helper()                       /* 执行当前语句并推进处理流程。 */
		encoded, e := json.Marshal(body) /* 更新 e 的值。 */
		if e != nil {                    /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		req, e := http.NewRequestWithContext(ctx, "POST", "http://"+address+path, bytes.NewReader(encoded)) /* 更新 e 的值。 */
		if e != nil {                                                                                       /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
		if token != "" {                                   /* 判断条件并选择处理分支。 */
			req.Header.Set("Authorization", "Bearer "+token) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		for key, value := range headers { /* 循环处理当前数据。 */
			req.Header.Set(key, value) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		response, e := client.Do(req) /* 更新 e 的值。 */
		if e != nil {                 /* 判断条件并选择处理分支。 */
			t.Fatalf("request failed: %s", path) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer response.Body.Close()      /* 安排函数结束时执行清理。 */
		if response.StatusCode != want { /* 判断条件并选择处理分支。 */
			t.Fatalf("%s status=%d want=%d", path, response.StatusCode, want) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if output != nil { /* 判断条件并选择处理分支。 */
			if e = json.NewDecoder(response.Body).Decode(output); e != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(e) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} else { /* 结束当前表达式或代码块。 */
			_, _ = io.Copy(io.Discard, response.Body) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	var login struct { /* 声明 login。 */
		AccessToken string `json:"accessToken"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	request(apiAddr, "/api/v1/auth/login", "", map[string]string{"username": "test-admin", "password": "wrong", "tenantId": "t"}, nil, 401, nil)                                                                                                           /* 执行当前语句并推进处理流程。 */
	request(apiAddr, "/api/v1/auth/login", "", map[string]string{"username": "test-admin", "password": "isolated-process-password", "tenantId": "t"}, nil, 200, &login)                                                                                    /* 执行当前语句并推进处理流程。 */
	request(gatewayAddr, "/api/v1/auth/login", "", map[string]string{}, nil, 404, nil)                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
	q := onboarding.Request{ProductID: "p", ProductName: "process product", DeviceID: "d", Name: "process device", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"preview","timestamp":1000,"data":{"temperature":42}}`)} /* 更新 q 的值。 */
	var preview connector.Result                                                                                                                                                                                                                           /* 声明 preview。 */
	request(apiAddr, "/api/v1/onboarding/test", login.AccessToken, q, nil, 200, &preview)                                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	if !preview.Success {                                                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal("onboarding preview failed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.TestToken = preview.TestToken                                                                                          /* 更新 q.TestToken 的值。 */
	var created onboarding.Result                                                                                            /* 声明 created。 */
	request(apiAddr, "/api/v1/onboarding", login.AccessToken, q, nil, 201, &created)                                         /* 执行当前语句并推进处理流程。 */
	headers := map[string]string{"X-Device-Key": created.Credential.AccessKey, "X-Device-Secret": created.Credential.Secret} /* 更新 headers 的值。 */
	path := "/api/v1/device-ingest/standard/t/p/d/property"                                                                  /* 更新 path 的值。 */
	request(apiAddr, path, "", map[string]any{"id": "bad", "data": map[string]int{"temperature": 42}}, nil, 401, nil)        /* 执行当前语句并推进处理流程。 */
	var receipt struct {                                                                                                     /* 声明 receipt。 */
		MessageID string `json:"messageId"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	request(apiAddr, path, "", map[string]any{"id": "first", "timestamp": time.Now().UnixMilli(), "data": map[string]int{"temperature": 42}}, headers, 202, &receipt) /* 执行当前语句并推进处理流程。 */
	repo, e := postgres.New(ctx, isolatedDSN)                                                                                                                         /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatal("open test repository") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	waitParsed := func(id string) { /* 更新 waitParsed 的值。 */
		t.Helper() /* 执行当前语句并推进处理流程。 */
		for {      /* 循环处理当前数据。 */
			m, e := repo.GetStandardMessageByRaw(ctx, "t", id)          /* 更新 e 的值。 */
			if e == nil && m.Properties["temperature"] == float64(42) { /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			select { /* 根据条件选择处理路径。 */
			case <-ctx.Done(): /* 处理当前分支。 */
				t.Fatal("Kafka parsing deadline") /* 验证实际结果符合预期。 */
			case <-time.After(100 * time.Millisecond): /* 处理当前分支。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	waitParsed(receipt.MessageID)                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	stopAPI()                                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	request(gatewayAddr, path, "", map[string]any{"id": "while-api-down", "timestamp": time.Now().UnixMilli(), "data": map[string]int{"temperature": 42}}, headers, 202, &receipt) /* 执行当前语句并推进处理流程。 */
	if _, err = repo.GetRawIndex(ctx, "t", receipt.MessageID); err != nil {                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal("gateway did not persist raw while API stopped") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	stopAPI = start("api", apiAddr)                                                                                                                                     /* 更新 stopAPI 的值。 */
	defer stopAPI()                                                                                                                                                     /* 安排函数结束时执行清理。 */
	waitParsed(receipt.MessageID)                                                                                                                                       /* 执行当前语句并推进处理流程。 */
	stopGateway()                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
	request(apiAddr, path, "", map[string]any{"id": "gateway-down", "timestamp": time.Now().UnixMilli(), "data": map[string]int{"temperature": 42}}, headers, 503, nil) /* 执行当前语句并推进处理流程。 */
	messages, count, e := repo.ListDeviceMessages(ctx, "t", "d", model.PropertyReport, 10, 0)                                                                           /* 更新 e 的值。 */
	if e != nil || count != 2 || len(messages) != 2 {                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal("unexpected message count", count, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
