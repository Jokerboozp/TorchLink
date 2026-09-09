package gbmetadata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNodeAuthenticationSnapshotRetryAndRestore(t *testing.T) {
	var unavailable, revoked atomic.Bool
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Edge-Secret") != "test-node-secret" || revoked.Load() {
			w.WriteHeader(401)
			return
		}
		if unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/api/v1/edge/tenant/node/config":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tenantId":"tenant","nodeId":"node","tasks":[]}`))
		case "/api/v1/edge/tenant/node/heartbeat":
			var h struct {
				VideoCatalog []DeviceCatalog `json:"videoCatalog"`
			}
			if json.NewDecoder(r.Body).Decode(&h) != nil || len(h.VideoCatalog) != 1 {
				w.WriteHeader(422)
				return
			}
			uploads.Add(1)
			w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	u := Uploader{URL: server.URL, TenantID: "tenant", NodeID: "node", Secret: "wrong", DataDir: t.TempDir(), AllowHTTP: true}
	if err := u.Init(context.Background()); !errors.Is(err, ErrNodeUnauthorized) {
		t.Fatal("wrong credential accepted", err)
	}
	u.Secret = "test-node-secret"
	if err := u.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog := []DeviceCatalog{{DeviceID: testDevice, Registered: true, CatalogAt: 1, Channels: []Channel{{DeviceID: testChannel, Name: "Camera"}}}}
	unavailable.Store(true)
	if err := u.Upload(context.Background(), catalog); err == nil {
		t.Fatal("outage returned success")
	}
	data, err := os.ReadFile(filepath.Join(u.DataDir, "catalog.json"))
	if err != nil || strings.Contains(string(data), u.Secret) || !strings.Contains(string(data), testChannel) {
		t.Fatal("snapshot not safely retained", err)
	}
	r, err := New(Config{ServerID: "34020000002000000001", Realm: "3402000000", Listen: "127.0.0.1:5060", AllowedCIDRs: []string{"127.0.0.0/8"}, Devices: map[string]string{testDevice: testPassword}})
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Restore(r); err != nil {
		t.Fatal(err)
	}
	restored := r.Snapshot()
	if len(restored) != 1 || restored[0].Registered || len(restored[0].Channels) != 1 {
		t.Fatal("restore fabricated registration", restored)
	}
	unavailable.Store(false)
	if err := u.Upload(context.Background(), restored); err != nil || uploads.Load() != 1 {
		t.Fatal("retry failed", err)
	}
	revoked.Store(true)
	if err := u.Upload(context.Background(), restored); !errors.Is(err, ErrNodeUnauthorized) {
		t.Fatal("revocation accepted", err)
	}
}
