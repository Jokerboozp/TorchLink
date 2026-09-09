package httpapi

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

// Execution endpoints are advertised by configured runtimes through PostgreSQL,
// never taken from a request body or arbitrary redirect supplied by the device.
func (s *Server) executionTarget(r *http.Request) string {
	if !s.cfg.AccessCoordination {
		return ""
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	claim, err := s.auth.Parse(token)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var profile string
	if len(parts) >= 5 && parts[1] == "v2" && parts[2] == "device-access-profiles" {
		profile = parts[3]
	}
	if len(parts) == 5 && parts[1] == "v1" && parts[2] == "device-registry" && (parts[4] == "connection" || parts[4] == "commands") {
		d, err := s.engine.Repo.GetManagedDevice(r.Context(), claim.TenantID, parts[3])
		if err != nil {
			return ""
		}
		profile = d.Tags["connectorProfileId"]
		if profile == "" {
			profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), claim.TenantID)
			if err != nil {
				return ""
			}
			for _, p := range profiles {
				if p.DeviceID == d.ID {
					if profile != "" {
						return ""
					}
					profile = p.ID
				}
			}
		}
	}
	if profile == "" {
		return ""
	}
	lease, err := s.engine.Repo.GetExecutionLease(r.Context(), claim.TenantID, "profile/"+profile)
	if err != nil || lease.Endpoint == "" || lease.Endpoint == s.cfg.AccessNodeURL {
		return ""
	}
	return lease.Endpoint
}
func executionProxy(w http.ResponseWriter, r *http.Request, origin string) {
	hops, _ := strconv.Atoi(r.Header.Get("X-Iot-Gateway-Hops"))
	if hops >= 2 {
		problem(w, 503, "execution routing changed; retry status lookup")
		return
	}
	target, err := url.Parse(origin)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil {
		problem(w, 503, "invalid execution endpoint")
		return
	}
	forwarded := r.Clone(r.Context())
	forwarded.Header.Set("X-Iot-Gateway-Hops", strconv.Itoa(hops+1))
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		problem(w, 503, "execution node unavailable; command result is unconfirmed")
	}
	proxy.ServeHTTP(w, forwarded)
}
