package externaldata

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"iot-platform/internal/netguard"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureHTTP(rawURL string) (Source, Endpoint) {
	u, _ := url.Parse(rawURL)
	return Source{ID: "s1", Name: "视频平台", Username: "admin", AllowedHosts: []string{u.Host}}, Endpoint{ID: "e1", SourceID: "s1", Name: "告警查询", Mode: "pull", Kind: "video_alarm", URL: rawURL, Mapping: Mapping{Fields: []Field{{Target: "id", Path: "id"}, {Target: "timestamp", Path: "timestamp"}}}}
}

func TestHTTPAuthenticationsAndTemplates(t *testing.T) {
	secret := `secret-"value`
	for _, kind := range []string{"none", "bearer", "api_key_header", "api_key_query", "basic", "hmac"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Query().Get("from") != "1000" || r.URL.Query().Get("to") != "2000" {
					t.Errorf("unexpected request method or query")
				}
				body, _ := io.ReadAll(r.Body)
				var parsed map[string]any
				if json.Unmarshal(body, &parsed) != nil || parsed["from"] != float64(1000) || parsed["nested"].(map[string]any)["limit"] != float64(100) {
					t.Errorf("body template: %s", body)
				}
				switch kind {
				case "bearer":
					if r.Header.Get("Authorization") != "Bearer "+secret {
						t.Error("bearer")
					}
				case "api_key_header":
					if r.Header.Get("X-Key") != secret {
						t.Error("header key")
					}
				case "api_key_query":
					if r.URL.Query().Get("key") != secret {
						t.Error("query key")
					}
				case "basic":
					u, p, ok := r.BasicAuth()
					if !ok || u != "user" || p != secret {
						t.Error("basic")
					}
				case "hmac":
					stamp := r.Header.Get("X-Timestamp")
					ts, err := strconv.ParseInt(stamp, 10, 64)
					if err != nil || time.Now().Unix()-ts > 5 {
						t.Error("timestamp")
					}
					mac := hmac.New(sha256.New, []byte(secret))
					mac.Write([]byte(stamp))
					mac.Write(body)
					if r.Header.Get("X-Signature") != hex.EncodeToString(mac.Sum(nil)) {
						t.Error("hmac")
					}
				}
				fmt.Fprint(w, `[{"id":"a","timestamp":1000}]`)
			}))
			defer server.Close()
			s, e := fixtureHTTP(server.URL)
			s.Auth = Auth{Type: kind, Secret: secret}
			if kind == "api_key_header" {
				s.Auth.Type = "api_key"
				s.Auth.Header = "X-Key"
			}
			if kind == "api_key_query" {
				s.Auth.Type = "api_key"
				s.Auth.Query = "key"
			}
			if kind == "basic" {
				s.Auth.Username = "user"
			}
			e.Method = http.MethodPost
			e.Query = map[string]string{"from": "{{from}}", "to": "{{to}}"}
			e.RequestBody = map[string]any{"from": "{{from}}", "nested": map[string]any{"limit": "{{pageSize}}"}}
			result, err := NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{From: 1000, To: 2000})
			if err != nil || len(result.Items) != 1 || !result.Done {
				t.Fatalf("fetch: %+v %v", result, err)
			}
		})
	}
}

func TestHTTPTokenCacheAndRefresh(t *testing.T) {
	var logins atomic.Int32
	var revoked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			body, _ := io.ReadAll(r.Body)
			var value map[string]any
			json.Unmarshal(body, &value)
			if value["password"] != `p"word` {
				t.Errorf("secret template incorrectly escaped: %s", body)
			}
			n := logins.Add(1)
			fmt.Fprintf(w, `{"data":{"token":"t%d","expires":3600}}`, n)
			return
		}
		if revoked.Load() && r.Header.Get("Authorization") == "Bearer t1" {
			http.Error(w, "secret-body", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer t") {
			t.Error("missing token")
		}
		fmt.Fprint(w, `[{"id":"a","timestamp":1000}]`)
	}))
	defer server.Close()
	s, e := fixtureHTTP(server.URL + "/events")
	s.Auth = Auth{Type: "token", Secret: `p"word`, TokenURL: server.URL + "/login", TokenBody: map[string]any{"password": "{{secret}}"}, TokenPath: "data.token", TokenExpiresPath: "data.expires"}
	c := NewHTTPClient(netguard.Loopback)
	for i := 0; i < 2; i++ {
		if _, err := c.Fetch(context.Background(), s, e, Job{}); err != nil {
			t.Fatal(err)
		}
	}
	if logins.Load() != 1 {
		t.Fatal("token not cached")
	}
	revoked.Store(true)
	if _, err := c.Fetch(context.Background(), s, e, Job{}); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 {
		t.Fatal("401 did not refresh token")
	}
}

func TestHTTPPaginationBoundaries(t *testing.T) {
	for _, mode := range []string{"page", "offset", "cursor"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("pageSize") != "2" {
					t.Error("page size")
				}
				value := r.URL.Query().Get(mode)
				if value == "1" && mode == "page" || value == "0" && mode == "offset" || value == "" && mode == "cursor" {
					fmt.Fprint(w, `{"items":[{"id":"a","timestamp":1000},{"id":"b","timestamp":1000}],"total":3,"next":"second"}`)
				} else {
					fmt.Fprint(w, `{"items":[{"id":"c","timestamp":1000}],"total":3,"next":""}`)
				}
			}))
			defer server.Close()
			s, e := fixtureHTTP(server.URL)
			e.Mapping.ItemsPath = "items"
			e.Pagination = Pagination{Mode: mode, PageSize: 2, TotalPath: "total", NextPath: "next"}
			c := NewHTTPClient(netguard.Loopback)
			result, err := c.Fetch(context.Background(), s, e, Job{})
			if err != nil || result.Done || len(result.Items) != 2 {
				t.Fatalf("first page: %+v %v", result, err)
			}
			result, err = c.Fetch(context.Background(), s, e, Job{Page: 2, Pages: 1, Received: 2, Cursor: result.NextCursor})
			if err != nil || !result.Done || len(result.Items) != 1 {
				t.Fatalf("last page: %+v %v", result, err)
			}
		})
	}
	for _, tc := range []struct {
		name, body string
		p          Pagination
		j          Job
	}{
		{"stuck cursor", `{"items":[{}],"next":"same"}`, Pagination{Mode: "cursor", NextPath: "next"}, Job{Cursor: "same"}},
		{"empty next", `{"items":[],"next":"next"}`, Pagination{Mode: "cursor", NextPath: "next"}, Job{}},
		{"missing total", `{"items":[]}`, Pagination{Mode: "page", TotalPath: "total"}, Job{}},
		{"total gap", `{"items":[],"total":2}`, Pagination{Mode: "page", TotalPath: "total"}, Job{}},
		{"max pages", `{"items":[{}],"total":2}`, Pagination{Mode: "page", TotalPath: "total", PageSize: 1, MaxPages: 1}, Job{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			s, e := fixtureHTTP(server.URL)
			e.Mapping.ItemsPath = "items"
			e.Pagination = tc.p
			if _, err := NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, tc.j); err == nil {
				t.Fatal("accepted invalid pagination")
			}
		})
	}
}

func TestHTTPAllowedHostsRedirectAndRedactedErrors(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer server.Close()
	s, e := fixtureHTTP(server.URL)
	s.Auth = Auth{Type: "bearer", Secret: "PRIVATE-CREDENTIAL"}
	u, _ := url.Parse(other.URL)
	s.AllowedHosts = append(s.AllowedHosts, u.Host)
	_, err := NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{})
	if err == nil || leaked.Load() {
		t.Fatalf("cross-origin request made: %v", err)
	}
	if strings.Contains(err.Error(), s.Auth.Secret) || strings.Contains(err.Error(), server.URL) {
		t.Fatal("error leaked request details")
	}
	s.AllowedHosts = []string{"example.com:443"}
	if _, err = NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{}); err == nil {
		t.Fatal("allowlist not enforced")
	}
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", MaxBodyBytes+1)) }))
	defer oversized.Close()
	s, e = fixtureHTTP(oversized.URL)
	if _, err = NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{}); err == nil {
		t.Fatal("unbounded response")
	}
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "PRIVATE-CREDENTIAL", 500) }))
	defer errServer.Close()
	s, e = fixtureHTTP(errServer.URL)
	_, err = NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{})
	if err == nil || strings.Contains(err.Error(), "PRIVATE-CREDENTIAL") {
		t.Fatalf("error body leaked: %v", err)
	}
}

func TestHTTPTimeoutAndEndpointAuthOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("source auth inherited despite override")
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	s, e := fixtureHTTP(server.URL)
	s.Auth = Auth{Type: "bearer", Secret: "source"}
	e.Auth = &Auth{Type: "none"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := NewHTTPClient(netguard.Loopback).Fetch(ctx, s, e, Job{}); err == nil {
		t.Fatal("timeout not enforced")
	}
}

func TestHTTPPaginationInJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query pagination: %s", r.URL.RawQuery)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["page"] != float64(1) || body["size"] != float64(25) {
			t.Errorf("body pagination: %#v", body)
		}
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	s, e := fixtureHTTP(server.URL)
	e.Method = http.MethodPost
	e.Pagination = Pagination{Mode: "page", PageSize: 25}
	e.RequestBody = map[string]any{"page": "{{page}}", "size": "{{pageSize}}"}
	if _, err := NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPRetainsDataResponseWhenValidationFails(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		mapping    Mapping
		pagination Pagination
	}{
		{name: "malformed JSON", body: `<html>temporary upstream failure</html>`},
		{name: "success condition", body: `{"code":9,"items":[]}`, mapping: Mapping{ItemsPath: "items", SuccessPath: "code", SuccessValue: 0}},
		{name: "missing items", body: `{"changed":[]}`, mapping: Mapping{ItemsPath: "items"}},
		{name: "pagination metadata", body: `{"items":[{}]}`, mapping: Mapping{ItemsPath: "items"}, pagination: Pagination{Mode: "page", TotalPath: "total"}},
		{name: "invalid cursor", body: `{"items":[{}],"next":{}}`, mapping: Mapping{ItemsPath: "items"}, pagination: Pagination{Mode: "cursor", NextPath: "next"}},
		{name: "http error", body: `{"error":"upstream unavailable"}`, status: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			s, e := fixtureHTTP(server.URL)
			tc.mapping.Fields = e.Mapping.Fields
			e.Mapping = tc.mapping
			e.Pagination = tc.pagination
			result, err := NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{})
			if err == nil || string(result.Body) != tc.body || len(result.Items) != 0 || result.NextCursor != "" || result.Done {
				t.Fatalf("failed response lost or marked consumable: %+v %v", result, err)
			}
		})
	}
}

func TestHTTPNeverRetainsTokenEndpointResponse(t *testing.T) {
	for _, body := range []string{`{"error":"secret-password"}`, `{"token":"secret-token","expires":"invalid"}`, `secret-invalid-json`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/login" {
					t.Error("data request must not run when token acquisition fails")
				}
				io.WriteString(w, body)
			}))
			defer server.Close()
			s, e := fixtureHTTP(server.URL + "/data")
			s.Auth = Auth{Type: "token", Secret: "credential", TokenURL: server.URL + "/login", TokenPath: "token", TokenExpiresPath: "expires"}
			result, err := NewHTTPClient(netguard.Loopback).Fetch(context.Background(), s, e, Job{})
			if err == nil || len(result.Body) != 0 || strings.Contains(err.Error(), "secret-") {
				t.Fatalf("token response leaked: %q %v", result.Body, err)
			}
		})
	}
}

func TestRetryAfterParsesSecondsAndHTTPDate(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{{"120", 2 * time.Minute}, {now.Add(5 * time.Minute).UTC().Format(http.TimeFormat), 5 * time.Minute}, {"invalid", time.Minute}, {"0", time.Second}, {"999999999", 24 * time.Hour}} {
		if got := retryAfterDeadline(tc.header, now); got != now.Add(tc.want).UnixMilli() {
			t.Fatalf("Retry-After %s: %d", tc.header, got)
		}
	}
}

// Allowed host:port pairs name the partner system; they must not let an
// interface reach platform services, cloud metadata or other internal
// addresses unless IOT_EXTERNAL_DATA_ALLOWED_CIDRS lists the network.
func TestClientRefusesInternalTargetsByDefault(t *testing.T) {
	var reached atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true); fmt.Fprint(w, `[]`) }))
	defer server.Close()
	s, e := fixtureHTTP(server.URL + "/events")
	if _, err := NewHTTPClient(netguard.Policy{}).Fetch(context.Background(), s, e, Job{}); err == nil || reached.Load() {
		t.Fatal("the default policy reached a loopback service")
	}
	for _, target := range []string{"http://169.254.169.254/latest/meta-data", "http://100.100.100.200/latest/meta-data"} {
		u, _ := url.Parse(target)
		s.AllowedHosts = []string{hostPort(u)}
		e.URL = target
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := NewHTTPClient(netguard.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("100.64.0.0/10")}}).Fetch(ctx, s, e, Job{})
		cancel()
		if err == nil {
			t.Fatalf("%s was reachable", target)
		}
	}
}
