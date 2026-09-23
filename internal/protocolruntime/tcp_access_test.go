package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"encoding/hex"                          /* 执行当前语句并推进处理流程。 */
	"errors"                                /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/modbusframe"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker"  /* 执行当前语句并推进处理流程。 */
	"net"                                   /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestRTUOverTCPFragmentationAndCRC(t *testing.T) { /* 定义 TestRTUOverTCPFragmentationAndCRC 函数。 */
	for _, bad := range []bool{false, true} { /* 循环处理当前数据。 */
		t.Run(map[bool]string{false: "valid", true: "bad-crc"}[bad], func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			listener, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
			if err != nil {                                   /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			defer listener.Close()      /* 安排函数结束时执行清理。 */
			done := make(chan error, 1) /* 更新 done 的值。 */
			go func() {                 /* 执行当前语句并推进处理流程。 */
				c, e := listener.Accept() /* 更新 e 的值。 */
				if e != nil {             /* 判断条件并选择处理分支。 */
					done <- e /* 执行当前语句并推进处理流程。 */
					return    /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				defer c.Close()                            /* 安排函数结束时执行清理。 */
				c.SetDeadline(time.Now().Add(time.Second)) /* 执行当前语句并推进处理流程。 */
				q := make([]byte, 8)                       /* 更新 q 的值。 */
				if _, e = io.ReadFull(c, q); e != nil {    /* 判断条件并选择处理分支。 */
					done <- e /* 执行当前语句并推进处理流程。 */
					return    /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				if e = modbusframe.Validate(q); e != nil { /* 判断条件并选择处理分支。 */
					done <- e /* 执行当前语句并推进处理流程。 */
					return    /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				if q[0] != 7 || q[1] != 3 { /* 判断条件并选择处理分支。 */
					done <- errors.New("wrong query") /* 执行当前语句并推进处理流程。 */
					return                            /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				response := modbusframe.AppendCRC([]byte{7, 3, 2, 0, 42}) /* 更新 response 的值。 */
				if bad {                                                  /* 判断条件并选择处理分支。 */
					response[6] ^= 1 /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				for _, b := range response { /* 循环处理当前数据。 */
					if _, e = c.Write([]byte{b}); e != nil { /* 判断条件并选择处理分支。 */
						break /* 执行当前语句并推进处理流程。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
				done <- e /* 执行当前语句并推进处理流程。 */
			}() /* 结束当前表达式或代码块。 */
			p := model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 7, TimeoutMs: 1000, WireFormat: "rtu_over_tcp"}         /* 更新 p 的值。 */
			raws, err := ReadModbusTCP(context.Background(), p, model.ProtocolRelease{Transport: "MODBUS_RTU"}, []model.ModbusReadBlock{{FunctionCode: 3, Quantity: 1}}) /* 更新 err 的值。 */
			if bad {                                                                                                                                                     /* 判断条件并选择处理分支。 */
				if err == nil { /* 判断条件并选择处理分支。 */
					t.Fatal("accepted bad CRC") /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} else if err != nil || len(raws) != 1 || raws[0].Metadata["wireFormat"] != "rtu_over_tcp" { /* 结束当前表达式或代码块。 */
				t.Fatal(raws, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if e := <-done; e != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(e) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestModbusBusSerializesUnitsAndCancelsWaiter(t *testing.T) { /* 定义 TestModbusBusSerializesUnitsAndCancelsWaiter 函数。 */
	first, err := lockModbusBus(context.Background(), "endpoint") /* 更新 err 的值。 */
	if err != nil {                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)           /* 更新 cancel 的值。 */
	defer cancel()                                                                          /* 安排函数结束时执行清理。 */
	if _, err = lockModbusBus(ctx, "endpoint"); !errors.Is(err, context.DeadlineExceeded) { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	first()                                                        /* 执行当前语句并推进处理流程。 */
	unlock, err := lockModbusBus(context.Background(), "endpoint") /* 更新 err 的值。 */
	if err != nil {                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	unlock()                           /* 执行当前语句并推进处理流程。 */
	modbusBuses.Lock()                 /* 执行当前语句并推进处理流程。 */
	defer modbusBuses.Unlock()         /* 安排函数结束时执行清理。 */
	if len(modbusBuses.entries) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("bus locks leaked") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestTCPDialQueriesRegistrationAndReconnect(t *testing.T) { /* 定义 TestTCPDialQueriesRegistrationAndReconnect 函数。 */
	listener, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer listener.Close()                                                                                                                                                                                                                                                                                           /* 安排函数结束时执行清理。 */
	ctx, cancel := context.WithCancel(context.Background())                                                                                                                                                                                                                                                          /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                                                                                                                                                                                   /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                                                                                                                                                                                                                                                                                   /* 更新 repo 的值。 */
	repo.SaveProduct(ctx, model.Product{ID: "p", TenantID: "t", Status: "ENABLED"})                                                                                                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "d", TenantID: "t", ProductID: "p", Status: "ENABLED", AccessKey: "d"})                                                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	release := model.ProtocolRelease{TenantID: "t", ProtocolID: "proto", Version: "1", Transport: "TCP", Status: "PUBLISHED", ParserType: parser.GoProtocolParserName, Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: []string{"ingress", "decode", "encode"}}                           /* 更新 release 的值。 */
	repo.CreateProtocolRelease(ctx, release)                                                                                                                                                                                                                                                                         /* 执行当前语句并推进处理流程。 */
	repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "proto", Version: "1"})                                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	p := model.DeviceAccessProfile{ID: "dial", TenantID: "t", ProductID: "p", DeviceID: "d", Mode: "listener", Network: "tcp", ConnectionMode: "dial", Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Enabled: true, TimeoutMs: 1000, Queries: []model.ProtocolQuery{{Type: "read", IntervalSec: 1}}} /* 更新 p 的值。 */
	repo.SaveDeviceAccessProfile(ctx, p)                                                                                                                                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	raws := make(chan model.RawMessage, 8)                                                                                                                                                                                                                                                                           /* 更新 raws 的值。 */
	r := NewListeners(repo, "", func(_ context.Context, raw model.RawMessage) error { raws <- raw; return nil }, nil)                                                                                                                                                                                                /* 检查错误并决定后续处理。 */
	r.call = func(_ context.Context, _ string, _ model.ProtocolRelease, q protocolworker.Request) (protocolworker.Response, error) {                                                                                                                                                                                 /* 更新 r.call 的值。 */
		if q.Operation == "encode" { /* 判断条件并选择处理分支。 */
			return protocolworker.Response{Reply: "01", CorrelationID: "read"}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if q.Data != "02" { /* 判断条件并选择处理分支。 */
			return protocolworker.Response{}, errors.New("unexpected response") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return protocolworker.Response{Consumed: 1, DeviceID: "d", CorrelationID: "read"}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.reconcile(ctx)                                                         /* 执行当前语句并推进处理流程。 */
	defer r.stop()                                                           /* 安排函数结束时执行清理。 */
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second)) /* 执行当前语句并推进处理流程。 */
	for i := 0; i < 2; i++ {                                                 /* 循环处理当前数据。 */
		c, e := listener.Accept() /* 更新 e 的值。 */
		if e != nil {             /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		c.SetDeadline(time.Now().Add(2 * time.Second))       /* 执行当前语句并推进处理流程。 */
		b := make([]byte, 1)                                 /* 更新 b 的值。 */
		if _, e = io.ReadFull(c, b); e != nil || b[0] != 1 { /* 判断条件并选择处理分支。 */
			c.Close()                        /* 执行当前语句并推进处理流程。 */
			t.Fatal("no scheduled query", e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if _, e = r.Command(ctx, "t", p.ID, "d", map[string]any{"type": "manual"}); e == nil { /* 判断条件并选择处理分支。 */
			c.Close()                             /* 执行当前语句并推进处理流程。 */
			t.Fatal("overlapping query accepted") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		c.Write([]byte{2}) /* 执行当前语句并推进处理流程。 */
		select {           /* 根据条件选择处理路径。 */
		case raw := <-raws: /* 处理当前分支。 */
			if raw.DeviceID != "d" { /* 判断条件并选择处理分支。 */
				t.Fatal(raw) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		case <-time.After(time.Second): /* 处理当前分支。 */
			t.Fatal("no raw response") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		c.Close() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestTCPChildrenRegistrationAndSeparateProtocol(t *testing.T) { /* 定义 TestTCPChildrenRegistrationAndSeparateProtocol 函数。 */
	raws := make(chan model.RawMessage, 8)                                                                                                                      /* 更新 raws 的值。 */
	r, repo, conn, p := listenerFixture(t, func(_ context.Context, raw model.RawMessage) error { raws <- raw; return nil })                                     /* 检查错误并决定后续处理。 */
	ctx := context.Background()                                                                                                                                 /* 更新 ctx 的值。 */
	repo.SaveProduct(ctx, model.Product{TenantID: p.TenantID, ID: "sensor", Status: "ENABLED"})                                                                 /* 执行当前语句并推进处理流程。 */
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: p.TenantID, ProtocolID: "child", Version: "v1", Status: "PUBLISHED", PayloadFormat: "hex"}) /* 执行当前语句并推进处理流程。 */
	repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: p.TenantID, ProductID: "sensor", ProtocolID: "child", Version: "v1"})           /* 执行当前语句并推进处理流程。 */
	p.ChildProducts = []model.ChildProductBinding{{Type: "smoke", ProductID: "sensor"}}                                                                         /* 更新 p.ChildProducts 的值。 */
	repo.SaveDeviceAccessProfile(ctx, p)                                                                                                                        /* 执行当前语句并推进处理流程。 */
	r.reconcile(ctx)                                                                                                                                            /* 执行当前语句并推进处理流程。 */
	// Swap worker stub only before the first byte; the real socket and repositories run normally.
	r.call = func(_ context.Context, _ string, _ model.ProtocolRelease, q protocolworker.Request) (protocolworker.Response, error) { /* 更新 r.call 的值。 */
		if q.Data == "00" { /* 判断条件并选择处理分支。 */
			return protocolworker.Response{Consumed: 1, DeviceID: "device", Reply: "01"}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		data, _ := hex.DecodeString(q.Data) /* 更新 _ 的值。 */
		if len(data) != 1 {                 /* 判断条件并选择处理分支。 */
			return protocolworker.Response{}, errors.New("bad frame") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return protocolworker.Response{Consumed: 1, DeviceID: "device", Reply: "03", Children: []protocolworker.ChildFrame{{ChildIdentity: model.ChildIdentity{Address: "1", Type: "smoke", Name: "探测器"}, Payload: "002a"}}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	conn.SetDeadline(time.Now().Add(2 * time.Second))                /* 执行当前语句并推进处理流程。 */
	buf := make([]byte, 1)                                           /* 更新 buf 的值。 */
	conn.Write([]byte{0})                                            /* 执行当前语句并推进处理流程。 */
	if _, err := io.ReadFull(conn, buf); err != nil || buf[0] != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	<-raws                   /* 执行当前语句并推进处理流程。 */
	for i := 0; i < 2; i++ { /* 循环处理当前数据。 */
		conn.Write([]byte{2})                                            /* 执行当前语句并推进处理流程。 */
		if _, err := io.ReadFull(conn, buf); err != nil || buf[0] != 3 { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		parent, child := <-raws, <-raws                                                                                                                                                             /* 更新 child 的值。 */
		if child.GatewayID != "device" || child.ProductID != "sensor" || child.ProtocolID != "child" || child.ProtocolVersion != "v1" || child.Metadata["parentRawMessageId"] != parent.MessageID { /* 判断条件并选择处理分支。 */
			t.Fatal(child) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	devices, _ := repo.ListManagedDevices(ctx, p.TenantID) /* 更新 _ 的值。 */
	if len(devices) != 2 {                                 /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate child records", devices) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestTCPReconnectedIdentityReplacesOldSocket(t *testing.T) { /* 定义 TestTCPReconnectedIdentityReplacesOldSocket 函数。 */
	r, _, old, _ := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil }) /* 检查错误并决定后续处理。 */
	old.SetDeadline(time.Now().Add(2 * time.Second))                                                 /* 执行当前语句并推进处理流程。 */
	old.Write([]byte{0xaa, 0xbb})                                                                    /* 执行当前语句并推进处理流程。 */
	b := make([]byte, 1)                                                                             /* 更新 b 的值。 */
	if _, err := io.ReadFull(old, b); err != nil {                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	next, err := net.Dial("tcp", old.RemoteAddr().String()) /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer next.Close()                                /* 安排函数结束时执行清理。 */
	next.SetDeadline(time.Now().Add(2 * time.Second)) /* 执行当前语句并推进处理流程。 */
	next.Write([]byte{0xaa, 0xbb})                    /* 执行当前语句并推进处理流程。 */
	if _, err = io.ReadFull(next, b); err != nil {    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = old.Read(b); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("previous connection remains active") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	deadline := time.Now().Add(time.Second) /* 更新 deadline 的值。 */
	for time.Now().Before(deadline) {       /* 循环处理当前数据。 */
		if len(r.Sessions("tenant", "access")) == 1 { /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	t.Fatal("duplicate identified sessions") /* 验证实际结果符合预期。 */
} /* 结束当前表达式或代码块。 */
