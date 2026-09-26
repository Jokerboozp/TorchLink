package mqttadapter

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"golang.org/x/net/proxy"
)

// Retain a handle to the transport so backpressure can cause an ordinary
// connection loss, preserving the MQTT client's built-in reconnect lifecycle.
func (c *Client) openConnection(server *url.URL, options mqtt.ClientOptions) (net.Conn, error) {
	timeout := options.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(c.ctx, timeout)
	defer cancel()
	var conn net.Conn
	var err error
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if options.TLSConfig != nil {
		tlsConfig = options.TLSConfig.Clone()
	}
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = server.Hostname()
	}
	switch server.Scheme {
	case "ws", "wss":
		target := *server
		target.User = nil
		if server.Scheme == "ws" {
			tlsConfig = nil
		}
		conn, err = mqtt.NewWebsocket(target.String(), tlsConfig, timeout, options.HTTPHeaders, options.WebsocketOptions)
	case "tcp", "mqtt", "ssl", "tls", "mqtts", "mqtt+ssl", "tcps", "unix":
		dialer := &net.Dialer{Timeout: timeout}
		if options.Dialer != nil {
			copy := *options.Dialer
			dialer = &copy
		}
		if server.Scheme == "unix" {
			address := server.Path
			if server.Host != "" {
				address = server.Host
			}
			conn, err = dialer.DialContext(ctx, "unix", address)
		} else {
			proxied := proxy.FromEnvironmentUsing(dialer)
			if contextual, ok := proxied.(proxy.ContextDialer); ok {
				conn, err = contextual.DialContext(ctx, "tcp", server.Host)
			} else {
				conn, err = proxied.Dial("tcp", server.Host)
			}
			if err == nil && server.Scheme != "tcp" && server.Scheme != "mqtt" {
				secure := tls.Client(conn, tlsConfig)
				if err = secure.HandshakeContext(ctx); err != nil {
					_ = conn.Close()
				} else {
					conn = secure
				}
			}
		}
	default:
		return nil, errors.New("unsupported MQTT transport")
	}
	if err != nil {
		return nil, err
	}
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.ctx.Err() != nil {
		conn.Close()
		return nil, c.ctx.Err()
	}
	c.conn = conn
	return conn, nil
}
