package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestPolicyRejectsInternalAndMetadataAddresses(t *testing.T) {
	public := Policy{}
	for _, addr := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.20.0.5", "192.168.1.1", "169.254.169.254", "100.100.100.200", "100.64.0.1", "0.0.0.0", "fd00:ec2::254", "fe80::1", "224.0.0.1"} {
		if public.AllowedAddr(netip.MustParseAddr(addr)) {
			t.Fatalf("%s must be denied by default", addr)
		}
	}
	for _, addr := range []string{"8.8.8.8", "2001:4860:4860::8888", "::ffff:1.1.1.1"} {
		if !public.AllowedAddr(netip.MustParseAddr(addr)) {
			t.Fatalf("%s is public", addr)
		}
	}
	private := Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16")}}
	if !private.AllowedAddr(netip.MustParseAddr("10.9.9.9")) {
		t.Fatal("an allowed private network must be reachable")
	}
	if private.AllowedAddr(netip.MustParseAddr("169.254.169.254")) {
		t.Fatal("metadata addresses stay denied even inside an allowed CIDR")
	}
}

func TestTransportRefusesTheDialledAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	_, err := (&http.Client{Transport: Policy{}.Transport()}).Get(server.URL)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("loopback request must be refused at dial time, got %v", err)
	}
	resp, err := (&http.Client{Transport: Loopback.Transport()}).Get(server.URL)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("explicitly allowed loopback failed: %v", err)
	}
	resp.Body.Close()
}

func TestFromIPNets(t *testing.T) {
	_, n, _ := net.ParseCIDR("192.168.0.0/16")
	got := FromIPNets([]*net.IPNet{n})
	if len(got) != 1 || got[0] != netip.MustParsePrefix("192.168.0.0/16") {
		t.Fatalf("got %v", got)
	}
}
