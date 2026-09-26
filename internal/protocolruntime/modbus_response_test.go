package protocolruntime

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
)

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
				raws, err := ReadModbusTCP(context.Background(), p, model.ProtocolRelease{Transport: transport}, []model.ModbusReadBlock{{FunctionCode: tc.function, StartAddress: 0x2000, Quantity: tc.quantity}})
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
