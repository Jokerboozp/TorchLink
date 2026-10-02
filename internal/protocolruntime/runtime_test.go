package protocolruntime

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
)

func TestConnectionCountsAcrossListeners(t *testing.T) {
	r := &Listeners{}
	var got []bool
	r.SetConnectionReporter(func(_ context.Context, tenant, product, device string, connected bool, at int64) error {
		got = append(got, connected)
		return nil
	})
	p := model.DeviceAccessProfile{TenantID: "t", ProductID: "p"}
	r.reportConnection(p, "d", true)
	r.reportConnection(p, "d", true)
	r.reportConnection(p, "d", false)
	if !reflect.DeepEqual(got, []bool{true}) {
		t.Fatal(got)
	}
	r.reportConnection(p, "d", false)
	r.reportConnection(p, "d", false)
	if !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatal(got)
	}
}

func TestCoordinatorLossCancelsOldExecution(t *testing.T) {
	repo := memory.NewRepository()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", Enabled: true}
	if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	first := NewCoordinator(repo, "first", "http://first")
	second := NewCoordinator(repo, "second", "http://second")
	done := make(chan struct{})
	go func() { first.Run(ctx); close(done) }()
	execution, ok := first.Claim(ctx, p)
	if !ok {
		t.Fatal("first owner rejected")
	}
	if _, ok := second.Claim(ctx, p); ok {
		t.Fatal("duplicate executor")
	}
	if err := first.Validate(execution, p); err != nil {
		t.Fatal(err)
	}
	lease, err := repo.GetExecutionLease(ctx, p.TenantID, "profile/"+p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ReleaseExecutionLease(ctx, lease); err != nil {
		t.Fatal(err)
	}
	next, ok := second.Claim(ctx, p)
	if !ok {
		t.Fatal("takeover rejected")
	}
	select {
	case <-execution.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("old execution survived lease loss")
	}
	if err := first.Validate(execution, p); err == nil {
		t.Fatal("old execution may ingest")
	}
	if err := second.Validate(next, p); err != nil {
		t.Fatal("new execution rejected", err)
	}
	p.Enabled = false
	if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := second.Validate(next, p); err == nil {
		t.Fatal("disabled profile may ingest")
	}
	cancel()
	<-done
	second.mu.Lock()
	for _, h := range second.held {
		h.timer.Stop()
		h.cancel()
	}
	second.mu.Unlock()
}

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

func listenerFixture(t *testing.T, ingest IngestFunc, configure ...func(*Listeners)) (*Listeners, *memory.Repository, net.Conn, model.DeviceAccessProfile) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED"})
	release := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "package", Version: "1", Transport: "TCP_UDP", ParserType: parser.GoProtocolParserName, Status: "PUBLISHED", Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: []string{"decode", "ingress", "encode"}}
	_ = repo.CreateProtocolRelease(ctx, release)
	release.Version = "2"
	_ = repo.CreateProtocolRelease(ctx, release)
	_ = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "package", Version: "1"})
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.Addr().(*net.TCPAddr).Port
	address := socket.Addr().String()
	_ = socket.Close()
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "access", ProductID: "product", Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, Enabled: true, AutoRegister: true, TimeoutMs: 1000}
	_ = repo.SaveDeviceAccessProfile(ctx, p)
	r := NewListeners(repo, "", ingest, nil)
	for _, configure := range configure {
		configure(r)
	}
	r.call = func(_ context.Context, _ string, _ model.ProtocolRelease, in protocolworker.Request) (protocolworker.Response, error) {
		if in.Operation == "encode" {
			return protocolworker.Response{Reply: "22", CorrelationID: "pending"}, nil
		}
		if len(in.Data) < 4 {
			return protocolworker.Response{NeedMore: true}, nil
		}
		return protocolworker.Response{Consumed: 2, DeviceID: "device", Reply: "11"}, nil
	}
	r.reconcile(ctx)
	t.Cleanup(r.stop)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return r, repo, conn, p
}

func TestListenerPinsPartialFrameAndStopsPendingCommand(t *testing.T) {
	raws := make(chan model.RawMessage, 8)
	r, repo, conn, p := listenerFixture(t, func(_ context.Context, raw model.RawMessage) error { raws <- raw; return nil })
	read := func(want byte) {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var data [1]byte
		if _, err := io.ReadFull(conn, data[:]); err != nil || data[0] != want {
			t.Fatalf("read=%x err=%v", data, err)
		}
	}
	_, _ = conn.Write([]byte{0xaa})
	// Observe the actual partial state before changing a binding.
	deadline := time.Now().Add(2 * time.Second)
	partial := false
	for time.Now().Before(deadline) {
		r.mu.Lock()
		h := r.hosts[listenerKey("tenant", "access")]
		r.mu.Unlock()
		h.mu.Lock()
		var sessions []*listenerSession
		for _, s := range h.sessions {
			sessions = append(sessions, s)
		}
		h.mu.Unlock()
		for _, s := range sessions {
			s.mu.Lock()
			partial = s.partial
			s.mu.Unlock()
		}
		if partial {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !partial {
		t.Fatal("partial frame was not observed")
	}
	_ = repo.SaveProductProtocolBinding(context.Background(), model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "package", Version: "2"})
	_, _ = conn.Write([]byte{0xbb})
	read(0x11)
	if raw := <-raws; raw.ProtocolVersion != "1" {
		t.Fatalf("partial used version %s", raw.ProtocolVersion)
	}
	_, _ = conn.Write([]byte{0xaa, 0xbb})
	read(0x11)
	if raw := <-raws; raw.ProtocolVersion != "2" {
		t.Fatalf("next frame used version %s", raw.ProtocolVersion)
	}
	if _, err := r.Command(context.Background(), "other-tenant", "access", "device", map[string]any{"type": "test"}); err == nil {
		t.Fatal("cross tenant command accepted")
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.Command(context.Background(), "tenant", "access", "device", map[string]any{"type": "test"})
		done <- err
	}()
	read(0x22)
	p.Enabled = false
	_ = repo.SaveDeviceAccessProfile(context.Background(), p)
	r.reconcile(context.Background())
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "closed") {
			t.Fatalf("pending command: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending command hung after disable")
	}
}

func TestListenerDoesNotAcknowledgeFailedArchive(t *testing.T) {
	_, _, conn, _ := listenerFixture(t, func(context.Context, model.RawMessage) error { return errors.New("archive unavailable") })
	_, _ = conn.Write([]byte{0xaa, 0xbb})
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var buf [1]byte
	if n, err := conn.Read(buf[:]); n != 0 || err == nil {
		t.Fatalf("failed ingest was acknowledged: %d %v", n, err)
	}
}

func TestListenerCommandTimeoutClearsPending(t *testing.T) {
	r, _, conn, _ := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil })
	_, _ = conn.Write([]byte{0xaa, 0xbb})
	var b [1]byte
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = io.ReadFull(conn, b[:])
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := r.Command(ctx, "tenant", "access", "device", map[string]any{"type": "test"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	if _, err = io.ReadFull(conn, b[:]); err != nil || b[0] != 0x22 {
		t.Fatal("query was not sent", err)
	}
	if _, err = conn.Read(b[:]); err == nil {
		t.Fatal("timed out connection must close to isolate late responses")
	}
	if _, err = r.Command(context.Background(), "tenant", "access", "device", map[string]any{"type": "test"}); err == nil {
		t.Fatal("reused timed out session")
	}

}

func TestListenerStatusRetainsAcceptedFrameAfterDisconnect(t *testing.T) {
	r, _, connection, p := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil })
	status, _, last := r.Status(p.TenantID, p.ID)
	if status != "LISTENING" || last != 0 {
		t.Fatal("empty listener reported data", status, last)
	}
	connection.SetDeadline(time.Now().Add(2 * time.Second))
	connection.Write([]byte{0xaa, 0xbb})
	reply := make([]byte, 1)
	if _, err := io.ReadFull(connection, reply); err != nil {
		t.Fatal(err)
	}
	connection.Close()
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		h := r.hosts[listenerKey(p.TenantID, p.ID)]
		r.mu.Unlock()
		h.mu.Lock()
		closed := len(h.sessions) == 0
		h.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session did not close")
		}
		time.Sleep(time.Millisecond)
	}
	status, _, last = r.Status(p.TenantID, p.ID)
	if status != "LISTENING" || last <= 0 {
		t.Fatal("disconnected successful frame lost", status, last)
	}
	rejected, _, conn, q := listenerFixture(t, func(context.Context, model.RawMessage) error { return errors.New("archive unavailable") })
	conn.SetDeadline(time.Now().Add(time.Second))
	conn.Write([]byte{0xaa, 0xbb})
	conn.Read(reply)
	_, _, last = rejected.Status(q.TenantID, q.ID)
	if last != 0 {
		t.Fatal("rejected frame reported success", last)
	}
}

// Peers beyond the session limit are closed and counted instead of vanishing.
func TestListenerCountsPeersRefusedAtSessionLimit(t *testing.T) {
	r, _, first, p := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil }, func(r *Listeners) { r.SetMaxSessions(1) })
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		h := r.hosts[listenerKey(p.TenantID, p.ID)]
		r.mu.Unlock()
		h.mu.Lock()
		n := len(h.sessions)
		h.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first session not registered")
		}
		time.Sleep(time.Millisecond)
	}
	second, err := net.Dial("tcp", first.RemoteAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err = second.Read(make([]byte, 1)); err == nil {
		t.Fatal("a peer beyond the limit must be closed")
	}
	if got := r.RejectedSessions(); got != 1 {
		t.Fatalf("refused peers must be counted, got %d", got)
	}
}

func TestRTUOverTCPFragmentationAndCRC(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "bad-crc"}[bad], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				c, e := listener.Accept()
				if e != nil {
					done <- e
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				q := make([]byte, 8)
				if _, e = io.ReadFull(c, q); e != nil {
					done <- e
					return
				}
				if e = modbusframe.Validate(q); e != nil {
					done <- e
					return
				}
				if q[0] != 7 || q[1] != 3 {
					done <- errors.New("wrong query")
					return
				}
				response := modbusframe.AppendCRC([]byte{7, 3, 2, 0, 42})
				if bad {
					response[6] ^= 1
				}
				for _, b := range response {
					if _, e = c.Write([]byte{b}); e != nil {
						break
					}
				}
				done <- e
			}()
			p := model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 7, TimeoutMs: 1000, WireFormat: "rtu_over_tcp"}
			raws, err := ReadModbusTCP(context.Background(), p, model.ProtocolRelease{Transport: "MODBUS_RTU"}, []model.ModbusReadBlock{{FunctionCode: 3, Quantity: 1}})
			if bad {
				if err == nil {
					t.Fatal("accepted bad CRC")
				}
			} else if err != nil || len(raws) != 1 || raws[0].Metadata["wireFormat"] != "rtu_over_tcp" {
				t.Fatal(raws, err)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestModbusBusSerializesUnitsAndCancelsWaiter(t *testing.T) {
	first, err := lockModbusBus(context.Background(), "endpoint")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = lockModbusBus(ctx, "endpoint"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	first()
	unlock, err := lockModbusBus(context.Background(), "endpoint")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	modbusBuses.Lock()
	defer modbusBuses.Unlock()
	if len(modbusBuses.entries) != 0 {
		t.Fatal("bus locks leaked")
	}
}

func TestTCPDialQueriesRegistrationAndReconnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	repo.SaveProduct(ctx, model.Product{ID: "p", TenantID: "t", Status: "ENABLED"})
	repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "d", TenantID: "t", ProductID: "p", Status: "ENABLED", AccessKey: "d"})
	release := model.ProtocolRelease{TenantID: "t", ProtocolID: "proto", Version: "1", Transport: "TCP", Status: "PUBLISHED", ParserType: parser.GoProtocolParserName, Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: []string{"ingress", "decode", "encode"}}
	repo.CreateProtocolRelease(ctx, release)
	repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "proto", Version: "1"})
	p := model.DeviceAccessProfile{ID: "dial", TenantID: "t", ProductID: "p", DeviceID: "d", Mode: "listener", Network: "tcp", ConnectionMode: "dial", Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Enabled: true, TimeoutMs: 1000, Queries: []model.ProtocolQuery{{Type: "read", IntervalSec: 1}}}
	repo.SaveDeviceAccessProfile(ctx, p)
	raws := make(chan model.RawMessage, 8)
	r := NewListeners(repo, "", func(_ context.Context, raw model.RawMessage) error { raws <- raw; return nil }, nil)
	r.call = func(_ context.Context, _ string, _ model.ProtocolRelease, q protocolworker.Request) (protocolworker.Response, error) {
		if q.Operation == "encode" {
			return protocolworker.Response{Reply: "01", CorrelationID: "read"}, nil
		}
		if q.Data != "02" {
			return protocolworker.Response{}, errors.New("unexpected response")
		}
		return protocolworker.Response{Consumed: 1, DeviceID: "d", CorrelationID: "read"}, nil
	}
	r.reconcile(ctx)
	defer r.stop()
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	for i := 0; i < 2; i++ {
		c, e := listener.Accept()
		if e != nil {
			t.Fatal(e)
		}
		c.SetDeadline(time.Now().Add(2 * time.Second))
		b := make([]byte, 1)
		if _, e = io.ReadFull(c, b); e != nil || b[0] != 1 {
			c.Close()
			t.Fatal("no scheduled query", e)
		}
		if _, e = r.Command(ctx, "t", p.ID, "d", map[string]any{"type": "manual"}); e == nil {
			c.Close()
			t.Fatal("overlapping query accepted")
		}
		c.Write([]byte{2})
		select {
		case raw := <-raws:
			if raw.DeviceID != "d" {
				t.Fatal(raw)
			}
		case <-time.After(time.Second):
			t.Fatal("no raw response")
		}
		c.Close()
	}
}

func TestTCPChildrenRegistrationAndSeparateProtocol(t *testing.T) {
	raws := make(chan model.RawMessage, 8)
	r, repo, conn, p := listenerFixture(t, func(_ context.Context, raw model.RawMessage) error { raws <- raw; return nil })
	ctx := context.Background()
	repo.SaveProduct(ctx, model.Product{TenantID: p.TenantID, ID: "sensor", Status: "ENABLED"})
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: p.TenantID, ProtocolID: "child", Version: "v1", Status: "PUBLISHED", PayloadFormat: "hex"})
	repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: p.TenantID, ProductID: "sensor", ProtocolID: "child", Version: "v1"})
	p.ChildProducts = []model.ChildProductBinding{{Type: "smoke", ProductID: "sensor"}}
	repo.SaveDeviceAccessProfile(ctx, p)
	r.reconcile(ctx)
	// Swap worker stub only before the first byte; the real socket and repositories run normally.
	r.call = func(_ context.Context, _ string, _ model.ProtocolRelease, q protocolworker.Request) (protocolworker.Response, error) {
		if q.Data == "00" {
			return protocolworker.Response{Consumed: 1, DeviceID: "device", Reply: "01"}, nil
		}
		data, _ := hex.DecodeString(q.Data)
		if len(data) != 1 {
			return protocolworker.Response{}, errors.New("bad frame")
		}
		return protocolworker.Response{Consumed: 1, DeviceID: "device", Reply: "03", Children: []protocolworker.ChildFrame{{ChildIdentity: model.ChildIdentity{Address: "1", Type: "smoke", Name: "探测器"}, Payload: "002a"}}}, nil
	}
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	conn.Write([]byte{0})
	if _, err := io.ReadFull(conn, buf); err != nil || buf[0] != 1 {
		t.Fatal(err)
	}
	<-raws
	for i := 0; i < 2; i++ {
		conn.Write([]byte{2})
		if _, err := io.ReadFull(conn, buf); err != nil || buf[0] != 3 {
			t.Fatal(err)
		}
		parent, child := <-raws, <-raws
		if child.GatewayID != "device" || child.ProductID != "sensor" || child.ProtocolID != "child" || child.ProtocolVersion != "v1" || child.Metadata["parentRawMessageId"] != parent.MessageID {
			t.Fatal(child)
		}
	}
	devices, _ := repo.ListManagedDevices(ctx, p.TenantID)
	if len(devices) != 2 {
		t.Fatal("duplicate child records", devices)
	}
}

func TestTCPReconnectedIdentityReplacesOldSocket(t *testing.T) {
	r, _, old, _ := listenerFixture(t, func(context.Context, model.RawMessage) error { return nil })
	old.SetDeadline(time.Now().Add(2 * time.Second))
	old.Write([]byte{0xaa, 0xbb})
	b := make([]byte, 1)
	if _, err := io.ReadFull(old, b); err != nil {
		t.Fatal(err)
	}
	next, err := net.Dial("tcp", old.RemoteAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	next.SetDeadline(time.Now().Add(2 * time.Second))
	next.Write([]byte{0xaa, 0xbb})
	if _, err = io.ReadFull(next, b); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Read(b); err == nil {
		t.Fatal("previous connection remains active")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(r.Sessions("tenant", "access")) == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("duplicate identified sessions")
}
