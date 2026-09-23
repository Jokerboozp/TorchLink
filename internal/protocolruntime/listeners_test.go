package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"errors"                                /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker"  /* 执行当前语句并推进处理流程。 */
	"net"                                   /* 执行当前语句并推进处理流程。 */
	"strings"                               /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func listenerFixture(t *testing.T, ingest IngestFunc) (*Listeners, *memory.Repository, net.Conn, model.DeviceAccessProfile) { /* 定义 listenerFixture 函数。 */
	t.Helper()                                                                                                                                                                                                                                                                                        /* 执行当前语句并推进处理流程。 */
	ctx := context.Background()                                                                                                                                                                                                                                                                       /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                                                                                                                                                                                                    /* 更新 repo 的值。 */
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED"})                                                                                                                                                                                                    /* 更新 _ 的值。 */
	release := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "package", Version: "1", Transport: "TCP_UDP", ParserType: parser.GoProtocolParserName, Status: "PUBLISHED", Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: []string{"decode", "ingress", "encode"}} /* 更新 release 的值。 */
	_ = repo.CreateProtocolRelease(ctx, release)                                                                                                                                                                                                                                                      /* 更新 _ 的值。 */
	release.Version = "2"                                                                                                                                                                                                                                                                             /* 更新 release.Version 的值。 */
	_ = repo.CreateProtocolRelease(ctx, release)                                                                                                                                                                                                                                                      /* 更新 _ 的值。 */
	_ = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "package", Version: "1"})                                                                                                                                             /* 更新 _ 的值。 */
	socket, err := net.Listen("tcp", "127.0.0.1:0")                                                                                                                                                                                                                                                   /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	port := socket.Addr().(*net.TCPAddr).Port                                                                                                                                                                   /* 更新 port 的值。 */
	address := socket.Addr().String()                                                                                                                                                                           /* 更新 address 的值。 */
	_ = socket.Close()                                                                                                                                                                                          /* 更新 _ 的值。 */
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "access", ProductID: "product", Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, Enabled: true, AutoRegister: true, TimeoutMs: 1000} /* 更新 p 的值。 */
	_ = repo.SaveDeviceAccessProfile(ctx, p)                                                                                                                                                                    /* 更新 _ 的值。 */
	r := NewListeners(repo, "", ingest, nil)                                                                                                                                                                    /* 更新 r 的值。 */
	r.call = func(_ context.Context, _ string, _ model.ProtocolRelease, in protocolworker.Request) (protocolworker.Response, error) {                                                                           /* 更新 r.call 的值。 */
		if in.Operation == "encode" { /* 判断条件并选择处理分支。 */
			return protocolworker.Response{Reply: "22", CorrelationID: "pending"}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(in.Data) < 4 { /* 判断条件并选择处理分支。 */
			return protocolworker.Response{NeedMore: true}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return protocolworker.Response{Consumed: 2, DeviceID: "device", Reply: "11"}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.reconcile(ctx)                      /* 执行当前语句并推进处理流程。 */
	t.Cleanup(r.stop)                     /* 执行当前语句并推进处理流程。 */
	conn, err := net.Dial("tcp", address) /* 更新 err 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	t.Cleanup(func() { _ = conn.Close() }) /* 执行当前语句并推进处理流程。 */
	return r, repo, conn, p                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestListenerPinsPartialFrameAndStopsPendingCommand(t *testing.T) { /* 定义 TestListenerPinsPartialFrameAndStopsPendingCommand 函数。 */
	raws := make(chan model.RawMessage, 8)                                                                                  /* 更新 raws 的值。 */
	r, repo, conn, p := listenerFixture(t, func(_ context.Context, raw model.RawMessage) error { raws <- raw; return nil }) /* 检查错误并决定后续处理。 */
	read := func(want byte) {                                                                                               /* 更新 read 的值。 */
		t.Helper()                                                               /* 执行当前语句并推进处理流程。 */
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))                /* 更新 _ 的值。 */
		var data [1]byte                                                         /* 声明 data。 */
		if _, err := io.ReadFull(conn, data[:]); err != nil || data[0] != want { /* 判断条件并选择处理分支。 */
			t.Fatalf("read=%x err=%v", data, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = conn.Write([]byte{0xaa}) /* 更新 _ 的值。 */
	// Observe the actual partial state before changing a binding.
	deadline := time.Now().Add(2 * time.Second) /* 更新 deadline 的值。 */
	partial := false                            /* 更新 partial 的值。 */
	for time.Now().Before(deadline) {           /* 循环处理当前数据。 */
		r.mu.Lock()                                   /* 执行当前语句并推进处理流程。 */
		h := r.hosts[listenerKey("tenant", "access")] /* 更新 h 的值。 */
		r.mu.Unlock()                                 /* 执行当前语句并推进处理流程。 */
		h.mu.Lock()                                   /* 执行当前语句并推进处理流程。 */
		var sessions []*listenerSession               /* 声明 sessions。 */
		for _, s := range h.sessions {                /* 循环处理当前数据。 */
			sessions = append(sessions, s) /* 更新 sessions 的值。 */
		} /* 结束当前表达式或代码块。 */
		h.mu.Unlock()                /* 执行当前语句并推进处理流程。 */
		for _, s := range sessions { /* 循环处理当前数据。 */
			s.mu.Lock()         /* 执行当前语句并推进处理流程。 */
			partial = s.partial /* 更新 partial 的值。 */
			s.mu.Unlock()       /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if partial { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if !partial { /* 判断条件并选择处理分支。 */
		t.Fatal("partial frame was not observed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_ = repo.SaveProductProtocolBinding(context.Background(), model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "package", Version: "2"}) /* 更新 _ 的值。 */
	_, _ = conn.Write([]byte{0xbb})                                                                                                                                        /* 更新 _ 的值。 */
	read(0x11)                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	if raw := <-raws; raw.ProtocolVersion != "1" {                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatalf("partial used version %s", raw.ProtocolVersion) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = conn.Write([]byte{0xaa, 0xbb})          /* 更新 _ 的值。 */
	read(0x11)                                     /* 执行当前语句并推进处理流程。 */
	if raw := <-raws; raw.ProtocolVersion != "2" { /* 判断条件并选择处理分支。 */
		t.Fatalf("next frame used version %s", raw.ProtocolVersion) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := r.Command(context.Background(), "other-tenant", "access", "device", map[string]any{"type": "test"}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("cross tenant command accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	done := make(chan error, 1) /* 更新 done 的值。 */
	go func() {                 /* 执行当前语句并推进处理流程。 */
		_, err := r.Command(context.Background(), "tenant", "access", "device", map[string]any{"type": "test"}) /* 更新 err 的值。 */
		done <- err                                                                                             /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	read(0x22)                                                /* 执行当前语句并推进处理流程。 */
	p.Enabled = false                                         /* 更新 p.Enabled 的值。 */
	_ = repo.SaveDeviceAccessProfile(context.Background(), p) /* 更新 _ 的值。 */
	r.reconcile(context.Background())                         /* 执行当前语句并推进处理流程。 */
	select {                                                  /* 根据条件选择处理路径。 */
	case err := <-done: /* 处理当前分支。 */
		if err == nil || !strings.Contains(err.Error(), "closed") { /* 判断条件并选择处理分支。 */
			t.Fatalf("pending command: %v", err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	case <-time.After(2 * time.Second): /* 处理当前分支。 */
		t.Fatal("pending command hung after disable") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestListenerDoesNotAcknowledgeFailedArchive(t *testing.T) { /* 定义 TestListenerDoesNotAcknowledgeFailedArchive 函数。 */
	_, _, conn, _ := listenerFixture(t, func(context.Context, model.RawMessage) error { return errors.New("archive unavailable") }) /* 更新 _ 的值。 */
	_, _ = conn.Write([]byte{0xaa, 0xbb})                                                                                           /* 更新 _ 的值。 */
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))                                                                       /* 更新 _ 的值。 */
	var buf [1]byte                                                                                                                 /* 声明 buf。 */
	if n, err := conn.Read(buf[:]); n != 0 || err == nil {                                                                          /* 判断条件并选择处理分支。 */
		t.Fatalf("failed ingest was acknowledged: %d %v", n, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestListenerCommandTimeoutClearsPending(t *testing.T) { /* 定义 TestListenerCommandTimeoutClearsPending 函数。 */
	r, _, conn, _ := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil }) /* 检查错误并决定后续处理。 */
	_, _ = conn.Write([]byte{0xaa, 0xbb})                                                             /* 更新 _ 的值。 */
	var b [1]byte                                                                                     /* 声明 b。 */
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))                                         /* 更新 _ 的值。 */
	_, _ = io.ReadFull(conn, b[:])                                                                    /* 更新 _ 的值。 */
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)                     /* 更新 cancel 的值。 */
	defer cancel()                                                                                    /* 安排函数结束时执行清理。 */
	_, err := r.Command(ctx, "tenant", "access", "device", map[string]any{"type": "test"})            /* 更新 err 的值。 */
	if !errors.Is(err, context.DeadlineExceeded) {                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("timeout: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = io.ReadFull(conn, b[:]); err != nil || b[0] != 0x22 { /* 判断条件并选择处理分支。 */
		t.Fatal("query was not sent", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = conn.Read(b[:]); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("timed out connection must close to isolate late responses") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = r.Command(context.Background(), "tenant", "access", "device", map[string]any{"type": "test"}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("reused timed out session") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

} /* 结束当前表达式或代码块。 */

func TestListenerStatusRetainsAcceptedFrameAfterDisconnect(t *testing.T) { /* 定义 TestListenerStatusRetainsAcceptedFrameAfterDisconnect 函数。 */
	r, _, connection, p := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil }) /* 检查错误并决定后续处理。 */
	status, _, last := r.Status(p.TenantID, p.ID)                                                           /* 更新 last 的值。 */
	if status != "LISTENING" || last != 0 {                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal("empty listener reported data", status, last) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	connection.SetDeadline(time.Now().Add(2 * time.Second))   /* 执行当前语句并推进处理流程。 */
	connection.Write([]byte{0xaa, 0xbb})                      /* 执行当前语句并推进处理流程。 */
	reply := make([]byte, 1)                                  /* 更新 reply 的值。 */
	if _, err := io.ReadFull(connection, reply); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	connection.Close()                      /* 执行当前语句并推进处理流程。 */
	deadline := time.Now().Add(time.Second) /* 更新 deadline 的值。 */
	for {                                   /* 循环处理当前数据。 */
		r.mu.Lock()                                 /* 执行当前语句并推进处理流程。 */
		h := r.hosts[listenerKey(p.TenantID, p.ID)] /* 更新 h 的值。 */
		r.mu.Unlock()                               /* 执行当前语句并推进处理流程。 */
		h.mu.Lock()                                 /* 执行当前语句并推进处理流程。 */
		closed := len(h.sessions) == 0              /* 更新 closed 的值。 */
		h.mu.Unlock()                               /* 执行当前语句并推进处理流程。 */
		if closed {                                 /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if time.Now().After(deadline) { /* 判断条件并选择处理分支。 */
			t.Fatal("session did not close") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	status, _, last = r.Status(p.TenantID, p.ID) /* 更新 last 的值。 */
	if status != "LISTENING" || last <= 0 {      /* 判断条件并选择处理分支。 */
		t.Fatal("disconnected successful frame lost", status, last) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	rejected, _, conn, q := listenerFixture(t, func(context.Context, model.RawMessage) error { return errors.New("archive unavailable") }) /* 更新 q 的值。 */
	conn.SetDeadline(time.Now().Add(time.Second))                                                                                          /* 执行当前语句并推进处理流程。 */
	conn.Write([]byte{0xaa, 0xbb})                                                                                                         /* 执行当前语句并推进处理流程。 */
	conn.Read(reply)                                                                                                                       /* 执行当前语句并推进处理流程。 */
	_, _, last = rejected.Status(q.TenantID, q.ID)                                                                                         /* 更新 last 的值。 */
	if last != 0 {                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal("rejected frame reported success", last) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
