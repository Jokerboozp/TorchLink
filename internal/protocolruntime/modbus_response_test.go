package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"io"      /* 执行当前语句并推进处理流程。 */
	"net"     /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/modbusframe" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// A gateway must return exactly the requested register/coil data; a valid
// checksum alone does not prove a complete response to this polling request.
func TestModbusRejectsResponseQuantityMismatch(t *testing.T) { /* 定义 TestModbusRejectsResponseQuantityMismatch 函数。 */
	for _, wire := range []string{"modbus_tcp", "rtu_over_tcp"} { /* 循环处理当前数据。 */
		for _, tc := range []struct { /* 循环处理当前数据。 */
			name               string /* 执行当前语句并推进处理流程。 */
			function, quantity int    /* 执行当前语句并推进处理流程。 */
			data               []byte /* 执行当前语句并推进处理流程。 */
			valid              bool   /* 执行当前语句并推进处理流程。 */
		}{ /* 结束当前表达式或代码块。 */
			{"complete-registers", 3, 2, []byte{0, 1, 0, 2}, true}, /* 执行当前语句并推进处理流程。 */
			{"missing-register", 3, 2, []byte{0, 1}, false},        /* 执行当前语句并推进处理流程。 */
			{"extra-register", 3, 1, []byte{0, 1, 0, 2}, false},    /* 执行当前语句并推进处理流程。 */
			{"odd-register-bytes", 3, 1, []byte{1}, false},         /* 执行当前语句并推进处理流程。 */
			{"complete-coils", 1, 9, []byte{1, 1}, true},           /* 执行当前语句并推进处理流程。 */
			{"missing-coil-byte", 1, 9, []byte{1}, false},          /* 执行当前语句并推进处理流程。 */
		} { /* 结束当前表达式或代码块。 */
			t.Run(wire+"/"+tc.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
				listener, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
				if err != nil {                                   /* 判断条件并选择处理分支。 */
					t.Fatal(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				defer listener.Close() /* 安排函数结束时执行清理。 */
				go func() {            /* 执行当前语句并推进处理流程。 */
					c, e := listener.Accept() /* 更新 e 的值。 */
					if e != nil {             /* 判断条件并选择处理分支。 */
						return /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
					defer c.Close()                                /* 安排函数结束时执行清理。 */
					_ = c.SetDeadline(time.Now().Add(time.Second)) /* 更新 _ 的值。 */
					n := 12                                        /* 更新 n 的值。 */
					if wire == "rtu_over_tcp" {                    /* 判断条件并选择处理分支。 */
						n = 8 /* 更新 n 的值。 */
					} /* 结束当前表达式或代码块。 */
					q := make([]byte, n)                    /* 更新 q 的值。 */
					if _, e = io.ReadFull(c, q); e != nil { /* 判断条件并选择处理分支。 */
						return /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
					pdu := append([]byte{1, byte(tc.function), byte(len(tc.data))}, tc.data...) /* 更新 pdu 的值。 */
					response := modbusframe.AppendCRC(pdu)                                      /* 更新 response 的值。 */
					if wire == "modbus_tcp" {                                                   /* 判断条件并选择处理分支。 */
						response = append([]byte{q[0], q[1], 0, 0, 0, byte(len(pdu))}, pdu...) /* 更新 response 的值。 */
					} /* 结束当前表达式或代码块。 */
					_, _ = c.Write(response) /* 更新 _ 的值。 */
				}() /* 结束当前表达式或代码块。 */
				p := model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500, WireFormat: wire} /* 更新 p 的值。 */
				transport := "MODBUS_TCP"                                                                                                                 /* 更新 transport 的值。 */
				if wire == "rtu_over_tcp" {                                                                                                               /* 判断条件并选择处理分支。 */
					transport = "MODBUS_RTU" /* 更新 transport 的值。 */
				} /* 结束当前表达式或代码块。 */
				raws, err := ReadModbusTCP(context.Background(), p, model.ProtocolRelease{Transport: transport}, []model.ModbusReadBlock{{FunctionCode: tc.function, StartAddress: 0x2000, Quantity: tc.quantity}}) /* 更新 err 的值。 */
				if tc.valid {                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
					if err != nil || len(raws) != 1 { /* 判断条件并选择处理分支。 */
						t.Fatalf("valid response rejected: %v", err) /* 验证实际结果符合预期。 */
					} /* 结束当前表达式或代码块。 */
				} else if err == nil { /* 结束当前表达式或代码块。 */
					t.Fatalf("incomplete/mismatched response accepted as %d raw messages", len(raws)) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			}) /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
