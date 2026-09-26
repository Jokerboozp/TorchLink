package protocolruntime

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
	"net"
	"testing"
	"time"
)

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
