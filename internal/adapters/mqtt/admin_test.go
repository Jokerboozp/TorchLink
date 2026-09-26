package mqttadapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
