package httpapi

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"iot-platform/internal/ratelimit"
)

// Failed logins per account (tenant and username) within loginFailureWindow;
// reaching loginMaxFailures locks the account for loginLockout. Accounts,
// not client addresses, are counted because browsers usually reach the API
// through one reverse proxy address.
const (
	loginMaxFailures   = 10
	loginFailureWindow = 15 * time.Minute
	loginLockout       = 15 * time.Minute
)

// loginLimiter counts failed logins per account in fixed windows of
// loginFailureWindow; reaching loginMaxFailures locks the account until the
// window ends (at most loginLockout). With a cluster-wide limiter every API
// replica sees the same count.
type loginLimiter struct {
	limiter ratelimit.Limiter
	now     func() time.Time
}

func newLoginLimiter(shared ratelimit.Limiter) *loginLimiter {
	l := &loginLimiter{limiter: shared, now: time.Now}
	if l.limiter == nil {
		local := ratelimit.NewLocal()
		local.Now = func() time.Time { return l.now() }
		l.limiter = local
	}
	return l
}

func loginKey(account string) string { return "login\x00" + account }

// retryAfter reports how long the account stays locked; zero means allowed.
func (l *loginLimiter) retryAfter(account string) time.Duration {
	if l == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	n, reset, err := l.limiter.Hits(ctx, loginKey(account), loginFailureWindow)
	if err != nil || n < loginMaxFailures {
		return 0
	}
	return min(reset, loginLockout)
}

func (l *loginLimiter) record(account string, success bool) {
	if l == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if success {
		_ = l.limiter.Reset(ctx, loginKey(account), loginFailureWindow)
		return
	}
	_, _ = l.limiter.Allow(ctx, loginKey(account), math.MaxInt32, loginFailureWindow)
}

// statusRecorder remembers the status a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func lockedOut(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	problem(w, http.StatusTooManyRequests, "登录失败次数过多，请稍后再试")
}
