package httpapi

import (
	"net/http"
	"strings"
)

// SetVideoRouting enables single-executor live video: owner reports whether
// this instance runs the live module and, if not, the owner's node URL. Call
// it before Handler.
func (s *Server) SetVideoRouting(owner func() (local bool, endpoint string)) { s.videoOwner = owner }

func videoControlRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/video/") || strings.HasPrefix(path, "/api/v1/integrations/video/")
}

// videoRouting forwards live video control, hooks and media authorization to
// the instance that holds the video/control lease. Play sessions, SIP state
// and media tasks live only in that instance; user credentials are forwarded
// unchanged and checked again there.
func (s *Server) videoRouting(next http.Handler) http.Handler {
	if s.videoOwner == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !videoControlRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		local, endpoint := s.videoOwner()
		switch {
		case local:
			next.ServeHTTP(w, r)
		case endpoint == "":
			w.Header().Set("Retry-After", "5")
			problem(w, http.StatusServiceUnavailable, "视频控制实例切换中，请稍后重试")
		default:
			executionProxy(w, r, endpoint)
		}
	})
}
