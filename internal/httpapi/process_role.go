package httpapi

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Access routes execute on the process which owns transport sessions. Authentication
// is preserved and evaluated by the same API handlers at the destination.
func accessRoute(path string) bool {
	if strings.HasPrefix(path, "/api/v1/edge/") || strings.HasPrefix(path, "/api/v1/device-ingest/") || path == "/api/v1/device-mqtt/token" || path == "/api/v1/connectors" || path == "/api/v1/onboarding" || path == "/api/v1/onboarding/test" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/device-registry/") && (strings.HasSuffix(path, "/connection") || strings.HasSuffix(path, "/commands")) {
		return true
	}
	return strings.HasPrefix(path, "/api/v2/device-access-profiles/") && (strings.HasSuffix(path, "/commands") || strings.HasSuffix(path, "/sessions") || strings.HasSuffix(path, "/test"))
}
func (s *Server) roleHandler() http.Handler {
	if s.cfg.ProcessRole != "api" && s.cfg.ProcessRole != "gateway" && !s.cfg.AccessCoordination {
		return s.router
	}
	local := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if target := s.executionTarget(r); target != "" {
			executionProxy(w, r, target)
			return
		}
		s.router.ServeHTTP(w, r)
	})
	if s.cfg.ProcessRole == "gateway" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !accessRoute(r.URL.Path) && !strings.HasPrefix(r.URL.Path, "/health/") {
				problem(w, 404, "route is not served by access gateway")
				return
			}
			local.ServeHTTP(w, r)
		})
	}
	if s.cfg.ProcessRole != "api" {
		return local
	}
	target, err := url.Parse(s.cfg.AccessGatewayURL)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { problem(w, 503, "access gateway is not configured") })
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		problem(w, 503, "access gateway is unavailable; operation was not confirmed")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessRoute(r.URL.Path) {
			proxy.ServeHTTP(w, r)
			return
		}
		local.ServeHTTP(w, r)
	})
}
