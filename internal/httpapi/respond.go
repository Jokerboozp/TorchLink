package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/devicescope"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"iot-platform/internal/auth"
	"iot-platform/internal/model"
)

func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (s *Server) audit(r *http.Request, action, targetType, targetID string, details map[string]any) {
	c := claims(r)
	s.engine.RecordAudit(r.Context(), model.AuditLog{ID: "audit_" + randomHex(10), TenantID: c.TenantID, Actor: c.Username, Action: action, TargetType: targetType, TargetID: targetID, Details: details, CreatedAt: time.Now().UnixMilli()})
}

// Machine-readable error codes in problem responses. Callers decide by code
// rather than by the Chinese detail text, which may change.
const (
	codeRoleDenied        = "ROLE_DENIED"
	codeDeviceScopeDenied = "DEVICE_SCOPE_DENIED"
)

func ginProblemCode(c *gin.Context, status int, code, detail string) {
	c.JSON(status, gin.H{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}

func problemCode(w http.ResponseWriter, status int, code, detail string) {
	write(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}

func ginProblem(c *gin.Context, status int, detail string) {
	c.JSON(status, gin.H{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail})
}

func claims(r *http.Request) auth.Claims {
	v, _ := r.Context().Value(claimsKey).(auth.Claims)
	return v
}

func tenant(c auth.Claims, requested string) string {
	if requested == "" || requested == c.TenantID {
		return c.TenantID
	}
	return c.TenantID
}

func adminTenantAllowed(configured []string, requested string) bool {
	if len(configured) == 0 {
		configured = []string{"tenant_001"}
	}
	requested = strings.TrimSpace(requested)
	for _, tenantID := range configured {
		if strings.TrimSpace(tenantID) == requested {
			return true
		}
	}
	return false
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		problem(w, 400, "invalid request: "+err.Error())
		return err
	}
	return nil
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// API responses contain tenant-scoped, mutable state. Prevent browsers and
	// reverse proxies from serving a stale workflow catalog after a mutation.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// internalError answers an unexpected failure without exposing its text:
// storage and dependency errors can carry hosts, SQL or credentials. The
// reference in the response matches the logged error.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	reference := requestIDFrom(r.Context())
	if reference == "" {
		reference = randomHex(6)
	}
	s.log.ErrorContext(r.Context(), "request failed", "reference", reference, "method", r.Method, "path", r.URL.Path, "error", err)
	write(w, http.StatusInternalServerError, map[string]any{"type": "about:blank", "title": http.StatusText(http.StatusInternalServerError), "status": http.StatusInternalServerError, "detail": "服务内部错误，请稍后重试；如持续出现请提供编号 " + reference + " 联系管理员", "traceId": reference})
}

// statusError is a domain error that chooses its own HTTP status, such as an
// onboarding.EnrollError.
type statusError interface {
	error
	StatusCode() int
}

// fail answers a handler error by its category: a device outside the
// request's scope (404 for reads, 403 for changes), an error carrying its own
// status, then model.ErrNotFound (404), model.ErrInvalid (422),
// model.ErrConflict (409), model.ErrForbidden (403) and model.ErrUnavailable
// (503). Anything else is unexpected: it is logged and answered 500 with
// detail and a reference (see failure), or the generic message when detail is
// empty (see internalError).
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error, detail string) {
	var own statusError
	var domain *model.Error
	switch {
	case errors.Is(err, devicescope.ErrDenied):
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			problem(w, http.StatusNotFound, devicescope.ErrDenied.Error())
		} else {
			problemCode(w, http.StatusForbidden, codeDeviceScopeDenied, devicescope.ErrDenied.Error())
		}
	case errors.As(err, &own):
		problem(w, own.StatusCode(), own.Error())
	case errors.As(err, &domain):
		problem(w, categoryStatus(domain.Category), domain.Message)
	case errors.Is(err, model.ErrNotFound):
		problem(w, http.StatusNotFound, "资源不存在或无访问权限")
	case errors.Is(err, model.ErrInvalid), errors.Is(err, model.ErrConflict), errors.Is(err, model.ErrForbidden), errors.Is(err, model.ErrUnavailable):
		problem(w, categoryStatus(err), err.Error())
	case detail == "":
		s.internalError(w, r, err)
	default:
		s.failure(w, r, err, detail)
	}
}

// categoryStatus is the HTTP status of a model error category.
func categoryStatus(err error) int {
	switch {
	case errors.Is(err, model.ErrInvalid):
		return http.StatusUnprocessableEntity
	case errors.Is(err, model.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, model.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, model.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, model.ErrNotFound):
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// failure answers 500 with a Chinese hint and a reference, and logs err under
// that reference so the operator can find the cause ("request failed").
// Handlers use it instead of problem(w, 500, …), which loses the error.
func (s *Server) failure(w http.ResponseWriter, r *http.Request, err error, detail string) {
	if err == nil {
		err = errors.New(detail)
	}
	reference := requestIDFrom(r.Context())
	if reference == "" {
		reference = randomHex(6)
	}
	if s.log != nil {
		s.log.ErrorContext(r.Context(), "request failed", "reference", reference, "method", r.Method, "path", r.URL.Path, "detail", detail, "error", err)
	}
	write(w, http.StatusInternalServerError, map[string]any{"type": "about:blank", "title": http.StatusText(http.StatusInternalServerError), "status": http.StatusInternalServerError, "detail": detail + "（编号 " + reference + "）", "traceId": reference})
}

func problem(w http.ResponseWriter, status int, detail string) {
	write(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail})
}

func i64(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }

func intval(v string, d int) int {
	n, e := strconv.Atoi(v)
	if e != nil {
		return d
	}
	return n
}

func cleanStringList(values []string, maximum, maxLength int) []string {
	out := make([]string, 0, min(len(values), maximum))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > maxLength {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if len(out) >= maximum {
			break
		}
	}
	return out
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
