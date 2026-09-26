package mqttadapter

import (
	"context"
	"errors"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"io"
	"iot-platform/internal/connector"
	"net"
	"testing"
	"time"
)

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
