package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                                     /* 执行当前语句并推进处理流程。 */
	"errors"                                      /* 执行当前语句并推进处理流程。 */
	"github.com/eclipse/paho.mqtt.golang/packets" /* 执行当前语句并推进处理流程。 */
	"io"                                          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"             /* 执行当前语句并推进处理流程。 */
	"net"                                         /* 执行当前语句并推进处理流程。 */
	"testing"                                     /* 执行当前语句并推进处理流程。 */
	"time"                                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestProbeHandshake(t *testing.T) { /* 定义 TestProbeHandshake 函数。 */
	for _, code := range []byte{0, 4, 5} { /* 循环处理当前数据。 */
		t.Run(string(rune('0'+code)), func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
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
				defer conn.Close()                                /* 安排函数结束时执行清理。 */
				conn.SetDeadline(time.Now().Add(3 * time.Second)) /* 执行当前语句并推进处理流程。 */
				packet, err := packets.ReadPacket(conn)           /* 更新 err 的值。 */
				if err != nil {                                   /* 判断条件并选择处理分支。 */
					done <- err /* 执行当前语句并推进处理流程。 */
					return      /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				connect, ok := packet.(*packets.ConnectPacket)                                                                      /* 更新 ok 的值。 */
				if !ok || connect.Username != "probe-user" || string(connect.Password) != "probe-secret" || !connect.CleanSession { /* 判断条件并选择处理分支。 */
					done <- errors.New("invalid handshake") /* 执行当前语句并推进处理流程。 */
					return                                  /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				ack := packets.NewControlPacket(packets.Connack).(*packets.ConnackPacket) /* 更新 ack 的值。 */
				ack.ReturnCode = code                                                     /* 更新 ack.ReturnCode 的值。 */
				err = ack.Write(conn)                                                     /* 更新 err 的值。 */
				if code == 0 && err == nil {                                              /* 判断条件并选择处理分支。 */
					_, err = packets.ReadPacket(conn) /* 更新 err 的值。 */
				} /* 结束当前表达式或代码块。 */
				done <- err /* 执行当前语句并推进处理流程。 */
			}() /* 结束当前表达式或代码块。 */
			client := &Client{broker: "tcp://" + listener.Addr().String(), credentials: func() (string, string) { return "probe-user", "probe-secret" }} /* 更新 client 的值。 */
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)                                                                      /* 更新 cancel 的值。 */
			defer cancel()                                                                                                                               /* 安排函数结束时执行清理。 */
			err = client.Probe(ctx)                                                                                                                      /* 更新 err 的值。 */
			if code == 0 && err != nil {                                                                                                                 /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if code != 0 && !errors.Is(err, connector.ErrAuthentication) { /* 判断条件并选择处理分支。 */
				t.Fatalf("code %d: %v", code, err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if err := <-done; err != nil && !errors.Is(err, io.EOF) { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProbeDeadline(t *testing.T) { /* 定义 TestProbeDeadline 函数。 */
	listener, err := net.Listen("tcp", "127.0.0.1:0") /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer listener.Close()      /* 安排函数结束时执行清理。 */
	done := make(chan struct{}) /* 更新 done 的值。 */
	defer close(done)           /* 安排函数结束时执行清理。 */
	go func() {                 /* 执行当前语句并推进处理流程。 */
		conn, err := listener.Accept() /* 更新 err 的值。 */
		if err == nil {                /* 判断条件并选择处理分支。 */
			defer conn.Close() /* 安排函数结束时执行清理。 */
			<-done             /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	client := &Client{broker: "tcp://" + listener.Addr().String(), credentials: func() (string, string) { return "u", "p" }} /* 更新 client 的值。 */
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)                                            /* 更新 cancel 的值。 */
	defer cancel()                                                                                                           /* 安排函数结束时执行清理。 */
	if err := client.Probe(ctx); !errors.Is(err, context.DeadlineExceeded) {                                                 /* 判断条件并选择处理分支。 */
		t.Fatalf("expected deadline, got %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
