package mqttadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAdminBanBeforeKickAndRetry(t *testing.T) {
	var calls []string
	banned, kicked := false, false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "api" || p != "secret" {
			t.Error("missing API auth")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == "POST":
			var v map[string]any
			json.NewDecoder(r.Body).Decode(&v)
			if v["who"] != "device-key" || v["as"] != "username" || v["until"] != "infinity" {
				t.Error(v)
			}
			if banned {
				w.WriteHeader(400)
				return
			}
			banned = true
			w.WriteHeader(200)
		case r.URL.Path == "/api/v5/banned":
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"as": "username", "who": "device-key", "until": "infinity"}}})
		case r.Method == "GET":
			if !banned {
				t.Error("listed before banned")
			}
			if r.URL.Query().Get("username") != "device-key" {
				t.Error("unscoped lookup")
			}
			if kicked {
				w.Write([]byte(`{"data":[]}`))
			} else {
				w.Write([]byte(`{"data":[{"clientid":"client-1","username":"device-key"}]}`))
			}
		case r.Method == "DELETE":
			kicked = true
			w.WriteHeader(204)
		}
	}))
	defer s.Close()
	a := Admin{URL: s.URL, Key: "api", Secret: "secret"}
	if e := a.RevokeUsername(context.Background(), "device-key"); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(calls, []string{"POST /api/v5/banned", "GET /api/v5/clients", "DELETE /api/v5/clients/client-1", "GET /api/v5/clients"}) {
		t.Fatal(calls)
	}
	if e := a.RevokeUsername(context.Background(), "device-key"); e != nil {
		t.Fatal(e)
	}
}
func TestAdminRejectsUnexpectedClientAndRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "identity", true: "redirect"}[redirect], func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					w.Header().Set("Location", "http://localhost:1")
					w.WriteHeader(302)
					return
				}
				if r.Method == "DELETE" {
					t.Error("wrong user kicked")
				}
				if r.Method == "GET" {
					w.Write([]byte(`{"data":[{"clientid":"x","username":"other"}]}`))
				}
			}))
			defer s.Close()
			a := Admin{URL: s.URL, Key: "a", Secret: "b"}
			if a.RevokeUsername(context.Background(), "device") == nil {
				t.Fatal("unsafe response accepted")
			}
		})
	}
}

func TestAdminSessionQueueReadsBrokerDrops(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v5/clients/iot-inbox-1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"clientid": "iot-inbox-1", "mqueue_len": 1000, "mqueue_dropped": 4639})
	}))
	defer s.Close()
	queued, dropped, err := (&Admin{URL: s.URL, Key: "api", Secret: "secret"}).SessionQueue(context.Background(), "iot-inbox-1")
	if err != nil || queued != 1000 || dropped != 4639 {
		t.Fatal(queued, dropped, err)
	}
}

func TestAdminTopicAuthorizationChecksRuntimeContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		want   bool
	}{
		{"secure JWT and default file fallback", func(map[string]any) {}, true},
		{"disabled JWT", func(v map[string]any) { v["/authentication"].([]any)[0].(map[string]any)["enable"] = false }, false},
		{"additional authenticator", func(v map[string]any) {
			v["/authentication"] = append(v["/authentication"].([]any), map[string]any{"enable": true, "mechanism": "password_based"})
		}, false},
		{"wrong JWT claim", func(v map[string]any) { v["/authentication"].([]any)[0].(map[string]any)["acl_claim_name"] = "other" }, false},
		{"username not verified", func(v map[string]any) { v["/authentication"].([]any)[0].(map[string]any)["verify_claims"] = []any{} }, false},
		{"expiry remains connected", func(v map[string]any) {
			v["/authentication"].([]any)[0].(map[string]any)["disconnect_after_expire"] = false
		}, false},
		{"allow by default", func(v map[string]any) { v["/authorization/settings"].(map[string]any)["no_match"] = "allow" }, false},
		{"unknown authorizer", func(v map[string]any) {
			v["/authorization/sources"].(map[string]any)["sources"] = []any{map[string]any{"type": "http", "enable": true}}
		}, false},
		{"missing source configuration", func(v map[string]any) {
			v["/authorization/sources"] = map[string]any{}
		}, false},
		{"disabled authorizer", func(v map[string]any) {
			v["/authorization/sources"].(map[string]any)["sources"] = []any{map[string]any{"type": "http", "enable": false}}
		}, true},
		{"anonymous listener", func(v map[string]any) { v["/listeners/tcp:default"].(map[string]any)["enable_authn"] = false }, false},
		{"anonymous quick denial", func(v map[string]any) {
			v["/listeners/tcp:default"].(map[string]any)["enable_authn"] = "quick_deny_anonymous"
		}, true},
		{"listener chain override", func(v map[string]any) {
			v["/listeners/tcp:default"].(map[string]any)["authentication"] = []any{map[string]any{"enable": true, "mechanism": "password_based"}}
		}, false},
		{"no listener", func(v map[string]any) { v["/listeners"] = []any{} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			responses := map[string]any{
				"/authentication":         []any{map[string]any{"enable": true, "mechanism": "jwt", "from": "password", "algorithm": "hmac-based", "use_jwks": false, "acl_claim_name": "acl", "disconnect_after_expire": true, "verify_claims": []any{map[string]any{"name": "username", "value": "${username}"}}, "secret": "must-not-appear"}},
				"/authorization/settings": map[string]any{"no_match": "deny"},
				"/authorization/sources":  map[string]any{"sources": []any{map[string]any{"type": "file", "enable": true, "rules": "{allow, {ipaddr, \"127.0.0.1\"}, all, [\"$SYS/#\", \"#\"]}.\n{deny,all}."}}},
				"/listeners":              []any{map[string]any{"id": "tcp:default", "enable": true}},
				"/listeners/tcp:default":  map[string]any{"enable_authn": true},
			}
			tc.change(responses)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Fatal("readiness performed mutation")
				}
				v, ok := responses[strings.TrimPrefix(r.URL.Path, "/api/v5")]
				if !ok {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(v)
			}))
			defer server.Close()
			err := (&Admin{URL: server.URL, Key: "key", Secret: "secret"}).CheckTopicAuthorization(context.Background())
			if (err == nil) != tc.want {
				t.Fatalf("ready=%v want=%v err=%v", err == nil, tc.want, err)
			}
			if err != nil && strings.Contains(err.Error(), "must-not-appear") {
				t.Fatal("broker signing secret leaked")
			}
		})
	}
}
