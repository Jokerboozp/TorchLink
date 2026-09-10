package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
)

func TestProtocolDevicesHaveNoPlatformCredentials(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.DataDir, cfg.JWTSecret = root, "credential-scope-test-key-32-characters"
	api := New(cfg, core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log), metrics.New(), log)
	call := func(method, path, body, tenant, role, key, secret string) *httptest.ResponseRecorder {
		token, err := api.auth.Issue("tester", tenant, role, nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Key", key)
		req.Header.Set("X-Device-Secret", secret)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		return w
	}
	for _, transport := range []string{"TCP", "UDP", "TCP_UDP", "MODBUS_TCP", "MODBUS_RTU_TCP"} {
		t.Run(transport, func(t *testing.T) {
			id := strings.ToLower(transport)
			p := model.Product{TenantID: "tenant", ID: id, Status: "ENABLED", Transport: transport}
			if err := repo.SaveProduct(ctx, p); err != nil {
				t.Fatal(err)
			}
			w := call("POST", "/api/v1/device-registry", `{"id":"`+id+`","name":"设备","productId":"`+id+`"}`, "tenant", "admin", "", "")
			if w.Code != 201 || bytes.Contains(w.Body.Bytes(), []byte(`"credential"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
				t.Fatalf("unexpected creation: %d %s", w.Code, w.Body.String())
			}
			discoveredID := "discovered-" + id
			if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant", DeviceID: discoveredID, ProductID: id}); err != nil {
				t.Fatal(err)
			}
			w = call("POST", "/api/v1/discovered-devices/"+discoveredID+"/register", "{}", "tenant", "admin", "", "")
			if w.Code != 201 || bytes.Contains(w.Body.Bytes(), []byte(`"credential"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
				t.Fatalf("discovery generated credentials: %d %s", w.Code, w.Body.String())
			}
			d, err := repo.GetManagedDevice(ctx, "tenant", id)
			if err != nil || d.SecretHash != "" || d.AccessKey != model.ProtocolDeviceAccessKey("tenant", id) {
				t.Fatal("invalid stored identity", err)
			}
			// Existing records may still contain formerly generated secrets. They
			// must not expose or accept those as an alternate HTTP/MQTT identity.
			d.SecretHash, d.SecretHint = onboarding.Hash("old-secret"), "secret"
			if err = repo.SaveManagedDevice(ctx, d); err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{"connection", "connection-guide"} {
				w = call("GET", "/api/v1/device-registry/"+id+"/"+suffix, "", "tenant", "admin", "", "")
				if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"credentialSupported":false`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
					t.Fatalf("unexpected detail: %d %s", w.Code, w.Body.String())
				}
			}
			for _, method := range []string{"POST", "DELETE"} {
				path := "/api/v1/device-registry/" + id + "/credentials"
				for _, scope := range []struct {
					tenant, role string
					status       int
				}{{"tenant", "admin", 422}, {"tenant", "viewer", 403}, {"other", "admin", 404}} {
					w = call(method, path, "{}", scope.tenant, scope.role, "", "")
					if w.Code != scope.status {
						t.Fatalf("%s %s: %d want %d", method, path, w.Code, scope.status)
					}
				}
			}
			for _, path := range []string{"/api/v1/device-mqtt/token", "/api/v1/device-ingest/" + id, "/api/v1/device-ingest/standard/tenant/" + id + "/" + id + "/property"} {
				w = call("POST", path, "{}", "tenant", "admin", d.AccessKey, "old-secret")
				if w.Code != 401 {
					t.Fatalf("native device authenticated on %s: %d", path, w.Code)
				}
			}
			after, _ := repo.GetManagedDevice(ctx, "tenant", id)
			if after.AccessKey != d.AccessKey || after.SecretHash != d.SecretHash {
				t.Fatal("rejected operation mutated device")
			}
			revocations, err := repo.ListCredentialRevocations(ctx, "tenant", id, false)
			if err != nil || len(revocations) != 0 {
				t.Fatal("rejected operation generated revocations", err)
			}
		})
	}
	w := call("GET", "/api/v1/device-registry", "", "tenant", "admin", "", "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
		t.Fatal("registry exposes protocol keys", w.Code)
	}
}
