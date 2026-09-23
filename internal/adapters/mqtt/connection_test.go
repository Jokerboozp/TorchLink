package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                                  /* 执行当前语句并推进处理流程。 */
	"crypto/tls"                               /* 执行当前语句并推进处理流程。 */
	"crypto/x509"                              /* 执行当前语句并推进处理流程。 */
	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"github.com/gorilla/websocket"             /* 执行当前语句并推进处理流程。 */
	"net"                                      /* 执行当前语句并推进处理流程。 */
	"net/http"                                 /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                        /* 执行当前语句并推进处理流程。 */
	"net/url"                                  /* 执行当前语句并推进处理流程。 */
	"testing"                                  /* 执行当前语句并推进处理流程。 */
	"time"                                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestMQTTTransportTLSAndWebsocketVerification(t *testing.T) { /* 定义 TestMQTTTransportTLSAndWebsocketVerification 函数。 */
	c := &Client{ctx: context.Background()}                                                        /* 更新 c 的值。 */
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})) /* 更新 server 的值。 */
	defer server.Close()                                                                           /* 安排函数结束时执行清理。 */
	endpoint, _ := url.Parse("tls://" + server.Listener.Addr().String())                           /* 更新 _ 的值。 */
	opts := mqtt.NewClientOptions()                                                                /* 更新 opts 的值。 */
	opts.ConnectTimeout = time.Second                                                              /* 更新 opts.ConnectTimeout 的值。 */
	if conn, err := c.openConnection(endpoint, *opts); err == nil {                                /* 判断条件并选择处理分支。 */
		conn.Close()                             /* 执行当前语句并推进处理流程。 */
		t.Fatal("untrusted TLS broker accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	roots := x509.NewCertPool()                                                /* 更新 roots 的值。 */
	roots.AddCert(server.Certificate())                                        /* 执行当前语句并推进处理流程。 */
	opts.TLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12} /* 更新 opts.TLSConfig 的值。 */
	conn, err := c.openConnection(endpoint, *opts)                             /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		t.Fatal("trusted TLS broker", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	conn.Close()                                                                                /* 执行当前语句并推进处理流程。 */
	upgrade := websocket.Upgrader{Subprotocols: []string{"mqtt"}}                               /* 更新 upgrade 的值。 */
	ws := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 ws 的值。 */
		conn, err := upgrade.Upgrade(w, r, nil) /* 检查错误并决定后续处理。 */
		if err == nil {                         /* 判断条件并选择处理分支。 */
			defer conn.Close()           /* 安排函数结束时执行清理。 */
			_, _, _ = conn.ReadMessage() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer ws.Close()                                                                        /* 安排函数结束时执行清理。 */
	opts.TLSConfig = &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12} /* 更新 opts.TLSConfig 的值。 */
	endpoint, _ = url.Parse("wss://" + ws.Listener.Addr().String() + "/mqtt")               /* 更新 _ 的值。 */
	if conn, err := c.openConnection(endpoint, *opts); err == nil {                         /* 判断条件并选择处理分支。 */
		conn.Close()                        /* 执行当前语句并推进处理流程。 */
		t.Fatal("wrong WSS trust accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	roots = x509.NewCertPool()                                                 /* 更新 roots 的值。 */
	roots.AddCert(ws.Certificate())                                            /* 执行当前语句并推进处理流程。 */
	opts.TLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12} /* 更新 opts.TLSConfig 的值。 */
	conn, err = c.openConnection(endpoint, *opts)                              /* 更新 err 的值。 */
	if err != nil {                                                            /* 判断条件并选择处理分支。 */
		t.Fatal("trusted WSS", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	conn.Close() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func TestMQTTTransportHandshakeTimeoutAndCancellation(t *testing.T) { /* 定义 TestMQTTTransportHandshakeTimeoutAndCancellation 函数。 */
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
	c := &Client{ctx: context.Background()}                         /* 更新 c 的值。 */
	endpoint, _ := url.Parse("tls://" + listener.Addr().String())   /* 更新 _ 的值。 */
	opts := mqtt.NewClientOptions()                                 /* 更新 opts 的值。 */
	opts.ConnectTimeout = 40 * time.Millisecond                     /* 更新 opts.ConnectTimeout 的值。 */
	start := time.Now()                                             /* 更新 start 的值。 */
	if conn, err := c.openConnection(endpoint, *opts); err == nil { /* 判断条件并选择处理分支。 */
		conn.Close()                              /* 执行当前语句并推进处理流程。 */
		t.Fatal("stalled TLS handshake accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if time.Since(start) > time.Second { /* 判断条件并选择处理分支。 */
		t.Fatal("handshake ignored deadline") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithCancel(context.Background())         /* 更新 cancel 的值。 */
	cancel()                                                        /* 执行当前语句并推进处理流程。 */
	c.ctx = ctx                                                     /* 更新 c.ctx 的值。 */
	endpoint.Scheme = "tcp"                                         /* 更新 endpoint.Scheme 的值。 */
	if conn, err := c.openConnection(endpoint, *opts); err == nil { /* 判断条件并选择处理分支。 */
		conn.Close()                             /* 执行当前语句并推进处理流程。 */
		t.Fatal("cancelled connection accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
