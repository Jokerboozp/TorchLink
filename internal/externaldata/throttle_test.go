package externaldata_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/externaldata"
)

func TestOutgoingThrottleCoordinatesReplicasAndEndpoints(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `[{"eventId":"event","time":1,"text":"告警"}]`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s, source := runtimeFixture(t, nil, u.Host)
	source.RequestIntervalMillis = 60000
	var err error
	source, err = s.SaveSource(context.Background(), testTenant, source)
	if err != nil {
		t.Fatal(err)
	}
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL, RequestIntervalMillis: 60000})
	other := runtimeService(t, s.Store, nil)
	var accepted, limited atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			service := s
			if i%2 == 0 {
				service = other
			}
			_, err := service.FetchPreview(context.Background(), testTenant, ep, 1, 2)
			var rate *externaldata.RateLimitError
			if err == nil {
				accepted.Add(1)
			} else if errors.As(err, &rate) {
				limited.Add(1)
			} else {
				t.Errorf("unexpected request error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 || limited.Load() != 19 || requests.Load() != 1 {
		t.Fatalf("replicas escaped shared source window: accepted=%d limited=%d calls=%d", accepted.Load(), limited.Load(), requests.Load())
	}
	// Advancing only the source window still leaves the endpoint reservation.
	entry, _ := s.Store.Get(context.Background(), testTenant, "rate_limit", source.ID)
	var state map[string]any
	json.Unmarshal(entry.Body, &state)
	state["nextSourceAt"] = 0
	entry.Body, _ = json.Marshal(state)
	s.Store.Put(context.Background(), entry, entry.Revision)
	if _, err = other.FetchPreview(context.Background(), testTenant, ep, 1, 2); !errors.Is(err, externaldata.ErrRateLimited) {
		t.Fatalf("endpoint reservation lost: %v", err)
	}
	ep2 := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL})
	if _, err = other.FetchPreview(context.Background(), testTenant, ep2, 1, 2); err != nil {
		t.Fatalf("independent endpoint blocked after source window elapsed: %v", err)
	}
}

func TestOutgoingRetryAfterPersistsWithoutConsumingAttempts(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":"too many requests"}`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s, source := runtimeFixture(t, nil, u.Host)
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL})
	job, err := s.Pull(context.Background(), testTenant, ep, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := s.Step(context.Background(), "job", "worker-1"); !worked || err != nil {
		t.Fatalf("first request: %v %v", worked, err)
	}
	stored, _ := s.Store.Get(context.Background(), testTenant, "job", job.ID)
	state := runtimeBody[externaldata.Job](t, stored)
	if stored.Status != "RETRY" || stored.DueAt < time.Now().Add(115*time.Second).UnixMilli() || state.Attempts != 0 || state.Pages != 0 {
		t.Fatalf("Retry-After was not deferred intact: %+v %+v", stored, state)
	}
	if receipts := runtimeEntries(t, s, "receipt"); len(receipts) != 1 || runtimeBody[externaldata.Receipt](t, receipts[0]).Error == "" {
		t.Fatalf("429 business response not retained: %+v", receipts)
	}
	other := runtimeService(t, s.Store, nil)
	_, err = other.FetchPreview(context.Background(), testTenant, ep, 1, 2)
	var rate *externaldata.RateLimitError
	if !errors.As(err, &rate) || rate.RetryAfterSeconds() < 115 || requests.Load() != 1 {
		t.Fatalf("restart lost source cooldown: %v requests=%d", err, requests.Load())
	}
	manual, err := other.Pull(context.Background(), testTenant, ep, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := other.Step(context.Background(), "job", "worker-2"); !worked || err != nil {
		t.Fatalf("manual throttle: %v %v", worked, err)
	}
	stored, _ = s.Store.Get(context.Background(), testTenant, "job", manual.ID)
	state = runtimeBody[externaldata.Job](t, stored)
	if state.Attempts != 0 || stored.Status != "RETRY" || requests.Load() != 1 {
		t.Fatalf("manual request bypassed cooldown or consumed retry: %+v %+v", stored, state)
	}
}

func TestOutgoingTokenRefreshSharesWindowWithoutStaleTokenLoop(t *testing.T) {
	var logins, calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			n := logins.Add(1)
			fmt.Fprintf(w, `{"token":"token-%d"}`, n)
			return
		}
		calls.Add(1)
		if r.Header.Get("Authorization") == "Bearer token-1" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"expired"}`)
			return
		}
		fmt.Fprint(w, `[{"eventId":"event","time":1,"text":"告警"}]`)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	s, source := runtimeFixture(t, nil, u.Host)
	source.Auth = externaldata.Auth{Type: "token", Secret: "login-credential", TokenURL: server.URL + "/login", TokenPath: "token"}
	source.RequestIntervalMillis = 60000
	source, err := s.SaveSource(context.Background(), testTenant, source)
	if err != nil {
		t.Fatal(err)
	}
	ep := runtimeEndpoint(t, s, source, externaldata.Endpoint{Mode: "pull", URL: server.URL + "/events"})
	// First request obtains a token; the data request waits for the same window.
	if _, err = s.FetchPreview(context.Background(), testTenant, ep, 1, 2); !errors.Is(err, externaldata.ErrRateLimited) {
		t.Fatalf("token login escaped throttle: %v", err)
	}
	for i := 0; i < 2; i++ {
		runtimeExpireRequestWindow(t, s)
		if _, err = s.FetchPreview(context.Background(), testTenant, ep, 1, 2); !errors.Is(err, externaldata.ErrRateLimited) {
			t.Fatalf("expected deferred refresh step %d: %v", i, err)
		}
	}
	runtimeExpireRequestWindow(t, s)
	if _, err = s.FetchPreview(context.Background(), testTenant, ep, 1, 2); err != nil {
		t.Fatalf("refreshed token remained stuck: %v", err)
	}
	if logins.Load() != 2 || calls.Load() != 2 {
		t.Fatalf("wrong refresh sequence: logins=%d calls=%d", logins.Load(), calls.Load())
	}
}
