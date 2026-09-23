// A standalone device fixture: it never creates platform protocols or bindings.
package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"          /* 执行当前语句并推进处理流程。 */
	"flag"                  /* 执行当前语句并推进处理流程。 */
	"fmt"                   /* 执行当前语句并推进处理流程。 */
	"gb26875-dahua/gb26875" /* 执行当前语句并推进处理流程。 */
	"io"                    /* 执行当前语句并推进处理流程。 */
	"net"                   /* 执行当前语句并推进处理流程。 */
	"os"                    /* 执行当前语句并推进处理流程。 */
	"time"                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	address := flag.String("address", "127.0.0.1:26875", "generic listener address")            /* 更新 address 的值。 */
	network := flag.String("network", "tcp", "tcp or udp")                                      /* 更新 network 的值。 */
	source := flag.String("source", "123456789012", "six-byte device address in hex")           /* 更新 source 的值。 */
	hold := flag.Duration("hold", 0, "keep online and acknowledge time-sync commands, e.g. 1m") /* 更新 hold 的值。 */
	flag.Parse()                                                                                /* 执行当前语句并推进处理流程。 */
	if *network != "tcp" && *network != "udp" {                                                 /* 判断条件并选择处理分支。 */
		fail(fmt.Errorf("network must be tcp or udp")) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	data, err := hex.DecodeString(*source) /* 更新 err 的值。 */
	fail(err)                              /* 执行当前语句并推进处理流程。 */
	if len(data) != 6 {                    /* 判断条件并选择处理分支。 */
		fail(fmt.Errorf("source must be six bytes")) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var device [6]byte                                              /* 声明 device。 */
	copy(device[:], data)                                           /* 执行当前语句并推进处理流程。 */
	conn, err := net.DialTimeout(*network, *address, 5*time.Second) /* 更新 err 的值。 */
	fail(err)                                                       /* 执行当前语句并推进处理流程。 */
	defer conn.Close()                                              /* 安排函数结束时执行清理。 */
	read := func() []byte {                                         /* 更新 read 的值。 */
		if *network == "udp" { /* 判断条件并选择处理分支。 */
			buf := make([]byte, 1024) /* 更新 buf 的值。 */
			n, err := conn.Read(buf)  /* 更新 err 的值。 */
			fail(err)                 /* 执行当前语句并推进处理流程。 */
			return buf[:n]            /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		head := make([]byte, 27)                  /* 更新 head 的值。 */
		_, err := io.ReadFull(conn, head)         /* 更新 err 的值。 */
		fail(err)                                 /* 执行当前语句并推进处理流程。 */
		size := int(head[24]) + int(head[25])*256 /* 更新 size 的值。 */
		if size > 512 {                           /* 判断条件并选择处理分支。 */
			fail(fmt.Errorf("invalid frame length")) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		tail := make([]byte, size+3)     /* 更新 tail 的值。 */
		_, err = io.ReadFull(conn, tail) /* 更新 err 的值。 */
		fail(err)                        /* 执行当前语句并推进处理流程。 */
		return append(head, tail...)     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, frame := range [][]byte{gb26875.BuildGB26875RegistrationFrame(1, device, time.Now().In(gb26875.DeviceLocation)), gb26875.BuildGB26875ComponentStatusFrame(2, device, 128, 1, 23, 2, 7, 2, "manual alarm", time.Now().In(gb26875.DeviceLocation))} { /* 循环处理当前数据。 */
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second)) /* 更新 _ 的值。 */
		_, err = conn.Write(frame)                             /* 更新 err 的值。 */
		fail(err)                                              /* 执行当前语句并推进处理流程。 */
		ack, err := gb26875.DecodeFrame(read())                /* 更新 err 的值。 */
		fail(err)                                              /* 执行当前语句并推进处理流程。 */
		if ack.Command != 3 {                                  /* 判断条件并选择处理分支。 */
			fail(fmt.Errorf("expected ACK, got %02X", ack.Command)) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		fmt.Printf("ACK sequence=%d\n", ack.Sequence) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if *hold <= 0 { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_ = conn.SetDeadline(time.Now().Add(*hold))                             /* 更新 _ 的值。 */
	fmt.Printf("online device=gb26875_%s; waiting for commands\n", *source) /* 执行当前语句并推进处理流程。 */
	for {                                                                   /* 循环处理当前数据。 */
		frame, err := gb26875.DecodeFrame(read())                                /* 更新 err 的值。 */
		fail(err)                                                                /* 执行当前语句并推进处理流程。 */
		if frame.Command != 1 || len(frame.Data) != 8 || frame.Data[0] != 0x5a { /* 判断条件并选择处理分支。 */
			fail(fmt.Errorf("unsupported downlink")) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		// Build a device-originated ACK by replacing the frame's two addresses.
		reply := gb26875.BuildGB26875AckFrame(frame.Sequence, [6]byte{255, 255, 255, 255, 255, 255}, time.Now().In(gb26875.DeviceLocation)) /* 更新 reply 的值。 */
		copy(reply[12:18], device[:])                                                                                                       /* 执行当前语句并推进处理流程。 */
		var sum byte                                                                                                                        /* 声明 sum。 */
		for _, v := range reply[2:27] {                                                                                                     /* 循环处理当前数据。 */
			sum += v /* 更新 sum 的值。 */
		} /* 结束当前表达式或代码块。 */
		reply[27] = sum                                                 /* 更新 reply[27] 的值。 */
		_, err = conn.Write(reply)                                      /* 更新 err 的值。 */
		fail(err)                                                       /* 执行当前语句并推进处理流程。 */
		fmt.Printf("confirmed time-sync sequence=%d\n", frame.Sequence) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func fail(err error) { /* 定义 fail 函数。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		fmt.Fprintln(os.Stderr, err) /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
