package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"    /* 执行当前语句并推进处理流程。 */
	"crypto/tls" /* 执行当前语句并推进处理流程。 */
	"errors"     /* 执行当前语句并推进处理流程。 */
	"net"        /* 执行当前语句并推进处理流程。 */
	"net/url"    /* 执行当前语句并推进处理流程。 */
	"time"       /* 执行当前语句并推进处理流程。 */

	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"golang.org/x/net/proxy"                   /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Retain a handle to the transport so backpressure can cause an ordinary
// connection loss, preserving the MQTT client's built-in reconnect lifecycle.
func (c *Client) openConnection(server *url.URL, options mqtt.ClientOptions) (net.Conn, error) { /* 定义 openConnection 函数。 */
	timeout := options.ConnectTimeout /* 更新 timeout 的值。 */
	if timeout <= 0 {                 /* 判断条件并选择处理分支。 */
		timeout = 10 * time.Second /* 更新 timeout 的值。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(c.ctx, timeout)     /* 更新 cancel 的值。 */
	defer cancel()                                         /* 安排函数结束时执行清理。 */
	var conn net.Conn                                      /* 声明 conn。 */
	var err error                                          /* 声明 err。 */
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12} /* 更新 tlsConfig 的值。 */
	if options.TLSConfig != nil {                          /* 判断条件并选择处理分支。 */
		tlsConfig = options.TLSConfig.Clone() /* 更新 tlsConfig 的值。 */
	} /* 结束当前表达式或代码块。 */
	if tlsConfig.ServerName == "" { /* 判断条件并选择处理分支。 */
		tlsConfig.ServerName = server.Hostname() /* 更新 tlsConfig.ServerName 的值。 */
	} /* 结束当前表达式或代码块。 */
	switch server.Scheme { /* 根据条件选择处理路径。 */
	case "ws", "wss": /* 处理当前分支。 */
		target := *server          /* 更新 target 的值。 */
		target.User = nil          /* 更新 target.User 的值。 */
		if server.Scheme == "ws" { /* 判断条件并选择处理分支。 */
			tlsConfig = nil /* 更新 tlsConfig 的值。 */
		} /* 结束当前表达式或代码块。 */
		conn, err = mqtt.NewWebsocket(target.String(), tlsConfig, timeout, options.HTTPHeaders, options.WebsocketOptions) /* 更新 err 的值。 */
	case "tcp", "mqtt", "ssl", "tls", "mqtts", "mqtt+ssl", "tcps", "unix": /* 处理当前分支。 */
		dialer := &net.Dialer{Timeout: timeout} /* 更新 dialer 的值。 */
		if options.Dialer != nil {              /* 判断条件并选择处理分支。 */
			copy := *options.Dialer /* 更新 copy 的值。 */
			dialer = &copy          /* 更新 dialer 的值。 */
		} /* 结束当前表达式或代码块。 */
		if server.Scheme == "unix" { /* 判断条件并选择处理分支。 */
			address := server.Path /* 更新 address 的值。 */
			if server.Host != "" { /* 判断条件并选择处理分支。 */
				address = server.Host /* 更新 address 的值。 */
			} /* 结束当前表达式或代码块。 */
			conn, err = dialer.DialContext(ctx, "unix", address) /* 更新 err 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			proxied := proxy.FromEnvironmentUsing(dialer)            /* 更新 proxied 的值。 */
			if contextual, ok := proxied.(proxy.ContextDialer); ok { /* 判断条件并选择处理分支。 */
				conn, err = contextual.DialContext(ctx, "tcp", server.Host) /* 更新 err 的值。 */
			} else { /* 结束当前表达式或代码块。 */
				conn, err = proxied.Dial("tcp", server.Host) /* 更新 err 的值。 */
			} /* 结束当前表达式或代码块。 */
			if err == nil && server.Scheme != "tcp" && server.Scheme != "mqtt" { /* 判断条件并选择处理分支。 */
				secure := tls.Client(conn, tlsConfig)               /* 更新 secure 的值。 */
				if err = secure.HandshakeContext(ctx); err != nil { /* 判断条件并选择处理分支。 */
					_ = conn.Close() /* 更新 _ 的值。 */
				} else { /* 结束当前表达式或代码块。 */
					conn = secure /* 更新 conn 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	default: /* 处理当前分支。 */
		return nil, errors.New("unsupported MQTT transport") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c.connMu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer c.connMu.Unlock() /* 安排函数结束时执行清理。 */
	if c.ctx.Err() != nil { /* 判断条件并选择处理分支。 */
		conn.Close()            /* 执行当前语句并推进处理流程。 */
		return nil, c.ctx.Err() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c.conn = conn    /* 更新 c.conn 的值。 */
	return conn, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
