package mqttadapter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/gorilla/websocket"

	"iot-platform/internal/connector"
	"iot-platform/internal/model"
)

func TestTopicIdentityOverridesPayloadTenant(t *testing.T) {
	raw := model.RawMessage{TenantID: "spoofed", ProductID: "spoofed", DeviceID: "spoofed"}
	if err := applyRawTopicIdentity("/external/raw/tenant-a/product-a/device-a", &raw); err != nil {
		t.Fatal(err)
	}
	if raw.TenantID != "tenant-a" || raw.ProductID != "product-a" || raw.DeviceID != "device-a" {
		t.Fatalf("topic identity was not authoritative: %#v", raw)
	}

	state := model.DeviceState{TenantID: "spoofed", ProductID: "spoofed", DeviceID: "spoofed"}
	if err := applyStateTopicIdentity("/iot/device/state/tenant-a/product-a/device-a", &state); err != nil {
		t.Fatal(err)
	}
	if state.TenantID != "tenant-a" || state.ProductID != "product-a" || state.DeviceID != "device-a" {
		t.Fatalf("state topic identity was not authoritative: %#v", state)
	}
}

func TestTopicIdentityRejectsMalformedTopics(t *testing.T) {
	if err := applyStateTopicIdentity("/iot/device/state/tenant-a/product-a", &model.DeviceState{}); err == nil {
		t.Fatal("expected malformed state topic to be rejected")
	}
}

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

func TestBoundedIngressRejectsOverload(t *testing.T) {
	c := &Client{jobs: make(chan func(), 2), stop: make(chan struct{}), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ran := 0
	for i := 0; i < 2; i++ {
		if !c.enqueue("test", func() { ran++ }) {
			t.Fatal("early rejection")
		}
	}
	if c.enqueue("test", func() { t.Fatal("overload executed") }) {
		t.Fatal("unbounded queue")
	}
	if ran != 0 || len(c.jobs) != 2 {
		t.Fatal("callback executed inline or queue changed")
	}
	(<-c.jobs)()
	(<-c.jobs)()
	if ran != 2 {
		t.Fatal("accepted jobs lost")
	}
	close(c.stop)
	if c.enqueue("test", func() {}) {
		t.Fatal("enqueue after shutdown")
	}
}

func TestStandardTopicIdentity(t *testing.T) {
	tenant, product, device, kind, err := StandardTopic("/iot/up/t/p/d/property")
	if err != nil || tenant != "t" || product != "p" || device != "d" || kind != "property" {
		t.Fatal(tenant, product, device, kind, err)
	}
	for _, topic := range []string{"/iot/up/t/p/d/shadow-get", "/iot/down/t/p/d/shadow", "/iot/up/t/p/d/command", "iot/up/t/p/d/property", "/iot/up/t/p/d/property/extra", "/external/raw/t/p/d"} {
		if _, _, _, _, err := StandardTopic(topic); err == nil {
			t.Fatalf("accepted %q", topic)
		}
	}
}

func TestProbeHandshake(t *testing.T) {
	for _, code := range []byte{0, 4, 5} {
		t.Run(string(rune('0'+code)), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				packet, err := packets.ReadPacket(conn)
				if err != nil {
					done <- err
					return
				}
				connect, ok := packet.(*packets.ConnectPacket)
				if !ok || connect.Username != "probe-user" || string(connect.Password) != "probe-secret" || !connect.CleanSession {
					done <- errors.New("invalid handshake")
					return
				}
				ack := packets.NewControlPacket(packets.Connack).(*packets.ConnackPacket)
				ack.ReturnCode = code
				err = ack.Write(conn)
				if code == 0 && err == nil {
					_, err = packets.ReadPacket(conn)
				}
				done <- err
			}()
			client := &Client{broker: "tcp://" + listener.Addr().String(), credentials: func() (string, string) { return "probe-user", "probe-secret" }}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err = client.Probe(ctx)
			if code == 0 && err != nil {
				t.Fatal(err)
			}
			if code != 0 && !errors.Is(err, connector.ErrAuthentication) {
				t.Fatalf("code %d: %v", code, err)
			}
			if err := <-done; err != nil && !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
		})
	}
}

func TestProbeDeadline(t *testing.T) {
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
	client := &Client{broker: "tcp://" + listener.Addr().String(), credentials: func() (string, string) { return "u", "p" }}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := client.Probe(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
}
