package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Failed logins per account (tenant and username) within loginFailureWindow;
// reaching loginMaxFailures locks the account for loginLockout. Accounts,
// not client addresses, are counted because browsers usually reach the API
// through one reverse proxy address.
const (
	loginMaxFailures   = 10
	loginFailureWindow = 15 * time.Minute
	loginLockout       = 15 * time.Minute
	loginTrackedLimit  = 100000
)

type loginAttempts struct {
	failures    int
	firstAt     time.Time
	lockedUntil time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	accounts map[string]loginAttempts
	now      func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{accounts: map[string]loginAttempts{}, now: time.Now}
}

// retryAfter reports how long the account stays locked; zero means allowed.
func (l *loginLimiter) retryAfter(account string) time.Duration {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if wait := l.accounts[account].lockedUntil.Sub(l.now()); wait > 0 {
		return wait
	}
	return 0
}

func (l *loginLimiter) record(account string, success bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if success {
		delete(l.accounts, account)
		return
	}
	a := l.accounts[account]
	if now.Sub(a.firstAt) > loginFailureWindow {
		a = loginAttempts{firstAt: now}
	}
	a.failures++
	if a.failures >= loginMaxFailures {
		a.lockedUntil = now.Add(loginLockout)
		a.failures, a.firstAt = 0, now
	}
	if len(l.accounts) >= loginTrackedLimit {
		for key, v := range l.accounts {
			if now.Sub(v.firstAt) > loginFailureWindow && now.After(v.lockedUntil) {
				delete(l.accounts, key)
			}
		}
	}
	l.accounts[account] = a
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
