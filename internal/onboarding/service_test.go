package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net"           /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/clickhouse"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"             /* 执行当前语句并推进处理流程。 */
	redisadapter "iot-platform/internal/adapters/redis" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"                   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"                       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"                      /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func fixture(t *testing.T) (*Service, *memory.Repository, Request) { /* 定义 fixture 函数。 */
	t.Helper()                                                                                                                          /* 执行当前语句并推进处理流程。 */
	repo := memory.NewRepository()                                                                                                      /* 更新 repo 的值。 */
	if err := repo.SaveProduct(context.Background(), model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return New(repo, parser.NewPlatformRegistry(t.TempDir()), t.TempDir(), []string{"127.0.0.0/8"}), repo, Request{ProductID: "product", DeviceID: "device", Name: "温度", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"1","timestamp":1788850000000,"data":{"temperature":26.5}}`)} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func tested(t *testing.T, s *Service, q Request) Request { /* 定义 tested 函数。 */
	t.Helper()                                          /* 执行当前语句并推进处理流程。 */
	r, err := s.Test(context.Background(), "tenant", q) /* 更新 err 的值。 */
	if err != nil {                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !r.Success || r.TestToken == "" { /* 判断条件并选择处理分支。 */
		t.Fatalf("test failed: %+v", r) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.TestToken = r.TestToken /* 更新 q.TestToken 的值。 */
	return q                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func TestCredentialAndAtomicCreation(t *testing.T) { /* 定义 TestCredentialAndAtomicCreation 函数。 */
	s, repo, q := fixture(t)                              /* 更新 q 的值。 */
	ctx := context.Background()                           /* 更新 ctx 的值。 */
	if _, err := s.Create(ctx, "tenant", q); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("untested request accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q = tested(t, s, q)                                         /* 更新 q 的值。 */
	changed := q                                                /* 更新 changed 的值。 */
	changed.Name = "changed"                                    /* 更新 changed.Name 的值。 */
	if _, err := s.Create(ctx, "tenant", changed); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("changed configuration accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	r, err := s.Create(ctx, "tenant", q) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	d, _ := repo.GetManagedDevice(ctx, "tenant", "device")                                /* 更新 _ 的值。 */
	if d.SecretHash == r.Credential.Secret || d.SecretHash != Hash(r.Credential.Secret) { /* 判断条件并选择处理分支。 */
		t.Fatal("secret storage invalid") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	data, _ := json.Marshal(d)                                                                               /* 更新 _ 的值。 */
	if strings.Contains(string(data), r.Credential.Secret) || strings.Contains(string(data), d.SecretHash) { /* 判断条件并选择处理分支。 */
		t.Fatal("credential leaked") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = s.Authenticate(ctx, r.Credential.AccessKey, r.Credential.Secret); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = s.Authenticate(ctx, r.Credential.AccessKey, "wrong"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("bad password accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if retry, err := s.Create(ctx, "tenant", q); err != nil || !retry.Reused || retry.Credential.Secret != "" { /* 判断条件并选择处理分支。 */
		t.Fatalf("retry must recover without a secret: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	after, _ := repo.GetManagedDevice(ctx, "tenant", "device") /* 更新 _ 的值。 */
	if after.SecretHash != d.SecretHash {                      /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate changed credential") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = s.Test(ctx, "other", q); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("cross-tenant product accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	d.SecretHash = ""                                                                                                  /* 更新 d.SecretHash 的值。 */
	_ = repo.SaveManagedDevice(ctx, d)                                                                                 /* 更新 _ 的值。 */
	if _, err = s.PrepareStandard(ctx, "tenant", "product", "device", "property", "MQTT", q.Payload); err != ErrAuth { /* 判断条件并选择处理分支。 */
		t.Fatalf("disabled credential MQTT: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestMQTTHealthAndNewProduct(t *testing.T) { /* 定义 TestMQTTHealthAndNewProduct 函数。 */
	s, repo, q := fixture(t)                                       /* 更新 q 的值。 */
	q.Type = connector.MQTT                                        /* 更新 q.Type 的值。 */
	q.ProductID = "new-product"                                    /* 更新 q.ProductID 的值。 */
	q.ProductName = "新产品"                                          /* 更新 q.ProductName 的值。 */
	r, err := s.Test(context.Background(), "tenant", q)            /* 更新 err 的值。 */
	if err != nil || r.Success || r.ErrorCode != "NETWORK_ERROR" { /* 判断条件并选择处理分支。 */
		t.Fatalf("missing broker reported healthy: %+v %v", r, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	s.MQTTHealth = func(context.Context) error { return nil }             /* 检查错误并决定后续处理。 */
	q = tested(t, s, q)                                                   /* 更新 q 的值。 */
	if _, err = s.Create(context.Background(), "tenant", q); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	p, err := repo.GetProduct(context.Background(), "tenant", "new-product") /* 更新 err 的值。 */
	if err != nil || p.Name != "新产品" || p.ProtocolPackageID == "" {          /* 判断条件并选择处理分支。 */
		t.Fatal("product not atomically created", p, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	pkg, err := repo.GetProtocolPackage(context.Background(), "tenant", p.ProtocolPackageID) /* 更新 err 的值。 */
	if err != nil || pkg.ParserType != parser.StandardParserName {                           /* 判断条件并选择处理分支。 */
		t.Fatal("legacy compatibility shim missing", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestOnboardingThroughProductionRepositoryDecorators(t *testing.T) { /* 定义 TestOnboardingThroughProductionRepositoryDecorators 函数。 */
	s, repo, q := fixture(t)                                /* 更新 q 的值。 */
	telemetry := &clickhouse.Repository{Repository: repo}   /* 更新 telemetry 的值。 */
	cache := redisadapter.New(telemetry, "127.0.0.1:1", "") /* 更新 cache 的值。 */
	// Onboarding must be forwarded without touching the external cache or
	// telemetry service. This is the same nesting as main's production wiring.
	s.Repo = cache                                                         /* 更新 s.Repo 的值。 */
	q = tested(t, s, q)                                                    /* 更新 q 的值。 */
	if _, err := s.Create(context.Background(), "tenant", q); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := repo.GetManagedDevice(context.Background(), "tenant", "device"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestStandardEnvelopeAndIdempotency(t *testing.T) { /* 定义 TestStandardEnvelopeAndIdempotency 函数。 */
	s, _, q := fixture(t)                                 /* 更新 q 的值。 */
	ctx := context.Background()                           /* 更新 ctx 的值。 */
	q = tested(t, s, q)                                   /* 更新 q 的值。 */
	if _, err := s.Create(ctx, "tenant", q); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	a, err := s.PrepareStandard(ctx, "tenant", "product", "device", "property", "HTTP", q.Payload) /* 更新 err 的值。 */
	if err != nil {                                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	b, _ := StandardRaw("tenant", "product", "device", "property", "MQTT", q.Payload)    /* 更新 _ 的值。 */
	other, _ := StandardRaw("tenant", "product", "other", "property", "HTTP", q.Payload) /* 更新 _ 的值。 */
	if a.MessageID != b.MessageID || a.MessageID == other.MessageID {                    /* 判断条件并选择处理分支。 */
		t.Fatal("idempotency not scoped") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if string(a.Payload) != string(q.Payload) { /* 判断条件并选择处理分支。 */
		t.Fatal("raw body changed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, kind := range []string{"property", "event", "state"} { /* 循环处理当前数据。 */
		raw, err := StandardRaw("tenant", "product", "device", kind, "MQTT", q.Payload) /* 更新 err 的值。 */
		if err != nil {                                                                 /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		m, err := s.Parsers.Parse(raw)                     /* 更新 err 的值。 */
		if err != nil || m.RawMessageID != raw.MessageID { /* 判断条件并选择处理分支。 */
			t.Fatalf("%s: %v", kind, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, payload := range []string{`null`, `{}`, `{"id":"1","timestamp":1,"data":null}`, `{"id":"1","timestamp":1,"data":[]}`, `{"id":"1","timestamp":1,"data":{"x":1}} trailing`} { /* 循环处理当前数据。 */
		if _, err := StandardRaw("tenant", "product", "device", "property", "HTTP", []byte(payload)); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("accepted %s", payload) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for i := 0; i < 19; i++ { /* 循环处理当前数据。 */
		if !s.Allow("tenant\x00device") { /* 判断条件并选择处理分支。 */
			t.Fatal("early rate limit") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if s.Allow("tenant\x00device") { /* 判断条件并选择处理分支。 */
		t.Fatal("rate limit missing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestModbusPreviewAndException(t *testing.T) { /* 定义 TestModbusPreviewAndException 函数。 */
	for _, exception := range []bool{false, true} { /* 循环处理当前数据。 */
		t.Run(map[bool]string{true: "exception", false: "success"}[exception], func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			s, repo, q := fixture(t)                    /* 更新 q 的值。 */
			ln, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
			if err != nil {                             /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			defer ln.Close()            /* 安排函数结束时执行清理。 */
			done := make(chan struct{}) /* 更新 done 的值。 */
			go func() {                 /* 执行当前语句并推进处理流程。 */
				defer close(done)   /* 安排函数结束时执行清理。 */
				c, e := ln.Accept() /* 更新 e 的值。 */
				if e != nil {       /* 判断条件并选择处理分支。 */
					return /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				defer c.Close()                           /* 安排函数结束时执行清理。 */
				req := make([]byte, 12)                   /* 更新 req 的值。 */
				if _, e = io.ReadFull(c, req); e != nil { /* 判断条件并选择处理分支。 */
					return /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				response := []byte{req[0], req[1], 0, 0, 0, 5, req[6], 3, 2, 0, 42} /* 更新 response 的值。 */
				if exception {                                                      /* 判断条件并选择处理分支。 */
					response = []byte{req[0], req[1], 0, 0, 0, 3, req[6], 0x83, 2} /* 更新 response 的值。 */
				} /* 结束当前表达式或代码块。 */
				_, _ = c.Write(response) /* 更新 _ 的值。 */
			}() /* 结束当前表达式或代码块。 */
			q.Type = connector.ModbusTCP                                                                                        /* 更新 q.Type 的值。 */
			q.Profile = model.DeviceAccessProfile{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, UnitID: 1}            /* 更新 q.Profile 的值。 */
			q.PointTableCSV = "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n" /* 更新 q.PointTableCSV 的值。 */
			r, err := s.Test(context.Background(), "tenant", q)                                                                 /* 更新 err 的值。 */
			if err != nil {                                                                                                     /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			<-done         /* 执行当前语句并推进处理流程。 */
			if exception { /* 判断条件并选择处理分支。 */
				if r.Success || r.ErrorCode != "PROTOCOL_ERROR" || r.ExceptionCode != 2 || r.RawRequest == "" || r.RawResponse == "" { /* 判断条件并选择处理分支。 */
					t.Fatalf("lost exception: %+v", r) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if !r.Success || len(r.StandardMessages) != 1 || r.RawRequest == "" { /* 判断条件并选择处理分支。 */
				t.Fatalf("preview: %+v", r) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if r.StandardMessages[0].Properties["temperature"] != uint64(42) { /* 判断条件并选择处理分支。 */
				data, _ := json.Marshal(r.StandardMessages[0].Properties) /* 更新 _ 的值。 */
				if string(data) != `{"temperature":42}` {                 /* 判断条件并选择处理分支。 */
					t.Fatalf("unexpected points %s", data) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			q.TestToken = r.TestToken                                             /* 更新 q.TestToken 的值。 */
			if _, err = s.Create(context.Background(), "tenant", q); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			profiles, _ := repo.ListDeviceAccessProfiles(context.Background(), "tenant")    /* 更新 _ 的值。 */
			if len(profiles) != 1 || !profiles[0].Enabled || profiles[0].ProtocolID == "" { /* 判断条件并选择处理分支。 */
				t.Fatal("runtime profile missing") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestModbusPollIntervalHasDistinctReleaseVersion(t *testing.T) { /* 定义 TestModbusPollIntervalHasDistinctReleaseVersion 函数。 */
	s, _, q := fixture(t)                                                                                               /* 更新 q 的值。 */
	q.Type = connector.ModbusTCP                                                                                        /* 更新 q.Type 的值。 */
	q.Profile = model.DeviceAccessProfile{Host: "127.0.0.1", Port: 502, UnitID: 1}                                      /* 更新 q.Profile 的值。 */
	q.PointTableCSV = "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n" /* 更新 q.PointTableCSV 的值。 */
	_, first, err := s.plan(context.Background(), "tenant", q)                                                          /* 更新 err 的值。 */
	if err != nil {                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.PollIntervalSec = 10                                          /* 更新 q.PollIntervalSec 的值。 */
	_, equivalent, err := s.plan(context.Background(), "tenant", q) /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if first.Version != equivalent.Version { /* 判断条件并选择处理分支。 */
		t.Fatal("default interval produced a different version") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.PollIntervalSec = 20                                       /* 更新 q.PollIntervalSec 的值。 */
	_, changed, err := s.plan(context.Background(), "tenant", q) /* 更新 err 的值。 */
	if err != nil {                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if first.Version == changed.Version { /* 判断条件并选择处理分支。 */
		t.Fatal("different polling configuration reused an immutable release") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
