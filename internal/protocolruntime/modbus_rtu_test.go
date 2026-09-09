package protocolruntime

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"net"
	"testing"
	"time"
)

type testRTUPort struct{ net.Conn }

func (testRTUPort) ResetInputBuffer() error { return nil }

func TestRTUReadAndParseWire(t *testing.T) {
	for _, test := range []string{"success", "crc", "unit", "function", "exception", "length", "timeout"} {
		t.Run(test, func(t *testing.T) {
			client, device := net.Pipe()
			defer client.Close()
			defer device.Close()
			p := model.DeviceAccessProfile{TenantID: "t", ProductID: "p", DeviceID: "d", ID: "a", SerialPort: "test", UnitID: 1, BaudRate: 9600, TimeoutMs: 100, Parity: "E", StopBits: 1}
			release := model.ProtocolRelease{Transport: "MODBUS_RTU", ProtocolID: "rtu", Version: "1"}
			block := model.ModbusReadBlock{FunctionCode: 3, Quantity: 1, ID: "b"}
			done := make(chan struct{})
			go func() {
				defer close(done)
				request := make([]byte, 8)
				if _, err := io.ReadFull(device, request); err != nil {
					return
				}
				if modbusframe.Validate(request) != nil || !bytes.Equal(request[:6], []byte{1, 3, 0, 0, 0, 1}) {
					t.Error("invalid RTU request")
				}
				response := []byte{1, 3, 2, 0, 42}
				switch test {
				case "unit":
					response[0] = 2
				case "function":
					response[1] = 4
				case "exception":
					response = []byte{1, 0x83, 2}
				case "length":
					response[2] = 4
				case "timeout":
					<-time.After(200 * time.Millisecond)
					return
				}
				response = modbusframe.AppendCRC(response)
				if test == "crc" {
					response[len(response)-1] ^= 1
				}
				_, _ = device.Write(response)
			}()
			raws, err := readRTU(context.Background(), p, release, []model.ModbusReadBlock{block}, testRTUPort{client})
			client.Close()
			device.Close()
			<-done
			if test != "success" {
				if err == nil {
					t.Fatal("invalid response accepted")
				}
				if test == "exception" {
					var e *ModbusException
					if !errors.As(err, &e) || e.Code != 2 {
						t.Fatal(err)
					}
				}
				return
			}
			if err != nil || len(raws) != 1 {
				t.Fatal(err)
			}
			m, err := (parser.ModbusRTUParser{}).ParseWithConfig(raws[0], map[string]any{"points": []model.ModbusPoint{{Identifier: "temperature", Address: 0, FunctionCode: 3, DataType: "uint16", RegisterCount: 1}}})
			if err != nil || m.Properties["temperature"] != float64(42) {
				t.Fatal(m, err)
			}
			if raws[0].Transport != "MODBUS_RTU" || m.Raw["payload"] != string(raws[0].Payload) {
				t.Fatal("original RTU evidence lost")
			}
		})
	}
}
func TestRTUCRCReferenceAndPortPolicy(t *testing.T) {
	data, _ := hex.DecodeString("01030000000AC5CD")
	if err := modbusframe.Validate(data); err != nil {
		t.Fatal(err)
	}
	p := model.DeviceAccessProfile{SerialPort: "/etc/passwd", BaudRate: 9600, Parity: "E", StopBits: 1, UnitID: 1, TimeoutMs: 100}
	if _, err := ReadModbusRTU(context.Background(), p, model.ProtocolRelease{Transport: "MODBUS_RTU"}, []model.ModbusReadBlock{{FunctionCode: 3, Quantity: 1}}, []string{"/dev/ttyUSB0"}); err == nil {
		t.Fatal("local allowlist bypass")
	}
}
