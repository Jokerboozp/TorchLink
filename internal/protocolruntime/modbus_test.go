package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"         /* 执行当前语句并推进处理流程。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"io"              /* 执行当前语句并推进处理流程。 */
	"net"             /* 执行当前语句并推进处理流程。 */
	"testing"         /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestReadModbusTCP(t *testing.T) { /* 定义 TestReadModbusTCP 函数。 */
	listener, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer listener.Close()      /* 安排函数结束时执行清理。 */
	done := make(chan error, 1) /* 更新 done 的值。 */
	go func() {                 /* 执行当前语句并推进处理流程。 */
		conn, err := listener.Accept() /* 更新 err 的值。 */
		if err != nil {                /* 判断条件并选择处理分支。 */
			done <- err /* 执行当前语句并推进处理流程。 */
			return      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		defer conn.Close()                                   /* 安排函数结束时执行清理。 */
		request := make([]byte, 12)                          /* 更新 request 的值。 */
		if _, err = io.ReadFull(conn, request); err != nil { /* 判断条件并选择处理分支。 */
			done <- err /* 执行当前语句并推进处理流程。 */
			return      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if request[7] != 3 || binary.BigEndian.Uint16(request[8:10]) != 100 || binary.BigEndian.Uint16(request[10:12]) != 2 { /* 判断条件并选择处理分支。 */
			done <- io.ErrUnexpectedEOF /* 执行当前语句并推进处理流程。 */
			return                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		response := []byte{request[0], request[1], 0, 0, 0, 7, 1, 3, 4, 0, 10, 0, 20} /* 更新 response 的值。 */
		_, err = conn.Write(response)                                                 /* 更新 err 的值。 */
		done <- err                                                                   /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
	address := listener.Addr().(*net.TCPAddr)                                                                                                                              /* 更新 address 的值。 */
	profile := model.DeviceAccessProfile{ID: "a", TenantID: "t", ProductID: "p", DeviceID: "d", Host: address.IP.String(), Port: address.Port, UnitID: 1, TimeoutMs: 1000} /* 更新 profile 的值。 */
	release := model.ProtocolRelease{ProtocolID: "modbus", Version: "1.0.0", PointTableVersion: "1.0.0", Transport: "MODBUS_TCP"}                                          /* 更新 release 的值。 */
	blocks := []model.ModbusReadBlock{{ID: "b", FunctionCode: 3, StartAddress: 100, Quantity: 2}}                                                                          /* 更新 blocks 的值。 */
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)                                                                                                /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                                         /* 安排函数结束时执行清理。 */
	raws, err := ReadModbusTCP(ctx, profile, release, blocks)                                                                                                              /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(raws) != 1 || raws[0].ProtocolVersion != "1.0.0" || raws[0].Metadata["startAddress"] != 100 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected raw: %+v", raws) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = <-done; err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestReadModbusTCPPolicyRejectsOutsideNetwork(t *testing.T) { /* 定义 TestReadModbusTCPPolicyRejectsOutsideNetwork 函数。 */
	profile := model.DeviceAccessProfile{Host: "8.8.8.8", Port: 502, UnitID: 1, TimeoutMs: 100}               /* 更新 profile 的值。 */
	release := model.ProtocolRelease{Transport: "MODBUS_TCP"}                                                 /* 更新 release 的值。 */
	blocks := []model.ModbusReadBlock{{ID: "b", FunctionCode: 3, StartAddress: 0, Quantity: 1}}               /* 更新 blocks 的值。 */
	_, err := ReadModbusTCPWithPolicy(context.Background(), profile, release, blocks, []string{"10.0.0.0/8"}) /* 更新 err 的值。 */
	if err == nil {                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("expected target outside allowed CIDRs to be rejected before dialing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
