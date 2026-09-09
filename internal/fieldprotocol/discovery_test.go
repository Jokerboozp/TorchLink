package fieldprotocol

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/testonvif"
	"net"
	"strings"
	"testing"
	"time"
)

func TestONVIFDiscoveryUDPBoundaries(t *testing.T) {
	sim := testonvif.StartDiscovery(t, "https://127.0.0.1:8443/onvif/device_service")
	options := DiscoveryOptions{InterfaceAddress: "127.0.0.1", AllowedInterfaces: []string{"127.0.0.1"}, AllowedCIDRs: []string{"127.0.0.0/8"}, ProbeAddress: sim.Address, Duration: 150 * time.Millisecond}
	result, err := DiscoverONVIF(context.Background(), options)
	if err != nil || len(result.Items) != 1 || result.Authenticated || result.Discarded < 1 || len(result.Items[0].XAddrs) != 1 || result.Items[0].XAddrs[0] != "https://127.0.0.1:8443/onvif/device_service" {
		t.Fatal("discovery response", result, err)
	}
	if err := ValidateDiscoveryResult(result); err != nil {
		t.Fatal(err)
	}
	result.Authenticated = true
	if ValidateDiscoveryResult(result) == nil {
		t.Fatal("untrusted discovery claimed authenticated")
	}
	before := sim.Packets.Load()
	options.AllowedInterfaces = nil
	if _, err := DiscoverONVIF(context.Background(), options); err == nil || sim.Packets.Load() != before {
		t.Fatal("discovery ran without local interface permission")
	}
	options.AllowedInterfaces = []string{"127.0.0.1"}
	sim.Quiet.Store(true)
	result, err = DiscoverONVIF(context.Background(), options)
	if err != nil || len(result.Items) != 0 {
		t.Fatal("no-response discovery invented a device", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverONVIF(ctx, options); err == nil {
		t.Fatal("cancelled discovery succeeded")
	}
}

func TestONVIFProbeMatchValidation(t *testing.T) {
	good := string(testonvif.DiscoveryMatch("urn:uuid:request", "https://127.0.0.1/onvif/device_service"))
	for _, value := range []string{strings.Replace(good, "urn:uuid:request", "wrong", 1), "<!DOCTYPE x>" + good, good + good, strings.Replace(good, "</a:RelatesTo>", "</a:RelatesTo><a:RelatesTo>urn:uuid:request</a:RelatesTo>", 1), strings.ReplaceAll(good, addressingNS, "urn:wrong"), strings.Repeat("x", 65536)} {
		if _, err := parseProbeMatches([]byte(value), "urn:uuid:request", net.ParseIP("127.0.0.1")); err == nil {
			t.Fatal("malformed/correlation response accepted")
		}
	}
	for _, address := range []string{"https://127.0.0.2/onvif", "https://localhost/onvif", "http://user:password@127.0.0.1/onvif", "file:///tmp/private", "https://127.0.0.1:99999/onvif", "https://127.0.0.1/onvif?secret=value"} {
		items, err := parseProbeMatches(testonvif.DiscoveryMatch("urn:uuid:request", address), "urn:uuid:request", net.ParseIP("127.0.0.1"))
		if err != nil || len(items) != 0 {
			t.Fatal("unsafe address exposed", address, err)
		}
	}
}

func TestONVIFDiscoveryLoopbackMulticast(t *testing.T) {
	iface, err := interfaceForIP(net.ParseIP("127.0.0.1"))
	if err != nil {
		t.Skip("loopback multicast interface unavailable: ", err)
	}
	socket, err := net.ListenMulticastUDP("udp4", iface, &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 3702})
	if err != nil {
		t.Skip("loopback multicast socket unavailable: ", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, peer, err := socket.ReadFromUDP(buf)
			if err != nil {
				return
			}
			data := string(buf[:n])
			start := strings.Index(data, "<a:MessageID>")
			end := strings.Index(data, "</a:MessageID>")
			if start < 0 || end <= start {
				continue
			}
			id := data[start+len("<a:MessageID>") : end]
			_, _ = socket.WriteToUDP(testonvif.DiscoveryMatch(id, "https://127.0.0.1/onvif/device_service"), peer)
		}
	}()
	defer func() { socket.Close(); <-done }()
	result, err := DiscoverONVIF(context.Background(), DiscoveryOptions{InterfaceAddress: "127.0.0.1", AllowedInterfaces: []string{"127.0.0.1"}, AllowedCIDRs: []string{"127.0.0.0/8"}, Duration: 500 * time.Millisecond})
	if err != nil || len(result.Items) != 1 {
		t.Fatal("loopback multicast did not discover simulator", result, err)
	}
}

func FuzzONVIFProbeMatch(f *testing.F) {
	f.Add(testonvif.DiscoveryMatch("request", "https://127.0.0.1/onvif"))
	f.Add([]byte("<invalid>"))
	f.Fuzz(func(t *testing.T, data []byte) {
		items, err := parseProbeMatches(data, "request", net.ParseIP("127.0.0.1"))
		if err == nil {
			if err := ValidateDiscoveryResult(model.ONVIFDiscovery{Items: items}); err != nil {
				t.Fatalf("invalid candidate escaped parser: %v", err)
			}
		}
	})
}
