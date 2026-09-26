package protocolruntime

import (
	"context"
	"errors"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"net"
	"testing"
	"time"
)

func TestCentralRuntimeDoesNotExecuteEdgeProfiles(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "edge-poll", EdgeNodeID: "remote", Mode: "poll", Enabled: true}
	_ = repo.SaveDeviceAccessProfile(ctx, p)
	r := New(repo, func(context.Context, model.RawMessage) error { t.Error("remote task executed locally"); return nil }, nil)
	r.scan(ctx, time.Now())
	if len(r.running) != 0 || len(r.last) != 0 {
		t.Fatal("remote task scheduled")
	}
	if _, e := ReadModbusTCP(ctx, p, model.ProtocolRelease{}, nil); e == nil {
		t.Fatal("remote preview allowed")
	}
	listeners, store, _, listener := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil })
	listener.EdgeNodeID = "remote"
	_ = store.SaveDeviceAccessProfile(ctx, listener)
	listeners.reconcile(ctx)
	if len(listeners.hosts) != 0 {
		t.Fatal("listener kept running after reassignment")
	}
}
func TestModbusCancellationInterruptsSilentDevice(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		b := make([]byte, 12)
		_, _ = io.ReadFull(c, b)
		cancel()
		_, _ = c.Read(b)
	}()
	p := model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 10000}
	started := time.Now()
	_, e = ReadModbusTCPWithPolicy(ctx, p, model.ProtocolRelease{Transport: "MODBUS_TCP"}, []model.ModbusReadBlock{{ID: "r", FunctionCode: 3, Quantity: 1}}, []string{"127.0.0.0/8"})
	if !errors.Is(e, context.Canceled) || time.Since(started) > time.Second {
		t.Fatal("read ignored cancellation", e)
	}
	<-done
}
