package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"errors"                                /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"net"                                   /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestCentralRuntimeDoesNotExecuteEdgeProfiles(t *testing.T) { /* 定义 TestCentralRuntimeDoesNotExecuteEdgeProfiles 函数。 */
	ctx := context.Background()                                                                                                /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                                                                                             /* 更新 repo 的值。 */
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "edge-poll", EdgeNodeID: "remote", Mode: "poll", Enabled: true}     /* 更新 p 的值。 */
	_ = repo.SaveDeviceAccessProfile(ctx, p)                                                                                   /* 更新 _ 的值。 */
	r := New(repo, func(context.Context, model.RawMessage) error { t.Error("remote task executed locally"); return nil }, nil) /* 检查错误并决定后续处理。 */
	r.scan(ctx, time.Now())                                                                                                    /* 执行当前语句并推进处理流程。 */
	if len(r.running) != 0 || len(r.last) != 0 {                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal("remote task scheduled") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e := ReadModbusTCP(ctx, p, model.ProtocolRelease{}, nil); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("remote preview allowed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	listeners, store, _, listener := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil }) /* 检查错误并决定后续处理。 */
	listener.EdgeNodeID = "remote"                                                                                    /* 更新 listener.EdgeNodeID 的值。 */
	_ = store.SaveDeviceAccessProfile(ctx, listener)                                                                  /* 更新 _ 的值。 */
	listeners.reconcile(ctx)                                                                                          /* 执行当前语句并推进处理流程。 */
	if len(listeners.hosts) != 0 {                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal("listener kept running after reassignment") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestModbusCancellationInterruptsSilentDevice(t *testing.T) { /* 定义 TestModbusCancellationInterruptsSilentDevice 函数。 */
	listener, e := net.Listen("tcp", "127.0.0.1:0") /* 更新 e 的值。 */
	if e != nil {                                   /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer listener.Close()                                  /* 安排函数结束时执行清理。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	done := make(chan struct{})                             /* 更新 done 的值。 */
	go func() {                                             /* 执行当前语句并推进处理流程。 */
		defer close(done)         /* 安排函数结束时执行清理。 */
		c, e := listener.Accept() /* 更新 e 的值。 */
		if e != nil {             /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		defer c.Close()          /* 安排函数结束时执行清理。 */
		b := make([]byte, 12)    /* 更新 b 的值。 */
		_, _ = io.ReadFull(c, b) /* 更新 _ 的值。 */
		cancel()                 /* 执行当前语句并推进处理流程。 */
		_, _ = c.Read(b)         /* 更新 _ 的值。 */
	}() /* 结束当前表达式或代码块。 */
	p := model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 10000}                                                         /* 更新 p 的值。 */
	started := time.Now()                                                                                                                                                             /* 更新 started 的值。 */
	_, e = ReadModbusTCPWithPolicy(ctx, p, model.ProtocolRelease{Transport: "MODBUS_TCP"}, []model.ModbusReadBlock{{ID: "r", FunctionCode: 3, Quantity: 1}}, []string{"127.0.0.0/8"}) /* 更新 e 的值。 */
	if !errors.Is(e, context.Canceled) || time.Since(started) > time.Second {                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal("read ignored cancellation", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	<-done /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
