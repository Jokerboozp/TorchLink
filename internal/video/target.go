package video

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/netguard"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrTargetDenied marks an address outside the configured camera networks.
var ErrTargetDenied = errors.New("目标地址不在允许的摄像头网段或端口内")

// targetGuard restricts ONVIF and RTSP access to administrator-configured
// camera networks and ports. Every check resolves the host and validates all
// resulting addresses; connections then dial the validated IP, so DNS changes
// between check and use (rebinding) cannot redirect a request.
type targetGuard struct {
	networks []*net.IPNet
	// denied excludes addresses inside networks, such as the Compose network.
	denied   []*net.IPNet
	ports    map[int]bool
	resolver interface {
		LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
	}
}

func (g targetGuard) allowedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() {
		return false
	}
	// Metadata, link-local and multicast stay denied even inside a broad CIDR.
	if netguard.Denied(addr) {
		return false
	}
	for _, n := range g.denied {
		if n.Contains(net.IP(addr.AsSlice())) {
			return false
		}
	}
	for _, n := range g.networks {
		if n.Contains(net.IP(addr.AsSlice())) {
			return true
		}
	}
	return false
}

// resolve validates host:port and returns one validated address to dial.
func (g targetGuard) resolve(ctx context.Context, host string, port int) (netip.Addr, error) {
	if !g.ports[port] {
		return netip.Addr{}, fmt.Errorf("%w：端口 %d 不在 IOT_VIDEO_ALLOWED_PORTS 中", ErrTargetDenied, port)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "/\\@?#%") {
		return netip.Addr{}, fmt.Errorf("%w：主机名无效", ErrTargetDenied)
	}
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		resolver := g.resolver
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		addrs, err = resolver.LookupNetIP(lookup, "ip", host)
		if err != nil || len(addrs) == 0 {
			return netip.Addr{}, fmt.Errorf("无法解析摄像头主机名：%s", host)
		}
	}
	// A name that resolves to any disallowed address is rejected as a whole.
	for _, addr := range addrs {
		if !g.allowedAddr(addr) {
			return netip.Addr{}, fmt.Errorf("%w：%s 不在 IOT_VIDEO_ALLOWED_CIDRS 中或属于禁止访问的地址", ErrTargetDenied, addr.Unmap())
		}
	}
	return addrs[0].Unmap(), nil
}

// pinnedRTSP validates an RTSP URL and returns it with the host replaced by
// the validated IP. The media server then cannot re-resolve the name.
func (g targetGuard) pinnedRTSP(ctx context.Context, raw string) (*url.URL, error) {
	u, err := parseStreamURL(raw)
	if err != nil {
		return nil, err
	}
	port := 554
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	addr, err := g.resolve(ctx, u.Hostname(), port)
	if err != nil {
		return nil, err
	}
	pinned := *u
	pinned.Host = net.JoinHostPort(addr.String(), strconv.Itoa(port))
	return &pinned, nil
}

// parseStreamURL accepts only credential-free rtsp:// URLs.
func parseStreamURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 1024 || strings.ContainsAny(raw, " \t\r\n") {
		return nil, errors.New("流地址格式无效")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "rtsp") || u.Hostname() == "" || u.Fragment != "" {
		return nil, errors.New("仅支持 rtsp:// 流地址")
	}
	if u.User != nil {
		return nil, errors.New("请把用户名和密码填写在单独的字段中，不要写进流地址")
	}
	u.Scheme = "rtsp"
	return u, nil
}

// dial connects to a validated address.
func (g targetGuard) dial(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	addr, err := g.resolve(ctx, host, port)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), strconv.Itoa(port)))
}
