package fieldprotocol

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"net"
	"strings"
	"testing"
)

func TestBACnetReadPropertyWire(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		n, addr, e := conn.ReadFrom(buf)
		if e != nil {
			return
		}
		// Independent fixed analog-input:1 / present-value request and Real 42 response.
		if n != 17 || hex.EncodeToString(buf[9:n]) != "0c0c000000011955" {
			t.Error("unexpected BACnet request")
		}
		response, _ := hex.DecodeString("810a0018010030000c0c0000000119553e44422800003f")
		response[7] = buf[8]
		binary.BigEndian.PutUint16(response[2:4], uint16(len(response)))
		_, _ = conn.WriteTo(response, addr)
	}()
	p := model.DeviceAccessProfile{ID: "read", TenantID: "t", ProductID: "p", DeviceID: "d", Host: "127.0.0.1", Port: conn.LocalAddr().(*net.UDPAddr).Port, TimeoutMs: 1000, CredentialRef: "bacnet"}
	r := model.ProtocolRelease{Transport: "BACNET", ProtocolID: "bacnet", Version: "1", Config: map[string]any{"reads": []model.PollPoint{{Identifier: "temperature", Address: "0:1:85"}}}}
	c := Collector{AllowedCIDRs: []string{"127.0.0.0/8"}, Credentials: map[string]Credential{"bacnet": {BACnetMode: "ip"}}}
	raws, err := c.Read(context.Background(), p, r)
	<-done
	if err != nil || len(raws) != 1 {
		t.Fatal(err)
	}
	m, err := (parser.PollResponseParser{}).ParseWithConfig(raws[0], r.Config)
	if err != nil || m.Properties["temperature"] != float64(42) {
		t.Fatal(m, err)
	}
	if !strings.Contains(string(raws[0].Payload), "responseHex") || !strings.Contains(string(raws[0].Payload), "none-native-bacnet-ip") {
		t.Fatal("wire evidence or native auth boundary missing")
	}
}
func TestBACnetMalformedResponses(t *testing.T) {
	frame, _ := hex.DecodeString("810a0015010030010c0c0000000119553e44422800003f")
	binary.BigEndian.PutUint16(frame[2:4], uint16(len(frame)))
	p, _ := parseBACnetAddress("0:1:85")
	if _, err := decodeBACnetRead(frame, 1, p); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(frame); n++ {
		if _, err := decodeBACnetRead(frame[:n], 1, p); err == nil {
			t.Fatalf("truncated frame accepted at %d", n)
		}
	}
	for _, index := range []int{0, 1, 3, 4, 5, 6, 7, 8, 10, 14, 15, 16, 22} {
		mutated := append([]byte(nil), frame...)
		mutated[index] ^= 0xff
		if _, err := decodeBACnetRead(mutated, 1, p); err == nil {
			t.Fatalf("mutated frame accepted at %d", index)
		}
	}
}

func FuzzBACnetResponse(f *testing.F) {
	seed, _ := hex.DecodeString("810a0015010030010c0c0000000119553e44422800003f")
	f.Add(seed)
	f.Fuzz(func(t *testing.T, frame []byte) {
		if len(frame) > 1500 {
			return
		}
		_, _ = decodeBACnetRead(frame, 1, bacnetPoint{Object: 1, Property: 85})
	})
}
