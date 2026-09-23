package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                           /* 执行当前语句并推进处理流程。 */
	"io"                                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/modbusframe" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"       /* 执行当前语句并推进处理流程。 */
	"net"                               /* 执行当前语句并推进处理流程。 */
	"testing"                           /* 执行当前语句并推进处理流程。 */
	"time"                              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestRTUOverTCPOnboardingUsesOriginalFrame(t *testing.T) { /* 定义 TestRTUOverTCPOnboardingUsesOriginalFrame 函数。 */
	service, repo, q := fixture(t)                  /* 更新 q 的值。 */
	socket, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer socket.Close()        /* 安排函数结束时执行清理。 */
	done := make(chan error, 1) /* 更新 done 的值。 */
	go func() {                 /* 执行当前语句并推进处理流程。 */
		c, e := socket.Accept() /* 更新 e 的值。 */
		if e != nil {           /* 判断条件并选择处理分支。 */
			done <- e /* 执行当前语句并推进处理流程。 */
			return    /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		defer c.Close()                               /* 安排函数结束时执行清理。 */
		c.SetDeadline(time.Now().Add(time.Second))    /* 执行当前语句并推进处理流程。 */
		request := make([]byte, 8)                    /* 更新 request 的值。 */
		if _, e = io.ReadFull(c, request); e == nil { /* 判断条件并选择处理分支。 */
			e = modbusframe.Validate(request) /* 更新 e 的值。 */
		} /* 结束当前表达式或代码块。 */
		if e == nil { /* 判断条件并选择处理分支。 */
			_, e = c.Write(modbusframe.AppendCRC([]byte{1, 3, 2, 0, 42})) /* 更新 e 的值。 */
		} /* 结束当前表达式或代码块。 */
		done <- e /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	q.Type = connector.ModbusRTUTCP                                                                                     /* 更新 q.Type 的值。 */
	q.Profile = model.DeviceAccessProfile{Host: "127.0.0.1", Port: socket.Addr().(*net.TCPAddr).Port, UnitID: 1}        /* 更新 q.Profile 的值。 */
	q.PointTableCSV = "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n" /* 更新 q.PointTableCSV 的值。 */
	preview, err := service.Test(context.Background(), "tenant", q)                                                     /* 更新 err 的值。 */
	if err != nil || !preview.Success || len(preview.StandardMessages) != 1 {                                           /* 判断条件并选择处理分支。 */
		t.Fatal(preview, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e := <-done; e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if preview.RawRequest != "010300000001840A" || len(preview.Raw) != 1 || preview.Raw[0].Transport != "MODBUS_RTU_TCP" { /* 判断条件并选择处理分支。 */
		t.Fatalf("lost RTU wire: %+v", preview) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.TestToken = preview.TestToken                                  /* 更新 q.TestToken 的值。 */
	result, err := service.Create(context.Background(), "tenant", q) /* 更新 err 的值。 */
	if err != nil {                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	stored, err := repo.GetManagedDevice(context.Background(), "tenant", q.DeviceID)                                                                                /* 更新 err 的值。 */
	if err != nil || stored.SecretHash != "" || result.Credential.Secret != "" || result.Device.AccessKey != "" || result.Username != "" || result.ClientID != "" { /* 判断条件并选择处理分支。 */
		t.Fatal("RTU over TCP should not generate platform credentials", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if result.Connector.Profile.WireFormat != "rtu_over_tcp" { /* 判断条件并选择处理分支。 */
		t.Fatal(result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	binding, err := repo.GetProductProtocolBinding(context.Background(), "tenant", "product") /* 更新 err 的值。 */
	if err != nil {                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	release, err := repo.GetProtocolRelease(context.Background(), "tenant", binding.ProtocolID, binding.Version) /* 更新 err 的值。 */
	if err != nil || release.Transport != "MODBUS_RTU" {                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(release, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
