package protocolruntime

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"iot-platform/internal/model"
)

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
	raws, err := ReadModbusTCP(ctx, profile, release, blocks)
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
