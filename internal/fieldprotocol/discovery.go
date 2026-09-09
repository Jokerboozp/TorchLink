package fieldprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"golang.org/x/net/ipv4"
	"io"
	"iot-platform/internal/model"
	"iot-platform/internal/protocolruntime"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const discoveryNS = "http://schemas.xmlsoap.org/ws/2005/04/discovery"
const addressingNS = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
const soapNS = "http://www.w3.org/2003/05/soap-envelope"

type DiscoveryOptions struct {
	InterfaceAddress                string
	AllowedInterfaces, AllowedCIDRs []string
	// Local deployment setting only. The default is link-local WS-Discovery;
	// tests use a loopback simulator, never the operator's physical LAN.
	ProbeAddress string
	Duration     time.Duration
}

func DiscoverONVIF(ctx context.Context, options DiscoveryOptions) (model.ONVIFDiscovery, error) {
	out := model.ONVIFDiscovery{Items: []model.ONVIFCandidate{}}
	ip := net.ParseIP(options.InterfaceAddress)
	allowed := false
	for _, address := range options.AllowedInterfaces {
		if ip != nil && ip.To4() != nil && ip.Equal(net.ParseIP(address)) {
			allowed = true
		}
	}
	if !allowed {
		return out, errors.New("discovery interface is not in the node's local allowlist")
	}
	if options.ProbeAddress == "" {
		options.ProbeAddress = "239.255.255.250:3702"
	}
	target, err := net.ResolveUDPAddr("udp4", options.ProbeAddress)
	if err != nil || target.IP.To4() == nil || target.Port < 1 {
		return out, errors.New("invalid locally configured discovery destination")
	}
	if !target.IP.IsMulticast() {
		if _, err := protocolruntime.ResolveAllowedTarget(ctx, target.IP.String(), options.AllowedCIDRs); err != nil {
			return out, err
		}
	}
	if options.Duration <= 0 || options.Duration > 3*time.Second {
		options.Duration = 3 * time.Second
	}
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
	if err != nil {
		return out, errors.New("cannot bind configured discovery interface")
	}
	defer socket.Close()
	stop := context.AfterFunc(ctx, func() { socket.Close() })
	defer stop()
	if target.IP.IsMulticast() {
		iface, err := interfaceForIP(ip)
		if err != nil {
			return out, err
		}
		packet := ipv4.NewPacketConn(socket)
		if err = packet.SetMulticastInterface(iface); err != nil {
			return out, err
		}
		if err = packet.SetMulticastTTL(1); err != nil {
			return out, err
		}
	}
	id := "urn:uuid:" + uuid.NewString()
	probe := []byte(fmt.Sprintf(`<s:Envelope xmlns:s="%s" xmlns:a="%s" xmlns:d="%s"><s:Header><a:MessageID>%s</a:MessageID><a:To>urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To><a:Action>%s/Probe</a:Action></s:Header><s:Body><d:Probe/></s:Body></s:Envelope>`, soapNS, addressingNS, discoveryNS, id, discoveryNS))
	deadline := time.Now().Add(options.Duration)
	buf := make([]byte, 65536)
	seen := map[string]bool{}
	packets, attempts, total := 0, 0, 0
	nextSend := time.Now()
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if attempts < 3 && !time.Now().Before(nextSend) {
			if _, err = socket.WriteToUDP(probe, target); err != nil {
				return out, errors.New("send discovery probe failed")
			}
			attempts++
			nextSend = time.Now().Add(250 * time.Millisecond)
		}
		_ = socket.SetReadDeadline(minTime(deadline, time.Now().Add(100*time.Millisecond)))
		n, peer, err := socket.ReadFromUDP(buf)
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return out, errors.New("read discovery response failed")
		}
		packets++
		if packets > 256 {
			out.Truncated = true
			break
		}
		if _, err := protocolruntime.ResolveAllowedTarget(ctx, peer.IP.String(), options.AllowedCIDRs); err != nil {
			out.Discarded++
			continue
		}
		items, err := parseProbeMatches(buf[:n], id, peer.IP)
		if err != nil {
			out.Discarded++
			continue
		}
		for _, item := range items {
			key := item.EndpointID + "\x00" + item.SourceIP
			if seen[key] {
				continue
			}
			if len(out.Items) >= 128 {
				out.Truncated = true
				break
			}
			item.ObservedAt = time.Now().UnixMilli()
			encoded, _ := json.Marshal(item)
			if total+len(encoded) > 256<<10 {
				out.Truncated = true
				break
			}
			total += len(encoded)
			seen[key] = true
			out.Items = append(out.Items, item)
		}
		if out.Truncated {
			break
		}
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	sort.Slice(out.Items, func(i, j int) bool {
		return out.Items[i].EndpointID+out.Items[i].SourceIP < out.Items[j].EndpointID+out.Items[j].SourceIP
	})
	return out, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func interfaceForIP(ip net.IP) (*net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range interfaces {
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			value, _, err := net.ParseCIDR(address.String())
			if err == nil && ip.Equal(value) {
				return &iface, nil
			}
		}
	}
	return nil, errors.New("configured discovery address is not a local interface")
}

func parseProbeMatches(data []byte, requestID string, peer net.IP) ([]model.ONVIFCandidate, error) {
	if len(data) == 0 || len(data) > 65507 {
		return nil, errors.New("discovery packet size exceeds limit")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, tokens, roots := 0, 0, 0
	counts := map[string]int{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		tokens++
		if tokens > 4096 {
			return nil, errors.New("discovery XML exceeds complexity limit")
		}
		switch token := token.(type) {
		case xml.Directive:
			return nil, errors.New("discovery XML directives are forbidden")
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return nil, errors.New("multiple discovery envelopes")
				}
			}
			depth++
			if depth > 24 {
				return nil, errors.New("discovery XML nesting limit")
			}
			if token.Name.Space == addressingNS && (token.Name.Local == "RelatesTo" || token.Name.Local == "Action") {
				counts[token.Name.Local]++
				if counts[token.Name.Local] > 1 {
					return nil, errors.New("ambiguous discovery header")
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	var envelope struct {
		XMLName xml.Name `xml:"http://www.w3.org/2003/05/soap-envelope Envelope"`
		Header  struct {
			RelatesTo string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing RelatesTo"`
			Action    string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing Action"`
		} `xml:"http://www.w3.org/2003/05/soap-envelope Header"`
		Body struct {
			Matches struct {
				Items []struct {
					Endpoint struct {
						Address string `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing Address"`
					} `xml:"http://schemas.xmlsoap.org/ws/2004/08/addressing EndpointReference"`
					XAddrs string `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery XAddrs"`
					Scopes string `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery Scopes"`
				} `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery ProbeMatch"`
			} `xml:"http://schemas.xmlsoap.org/ws/2005/04/discovery ProbeMatches"`
		} `xml:"http://www.w3.org/2003/05/soap-envelope Body"`
	}
	if err := xml.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Header.RelatesTo != requestID || envelope.Header.Action != discoveryNS+"/ProbeMatches" || len(envelope.Body.Matches.Items) > 128 {
		return nil, errors.New("uncorrelated or oversized discovery response")
	}
	out := []model.ONVIFCandidate{}
	for _, match := range envelope.Body.Matches.Items {
		endpoint := strings.TrimSpace(match.Endpoint.Address)
		if !strings.HasPrefix(endpoint, "urn:uuid:") {
			continue
		}
		if _, err := uuid.Parse(strings.TrimPrefix(endpoint, "urn:uuid:")); err != nil {
			continue
		}
		candidate := model.ONVIFCandidate{EndpointID: endpoint, SourceIP: peer.String(), XAddrs: []string{}, Scopes: []string{}}
		onvif := false
		for _, scope := range strings.Fields(match.Scopes) {
			if len(candidate.Scopes) >= 32 || len(scope) > 1024 {
				break
			}
			u, err := url.Parse(scope)
			if err != nil {
				continue
			}
			if u.Scheme == "onvif" && u.Host == "www.onvif.org" {
				onvif = true
			}
			candidate.Scopes = append(candidate.Scopes, scope)
		}
		if !onvif {
			continue
		}
		addresses := map[string]bool{}
		for _, address := range strings.Fields(match.XAddrs) {
			if len(candidate.XAddrs) >= 8 {
				break
			}
			if !validDiscoveryAddress(address, peer) || addresses[address] {
				continue
			}
			addresses[address] = true
			candidate.XAddrs = append(candidate.XAddrs, address)
		}
		if len(candidate.XAddrs) > 0 {
			out = append(out, candidate)
		}
	}
	return out, nil
}

func validDiscoveryAddress(address string, peer net.IP) bool {
	u, err := url.Parse(address)
	if err != nil {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	return len(address) <= 2048 && err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil && u.RawQuery == "" && u.Fragment == "" && peer.Equal(net.ParseIP(u.Hostname())) && u.Path != "" && !strings.ContainsAny(address, "\r\n")
}

// ValidateDiscoveryResult also validates authenticated node responses before
// exposing untrusted device-supplied URLs to the operator.
func ValidateDiscoveryResult(result model.ONVIFDiscovery) error {
	data, err := json.Marshal(result)
	if err != nil || len(data) > 300<<10 || result.Authenticated || len(result.Items) > 128 || result.Discarded < 0 || result.Discarded > 256 {
		return errors.New("invalid discovery result")
	}
	for _, item := range result.Items {
		peer := net.ParseIP(item.SourceIP)
		if peer == nil || !strings.HasPrefix(item.EndpointID, "urn:uuid:") || len(item.XAddrs) == 0 || len(item.XAddrs) > 8 || len(item.Scopes) > 32 {
			return errors.New("invalid discovery candidate")
		}
		if _, err := uuid.Parse(strings.TrimPrefix(item.EndpointID, "urn:uuid:")); err != nil {
			return err
		}
		for _, address := range item.XAddrs {
			if !validDiscoveryAddress(address, peer) {
				return errors.New("discovery address differs from response source")
			}
		}
		for _, scope := range item.Scopes {
			if len(scope) > 1024 {
				return errors.New("discovery scope exceeds limit")
			}
		}
	}
	return nil
}
