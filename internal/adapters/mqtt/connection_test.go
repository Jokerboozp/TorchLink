package mqttadapter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/websocket"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestMQTTTransportTLSAndWebsocketVerification(t *testing.T) {
	c := &Client{ctx: context.Background()}
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	endpoint, _ := url.Parse("tls://" + server.Listener.Addr().String())
	opts := mqtt.NewClientOptions()
	opts.ConnectTimeout = time.Second
	if conn, err := c.openConnection(endpoint, *opts); err == nil {
		conn.Close()
		t.Fatal("untrusted TLS broker accepted")
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	opts.TLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	conn, err := c.openConnection(endpoint, *opts)
	if err != nil {
		t.Fatal("trusted TLS broker", err)
	}
	conn.Close()
	upgrade := websocket.Upgrader{Subprotocols: []string{"mqtt"}}
	ws := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrade.Upgrade(w, r, nil)
		if err == nil {
			defer conn.Close()
			_, _, _ = conn.ReadMessage()
		}
	}))
	defer ws.Close()
	opts.TLSConfig = &tls.Config{RootCAs: x509.NewCertPool(), MinVersion: tls.VersionTLS12}
	endpoint, _ = url.Parse("wss://" + ws.Listener.Addr().String() + "/mqtt")
	if conn, err := c.openConnection(endpoint, *opts); err == nil {
		conn.Close()
		t.Fatal("wrong WSS trust accepted")
	}
	roots = x509.NewCertPool()
	roots.AddCert(ws.Certificate())
	opts.TLSConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	conn, err = c.openConnection(endpoint, *opts)
	if err != nil {
		t.Fatal("trusted WSS", err)
	}
	conn.Close()
}

func TestMQTTTransportHandshakeTimeoutAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			<-done
		}
	}()
	c := &Client{ctx: context.Background()}
	endpoint, _ := url.Parse("tls://" + listener.Addr().String())
	opts := mqtt.NewClientOptions()
	opts.ConnectTimeout = 40 * time.Millisecond
	start := time.Now()
	if conn, err := c.openConnection(endpoint, *opts); err == nil {
		conn.Close()
		t.Fatal("stalled TLS handshake accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("handshake ignored deadline")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.ctx = ctx
	endpoint.Scheme = "tcp"
	if conn, err := c.openConnection(endpoint, *opts); err == nil {
		conn.Close()
		t.Fatal("cancelled connection accepted")
	}
}
