package externaldata

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitError is a deferred request, not a failed business-processing attempt.
type RateLimitError struct {
	RetryAt int64
	Remote  bool
}

func (e *RateLimitError) Error() string {
	if e.Remote {
		return "外部接口返回 429，已按 Retry-After 延后请求"
	}
	return "已达到来源请求频率限制，已延后请求"
}
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }
func (e *RateLimitError) RetryAfterSeconds() int64 {
	return max(1, (e.RetryAt-time.Now().UnixMilli()+999)/1000)
}

type throttleState struct {
	NextSourceAt int64 `json:"nextSourceAt"`
}
type requestGateKey struct{}
type requestGate struct {
	WaitBriefly bool
	Acquire     func(context.Context) error
	Cooldown    func(context.Context, int64) error
}

func configuredRequestInterval(ms int) int64 {
	if ms == 0 {
		return 1000
	}
	return int64(ms)
}

// All endpoints of a source share one CAS row, so replicas cannot exceed the
// source's request budget or lose a Retry-After cooldown.
func (s *Service) reserveRequest(ctx context.Context, tenant string, src Source, ep Endpoint, cooldown int64) error {
	if tenant == "" || src.ID == "" || ep.ID == "" || ep.SourceID != src.ID {
		return invalid("请求节流缺少有效来源或接口归属")
	}
	for attempt := 0; attempt < 16; attempt++ {
		entry, err := s.Store.Get(ctx, tenant, "rate_limit", src.ID)
		if errors.Is(err, ErrNotFound) {
			entry = Entry{TenantID: tenant, Kind: "rate_limit", ID: src.ID, SourceID: src.ID, Body: body(throttleState{})}
		} else if err != nil {
			return err
		}
		state, err := read[throttleState](entry)
		if err != nil {
			return errors.New("读取请求节流状态失败")
		}
		now := time.Now().UnixMilli()
		if cooldown == 0 {
			if state.NextSourceAt > now {
				return &RateLimitError{RetryAt: state.NextSourceAt}
			}
			state.NextSourceAt = now + configuredRequestInterval(src.RequestIntervalMillis)
		} else {
			state.NextSourceAt = max(state.NextSourceAt, cooldown)
		}
		entry.Body = body(state)
		if _, err = s.Store.Put(ctx, entry, entry.Revision); errors.Is(err, ErrConflict) {
			continue
		} else {
			return err
		}
	}
	return &RateLimitError{RetryAt: time.Now().Add(time.Second).UnixMilli()}
}

func (s *Service) fetch(ctx context.Context, tenant string, src Source, ep Endpoint, j Job, preview bool) (FetchResult, error) {
	gate := requestGate{
		WaitBriefly: preview,
		Acquire:     func(ctx context.Context) error { return s.reserveRequest(ctx, tenant, src, ep, 0) },
		Cooldown:    func(ctx context.Context, until int64) error { return s.reserveRequest(ctx, tenant, src, ep, until) },
	}
	return s.client.Fetch(context.WithValue(ctx, requestGateKey{}, gate), src, ep, j)
}

func retryAfterDeadline(header string, now time.Time) int64 {
	delay := time.Minute
	if seconds, err := strconv.ParseInt(strings.TrimSpace(header), 10, 64); err == nil && seconds >= 0 {
		delay = time.Duration(min(seconds, 86400)) * time.Second
	} else if at, err := http.ParseTime(header); err == nil {
		delay = at.Sub(now)
	}
	return now.Add(max(time.Second, min(delay, 24*time.Hour))).UnixMilli()
}
