package httpapi

import (
	"testing"
	"time"
)

// Repeated wrong passwords lock the account for a while; a success before the
// limit clears the count, and the lock ends on its own.
func TestLoginLimiterLocksAccountAfterRepeatedFailures(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newLoginLimiter()
	l.now = func() time.Time { return now }
	for i := 0; i < loginMaxFailures-1; i++ {
		l.record("tenant\x00admin", false)
	}
	l.record("tenant\x00admin", true)
	for i := 0; i < loginMaxFailures-1; i++ {
		l.record("tenant\x00admin", false)
	}
	if l.retryAfter("tenant\x00admin") != 0 {
		t.Fatal("a success must reset earlier failures")
	}
	l.record("tenant\x00admin", false)
	if l.retryAfter("tenant\x00admin") <= 0 {
		t.Fatal("the account must be locked after the limit")
	}
	if l.retryAfter("tenant\x00other") != 0 {
		t.Fatal("other accounts must not be affected")
	}
	now = now.Add(loginLockout + time.Second)
	if l.retryAfter("tenant\x00admin") != 0 {
		t.Fatal("the lock must expire")
	}
}
