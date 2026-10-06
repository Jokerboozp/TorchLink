package notify

import (
	"net"
	"net/netip"
	"testing"
)

func TestNotificationTargetsStayOffInternalAndPlainPublicHTTP(t *testing.T) {
	s := NewSender(nil)
	for _, addr := range []string{"100.100.100.200", "100.64.1.1", "169.254.169.254", "127.0.0.1", "10.0.0.1"} {
		if s.allowedIP(net.IP(netip.MustParseAddr(addr).AsSlice())) {
			t.Fatalf("%s must not be reachable without IOT_NOTIFY_ALLOWED_CIDRS", addr)
		}
	}
	if !s.allowedIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public addresses stay reachable")
	}
	if err := s.ValidateTarget(ChannelWebhook, ChannelConfig{}, ChannelSecret{URL: "http://hooks.example.com/alarm"}); err == nil {
		t.Fatal("a public webhook over plain HTTP was accepted")
	}
	for _, target := range []string{"https://hooks.example.com/alarm", "http://10.0.0.5:8080/alarm", "http://gateway:8080/alarm"} {
		if err := s.ValidateTarget(ChannelWebhook, ChannelConfig{}, ChannelSecret{URL: target}); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
}
