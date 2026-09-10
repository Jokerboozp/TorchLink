package onboarding

import (
	"context"
	"io"
	"iot-platform/internal/connector"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
	"net"
	"testing"
	"time"
)

func TestRTUOverTCPOnboardingUsesOriginalFrame(t *testing.T) {
	service, repo, q := fixture(t)
	socket, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	done := make(chan error, 1)
	go func() {
		c, e := socket.Accept()
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(time.Second))
		request := make([]byte, 8)
		if _, e = io.ReadFull(c, request); e == nil {
			e = modbusframe.Validate(request)
		}
		if e == nil {
			_, e = c.Write(modbusframe.AppendCRC([]byte{1, 3, 2, 0, 42}))
		}
		done <- e
	}()
	q.Type = connector.ModbusRTUTCP
	q.Profile = model.DeviceAccessProfile{Host: "127.0.0.1", Port: socket.Addr().(*net.TCPAddr).Port, UnitID: 1}
	q.PointTableCSV = "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n"
	preview, err := service.Test(context.Background(), "tenant", q)
	if err != nil || !preview.Success || len(preview.StandardMessages) != 1 {
		t.Fatal(preview, err)
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if preview.RawRequest != "010300000001840A" || len(preview.Raw) != 1 || preview.Raw[0].Transport != "MODBUS_RTU_TCP" {
		t.Fatalf("lost RTU wire: %+v", preview)
	}
	q.TestToken = preview.TestToken
	result, err := service.Create(context.Background(), "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetManagedDevice(context.Background(), "tenant", q.DeviceID)
	if err != nil || stored.SecretHash != "" || result.Credential.Secret != "" || result.Device.AccessKey != "" || result.Username != "" || result.ClientID != "" {
		t.Fatal("RTU over TCP should not generate platform credentials", err)
	}
	if result.Connector.Profile.WireFormat != "rtu_over_tcp" {
		t.Fatal(result)
	}
	binding, err := repo.GetProductProtocolBinding(context.Background(), "tenant", "product")
	if err != nil {
		t.Fatal(err)
	}
	release, err := repo.GetProtocolRelease(context.Background(), "tenant", binding.ProtocolID, binding.Version)
	if err != nil || release.Transport != "MODBUS_RTU" {
		t.Fatal(release, err)
	}
}
