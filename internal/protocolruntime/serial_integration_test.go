package protocolruntime

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/testserial"
	"testing"
)

func TestModbusRTUOSSerial(t *testing.T) {
	port := testserial.Start(t)
	p := model.DeviceAccessProfile{SerialPort: port, UnitID: 1, BaudRate: 9600, Parity: "N", StopBits: 1, TimeoutMs: 1000}
	raws, err := ReadModbusRTU(context.Background(), p, model.ProtocolRelease{Transport: "MODBUS_RTU"}, []model.ModbusReadBlock{{ID: "one", FunctionCode: 3, Quantity: 1}}, []string{port})
	if err != nil || len(raws) != 1 {
		t.Fatal("OS serial read", err)
	}
}
