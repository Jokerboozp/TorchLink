// Package netguard keeps platform-initiated HTTP requests away from internal
// networks. Integrations that fetch operator-configured URLs (external data,
// third-party media, notification webhooks, AI providers) dial through a
// Policy, which checks the address actually dialled after DNS resolution, so
// a public name rebound to an internal address is refused as well.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// ErrDenied marks a connection to an address the policy does not allow.
var ErrDenied = errors.New("目标地址属于内网、回环或元数据地址，未列入允许网段")

// AlwaysDenied can never be reached, not even through an allowed CIDR: cloud
// metadata services, link-local, multicast, broadcast and unspecified ranges.
var AlwaysDenied = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.100.100.200/32"), // Alibaba Cloud metadata
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fd00:ec2::254/128"), // AWS metadata over IPv6
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("255.255.255.255/32"),
}

// internal ranges are reachable only when listed in Policy.Allowed.
var internal = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT, also cloud internal services
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("198.18.0.0/15"),
}

// Policy allows public addresses and the internal networks in Allowed.
type Policy struct {
	Allowed []netip.Prefix
}

// Denied reports whether addr is in AlwaysDenied.
func Denied(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range AlwaysDenied {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// AllowedAddr reports whether the policy lets a connection reach addr.
func (p Policy) AllowedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() || Denied(addr) {
		return false
	}
	for _, prefix := range p.Allowed {
		if prefix.Contains(addr) {
			return true
		}
	}
	for _, prefix := range internal {
		if prefix.Contains(addr) {
			return false
		}
	}
	return !addr.IsLoopback() && !addr.IsPrivate() && !addr.IsLinkLocalUnicast() && !addr.IsMulticast()
}

// Control is a net.Dialer Control function enforcing the policy on the
// resolved address of every connection, including redirects and retries.
func (p Policy) Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !p.AllowedAddr(addr) {
		return fmt.Errorf("%w：%s", ErrDenied, host)
	}
	return nil
}

// Transport returns an HTTP transport that dials only allowed addresses and
// ignores proxy environment variables, which would otherwise receive the
// request (and its credentials) instead of the checked address.
func (p Policy) Transport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: p.Control}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// ParsePrefixes parses a comma-separated CIDR list; empty items are ignored.
func ParsePrefixes(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q", item)
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
}

// FromIPNets converts net.IPNet lists used by older configuration fields.
func FromIPNets(networks []*net.IPNet) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(networks))
	for _, n := range networks {
		if n == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			continue
		}
		ones, _ := n.Mask.Size()
		out = append(out, netip.PrefixFrom(addr.Unmap(), ones).Masked())
	}
	return out
}

// Loopback allows only the local host; tests use it for httptest servers.
var Loopback = Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}}
