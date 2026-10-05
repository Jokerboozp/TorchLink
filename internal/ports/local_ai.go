package ports

import (
	"net"
	"net/url"
	"strings"
)

// DefaultLocalAIHosts are the vector and rerank services deployed with the
// platform (Compose service names).
const DefaultLocalAIHosts = "embedding,reranker"

// LocalAIEndpoint reports whether rawURL addresses a model service the
// deployment runs itself: its host is listed in hosts (names, IPs or CIDRs,
// comma separated). Such a service may use plain HTTP, a private address and
// no API key; every other model endpoint must be an external HTTPS API.
func LocalAIEndpoint(rawURL, hosts string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	for _, entry := range strings.Split(hosts, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		switch {
		case entry == "":
		case entry == host:
			return true
		case ip != nil && strings.Contains(entry, "/"):
			if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
				return true
			}
		}
	}
	return false
}
