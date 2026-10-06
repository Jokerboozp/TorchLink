package httpapi

import (
	"context"
	"crypto/subtle"
	"fmt"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"iot-platform/internal/auth"
	"iot-platform/internal/logctx"
	"iot-platform/internal/metrics"
)

type endpointHandler func(http.ResponseWriter, *http.Request)

// endpoint adapts the established net/http business handlers to Gin while
// preserving Request.PathValue for code that reads named route parameters.
func (s *Server) endpoint(handler endpointHandler, pathParams ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, name := range pathParams {
			c.Request.SetPathValue(name, c.Param(name))
		}
		handler(c.Writer, c.Request)
	}
}

func (s *Server) authorize(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := auth.Bearer(c.GetHeader("Authorization"))
		claimsValue, err := s.auth.Parse(token)
		if err != nil {
			ginProblem(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		if claimsValue.TokenUse != "" && claimsValue.TokenUse != "user" {
			ginProblem(c, http.StatusForbidden, "此专用凭据不能用于管理接口")
			c.Abort()
			return
		}
		if claimsValue.TokenUse == "" && s.cfg.AdminUser != "" && claimsValue.Username == s.cfg.AdminUser && claimsValue.SessionVersion != s.adminSessionVersion() {
			ginProblem(c, http.StatusUnauthorized, "管理员凭据已更新，请重新登录")
			c.Abort()
			return
		}
		allowed := claimsValue.Role == "admin" || claimsValue.Role == role || role == "viewer" && (claimsValue.Role == "operator" || claimsValue.Role == "viewer")
		if claimsValue.TokenUse == "user" {
			user, permissions, err := s.managedIdentity(c.Request.Context(), claimsValue)
			if err != nil {
				ginProblem(c, 401, "账户已停用或会话已失效，请重新登录")
				c.Abort()
				return
			}
			allowed = allowsRoute(permissions, c.Request.Method, c.FullPath())
			scope := s.scopeFor(user, permissions, claimsValue.TenantID)
			c.Request = c.Request.WithContext(devicescope.With(c.Request.Context(), scope))
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), permissionsKey{}, permissions))
			if allowed {
				// Reads of out-of-scope devices and alarms answer 404, changes 403
				// with DEVICE_SCOPE_DENIED (see scopeDenied); operations that need
				// every device answer 403.
				switch s.scopeRequest(c, scope) {
				case scopeHidden:
					scopeDenied(c.Writer, c.Request, "该资源或操作超出当前账户的设备范围")
					c.Abort()
					return
				case scopeForbidden:
					ginProblemCode(c, http.StatusForbidden, codeDeviceScopeDenied, "该操作需要可访问全部设备的账户")
					c.Abort()
					return
				}
			}
		}
		if !allowed {
			ginProblemCode(c, http.StatusForbidden, codeRoleDenied, "insufficient role")
			c.Abort()
			return
		}
		ctx := auth.ContextWithClaims(context.WithValue(c.Request.Context(), claimsKey, claimsValue), claimsValue)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func (s *Server) authorizeHarness() gin.HandlerFunc {
	allowedScopes := make(map[string]struct{})
	for _, scope := range auth.HarnessReadScopes() {
		allowedScopes[scope] = struct{}{}
	}
	return func(c *gin.Context) {
		token := auth.Bearer(c.GetHeader("Authorization"))
		claimsValue, err := s.harnessAuth.Parse(token)
		if err != nil {
			ginProblem(c, http.StatusUnauthorized, err.Error())
			c.Abort()
			return
		}
		if claimsValue.TokenUse != "harness" || !claimsValue.HasAudience(auth.HarnessAudience) || claimsValue.RunID == "" || claimsValue.TenantID == "" {
			ginProblem(c, http.StatusForbidden, "invalid harness token")
			c.Abort()
			return
		}
		for _, scope := range claimsValue.Scopes {
			if _, ok := allowedScopes[scope]; !ok {
				ginProblem(c, http.StatusForbidden, "invalid harness scope")
				c.Abort()
				return
			}
		}
		if claimsValue.ManagedUser {
			user, permissions, err := s.managedIdentity(c.Request.Context(), claimsValue)
			if err != nil {
				ginProblem(c, http.StatusUnauthorized, "账户已停用或会话已失效，请重新登录")
				c.Abort()
				return
			}
			if claimsValue.Workflow != "" {
				if !businessWorkflowAllowed(permissions, claimsValue.Workflow) {
					ginProblem(c, http.StatusForbidden, "无此智能功能的访问权限")
					c.Abort()
					return
				}
			} else if !chatAllowed(permissions) {
				ginProblem(c, http.StatusForbidden, "无智能助手访问权限")
				c.Abort()
				return
			}
			ctx := devicescope.With(c.Request.Context(), s.scopeFor(user, permissions, claimsValue.TenantID))
			ctx = context.WithValue(ctx, permissionsKey{}, permissions)
			claimsValue.Scopes = intersectScopes(claimsValue.Scopes, workflowScopes(ctx))
			claimsValue.Permissions = permissionList(permissions)
			c.Request = c.Request.WithContext(ctx)
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		ctx := auth.ContextWithClaims(context.WithValue(c.Request.Context(), claimsKey, claimsValue), claimsValue)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func (s *Server) security() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; style-src 'self' 'unsafe-inline'; script-src 'self'; worker-src 'self' blob:")
		c.Next()
	}
}

func (s *Server) cors() gin.HandlerFunc {
	allowedOrigins := make(map[string]struct{}, len(s.cfg.CORSAllowedOrigins))
	for _, origin := range s.cfg.CORSAllowedOrigins {
		allowedOrigins[strings.TrimRight(origin, "/")] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := strings.TrimRight(c.GetHeader("Origin"), "/")
		if origin != "" {
			if _, allowed := allowedOrigins[origin]; allowed {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-Key, X-Device-Secret, X-Video-Platform-ID, X-Timestamp, X-Signature, X-Request-ID")
				c.Header("Access-Control-Expose-Headers", "X-Request-ID")
				c.Header("Access-Control-Max-Age", "600")
				c.Header("Vary", "Origin")
			} else if c.Request.Method == http.MethodOptions {
				ginProblem(c, http.StatusForbidden, "origin is not allowed")
				c.Abort()
				return
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Next()
	}
}

// slowRequest is the duration above which a successful request is still
// logged at info level. Event streams are long-lived by design and excluded.
const slowRequest = 3 * time.Second

// accessLog records failed and slow requests at info level or above.
// Routine successful requests (page polling, Prometheus scrapes, health
// checks) are debug records so they do not flood the collected logs; set
// IOT_LOG_LEVEL=debug to see every request.

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// requestID keeps a caller's or proxy's X-Request-ID, or assigns one, and
// echoes it so logs, error references and the client name the same request.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !validRequestID.MatchString(id) {
			id = randomHex(8)
		}
		c.Header("X-Request-ID", id)
		c.Request = c.Request.WithContext(logctx.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

func requestIDFrom(ctx context.Context) string { return logctx.RequestID(ctx) }

func (s *Server) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status, duration := c.Writer.Status(), time.Since(start)
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		if s.metrics != nil && !strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") {
			s.metrics.ObserveIn(metrics.Series("http_request_duration_seconds", "route", route, "method", c.Request.Method, "code", strconv.Itoa(status)), metrics.RequestBuckets, duration.Seconds())
		}
		if s.log == nil {
			return
		}
		level := slog.LevelDebug
		switch {
		case status >= 500:
			level = slog.LevelWarn
		case status >= 400:
			level = slog.LevelInfo
		case duration >= slowRequest && !strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream"):
			level = slog.LevelInfo
		}
		// requestId, tenantId and user come from the context (logctx).
		s.log.Log(c.Request.Context(), level, "http request", "method", c.Request.Method, "path", c.Request.URL.Path, "route", c.FullPath(), "status", status, "duration", duration.String())
	}
}

func (s *Server) recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// A handler that already started a response aborts it on
			// purpose; net/http then closes the connection quietly.
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			reference := requestIDFrom(c.Request.Context())
			if reference == "" {
				reference = randomHex(6)
			}
			if s.log != nil {
				s.log.Error("http panic recovered", "reference", reference, "method", c.Request.Method, "path", c.Request.URL.Path, "error", fmt.Sprint(recovered), "stack", string(debug.Stack()))
			}
			c.JSON(http.StatusInternalServerError, gin.H{"type": "about:blank", "title": http.StatusText(http.StatusInternalServerError), "status": http.StatusInternalServerError, "detail": "服务内部错误（编号 " + reference + "）", "traceId": reference})
			c.Abort()
		}()
		c.Next()
	}
}

// metricsAuthorized checks the optional IOT_METRICS_TOKEN bearer token.
func (s *Server) metricsAuthorized(r *http.Request) bool {
	if s.cfg.MetricsToken == "" {
		return true
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(s.cfg.MetricsToken)) == 1
}
