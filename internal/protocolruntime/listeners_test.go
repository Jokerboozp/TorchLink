package protocolruntime

import (
	"context"
	"errors"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
	"net"
	"strings"
	"testing"
	"time"
)

func listenerFixture(t *testing.T, ingest IngestFunc) (*Listeners, *memory.Repository, net.Conn, model.DeviceAccessProfile) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
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
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := r.Command(ctx, "tenant", "access", "device", map[string]any{"type": "test"})
		cancel()
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout %d: %v", i, err)
		}
		_, _ = io.ReadFull(conn, b[:])
		if b[0] != 0x22 {
			t.Fatalf("no command sent: %x", b)
		}
	}
}
