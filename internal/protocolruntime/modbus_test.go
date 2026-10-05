package protocolruntime

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
)

type failedPollingBindingRepository struct {
	Store
	failure error
}

func (r failedPollingBindingRepository) GetProductProtocolBinding(context.Context, string, string) (model.ProductProtocolBinding, error) {
	return model.ProductProtocolBinding{}, r.failure
}

func TestPollingBindingSwitchFreezesInFlightRawVersion(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requests, responses := make(chan int, 2), make(chan struct{}, 2)
	go func() {
		for i := 0; i < 2; i++ {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			q := make([]byte, 12)
			if _, e = io.ReadFull(conn, q); e != nil {
				conn.Close()
				return
			}
			requests <- int(binary.BigEndian.Uint16(q[8:10]))
			<-responses
			_, _ = conn.Write([]byte{q[0], q[1], 0, 0, 0, 5, 1, 3, 2, 0, 42})
			conn.Close()
		}
	}()
	profile := model.DeviceAccessProfile{TenantID: "t", ID: "poll", ProductID: "p", DeviceID: "d", ProtocolID: "modbus", ProtocolVersion: "1", Mode: "poll", Network: "tcp", Enabled: true, Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 2000}
	if err = repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	for i, version := range []string{"1", "2"} {
		release := model.ProtocolRelease{TenantID: "t", ProtocolID: "modbus", Version: version, PointTableVersion: version, Transport: "MODBUS_TCP", Status: "PUBLISHED", Config: map[string]any{"blocks": []model.ModbusReadBlock{{ID: "read", FunctionCode: 3, StartAddress: i * 100, Quantity: 1, PollIntervalSec: 1}}}}
		if err = repo.CreateProtocolRelease(ctx, release); err != nil {
			t.Fatal(err)
		}
	}
	bind := func(version string) {
		t.Helper()
		if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "modbus", Version: version}); err != nil {
			t.Fatal(err)
		}
	}
	bind("1")
	raws := make(chan model.RawMessage, 2)
	runtime := New(repo, func(_ context.Context, raw model.RawMessage) error { raws <- raw; return nil }, nil)
	waitRequest := func(want int) {
		t.Helper()
		select {
		case address := <-requests:
			if address != want {
				t.Fatalf("address %d want %d", address, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("poll not scheduled")
		}
	}
	waitRaw := func(version string) model.RawMessage {
		t.Helper()
		select {
		case raw := <-raws:
			if raw.ProtocolVersion != version || raw.PointTableVersion != version {
				t.Fatal("incorrect archived version", raw)
			}
			if raw.Metadata["profileFingerprint"] != profile.ConfigurationFingerprint() {
				t.Fatal("missing actual connection snapshot")
			}
			return raw
		case <-time.After(3 * time.Second):
			t.Fatal("poll not archived")
			return model.RawMessage{}
		}
	}
	runtime.scan(ctx, time.Now())
	waitRequest(0)
	bind("2")
	responses <- struct{}{}
	first := waitRaw("1")
	deadline := time.Now().Add(time.Second)
	for {
		runtime.mu.Lock()
		busy := len(runtime.running) > 0
		runtime.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("poll did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	runtime.scan(ctx, time.Now().Add(2*time.Second))
	waitRequest(100)
	responses <- struct{}{}
	waitRaw("2")
	if first.ProtocolVersion != "1" || first.Metadata["startAddress"] != 0 {
		t.Fatal("historical frame snapshot changed", first)
	}
	bind("missing")
	if _, err = runtime.pollingRelease(ctx, profile); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("missing bound release fell back", err)
	}
	failure := errors.New("binding storage unavailable")
	failedRuntime := New(failedPollingBindingRepository{Store: repo, failure: failure}, nil, nil)
	if _, err = failedRuntime.pollingRelease(ctx, profile); !errors.Is(err, failure) {
		t.Fatal("binding read failed open", err)
	}
	legacyRuntime := New(failedPollingBindingRepository{Store: repo, failure: model.ErrNotFound}, nil, nil)
	legacy, err := legacyRuntime.pollingRelease(ctx, profile)
	if err != nil || legacy.Version != "1" {
		t.Fatal("legacy fixed polling removed", legacy, err)
	}
	if err = repo.UpdateProtocolReleaseStatus(ctx, "t", "modbus", "1", "REVOKED", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = legacyRuntime.pollingRelease(ctx, profile); err == nil {
		t.Fatal("revoked legacy release executed")
	}
}

func TestReadModbusTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		request := make([]byte, 12)
		if _, err = io.ReadFull(conn, request); err != nil {
			done <- err
			return
		}
		if request[7] != 3 || binary.BigEndian.Uint16(request[8:10]) != 100 || binary.BigEndian.Uint16(request[10:12]) != 2 {
			done <- io.ErrUnexpectedEOF
			return
		}
		response := []byte{request[0], request[1], 0, 0, 0, 7, 1, 3, 4, 0, 10, 0, 20}
		_, err = conn.Write(response)
		done <- err
	}()
	address := listener.Addr().(*net.TCPAddr)
	profile := model.DeviceAccessProfile{ID: "a", TenantID: "t", ProductID: "p", DeviceID: "d", Host: address.IP.String(), Port: address.Port, UnitID: 1, TimeoutMs: 1000}
	release := model.ProtocolRelease{ProtocolID: "modbus", Version: "1.0.0", PointTableVersion: "1.0.0", Transport: "MODBUS_TCP"}
	blocks := []model.ModbusReadBlock{{ID: "b", FunctionCode: 3, StartAddress: 100, Quantity: 2}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raws, err := ReadModbusTCPWithPolicy(ctx, profile, release, blocks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(raws) != 1 || raws[0].ProtocolVersion != "1.0.0" || raws[0].Metadata["startAddress"] != 100 {
		t.Fatalf("unexpected raw: %+v", raws)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReadModbusTCPPolicyRejectsOutsideNetwork(t *testing.T) {
	profile := model.DeviceAccessProfile{Host: "8.8.8.8", Port: 502, UnitID: 1, TimeoutMs: 100}
	release := model.ProtocolRelease{Transport: "MODBUS_TCP"}
	blocks := []model.ModbusReadBlock{{ID: "b", FunctionCode: 3, StartAddress: 0, Quantity: 1}}
	_, err := ReadModbusTCPWithPolicy(context.Background(), profile, release, blocks, []string{"10.0.0.0/8"})
	if err == nil {
		t.Fatal("expected target outside allowed CIDRs to be rejected before dialing")
	}
}

// A gateway must return exactly the requested register/coil data; a valid
// checksum alone does not prove a complete response to this polling request.
func TestModbusRejectsResponseQuantityMismatch(t *testing.T) {
	for _, wire := range []string{"modbus_tcp", "rtu_over_tcp"} {
		for _, tc := range []struct {
			name               string
			function, quantity int
			data               []byte
			valid              bool
		}{
			{"complete-registers", 3, 2, []byte{0, 1, 0, 2}, true},
			{"missing-register", 3, 2, []byte{0, 1}, false},
			{"extra-register", 3, 1, []byte{0, 1, 0, 2}, false},
			{"odd-register-bytes", 3, 1, []byte{1}, false},
			{"complete-coils", 1, 9, []byte{1, 1}, true},
			{"missing-coil-byte", 1, 9, []byte{1}, false},
		} {
			t.Run(wire+"/"+tc.name, func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				go func() {
					c, e := listener.Accept()
					if e != nil {
						return
					}
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(time.Second))
					n := 12
					if wire == "rtu_over_tcp" {
						n = 8
					}
					q := make([]byte, n)
					if _, e = io.ReadFull(c, q); e != nil {
						return
					}
					pdu := append([]byte{1, byte(tc.function), byte(len(tc.data))}, tc.data...)
					response := modbusframe.AppendCRC(pdu)
					if wire == "modbus_tcp" {
						response = append([]byte{q[0], q[1], 0, 0, 0, byte(len(pdu))}, pdu...)
					}
					_, _ = c.Write(response)
				}()
				p := model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500, WireFormat: wire}
				transport := "MODBUS_TCP"
				if wire == "rtu_over_tcp" {
					transport = "MODBUS_RTU"
				}
				raws, err := ReadModbusTCPWithPolicy(context.Background(), p, model.ProtocolRelease{Transport: transport}, []model.ModbusReadBlock{{FunctionCode: tc.function, StartAddress: 0x2000, Quantity: tc.quantity}}, nil)
				if tc.valid {
					if err != nil || len(raws) != 1 {
						t.Fatalf("valid response rejected: %v", err)
					}
				} else if err == nil {
					t.Fatalf("incomplete/mismatched response accepted as %d raw messages", len(raws))
				}
			})
		}
	}
}
