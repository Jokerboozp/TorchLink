package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeviceShadowAuthenticatedReconciliation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer upstream.Close()
	product := model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "target", DataType: "number", Writable: true}, {Identifier: "battery", DataType: "number"}}}}
	if err := repo.SaveProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	q := onboarding.Request{ProductID: "product", DeviceID: "device", Name: "Shadow device", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"preview","timestamp":1,"data":{"target":42}}`)}
	preview, err := api.onboarding.Test(ctx, "tenant", q)
	if err != nil || !preview.Success {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	q.TestToken = preview.TestToken
	created, err := api.onboarding.Create(ctx, "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, tenant, role string, body any, secret string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		if tenant != "" {
			token, e := api.auth.Issue("tester", tenant, role, nil, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("X-Device-Key", created.Credential.AccessKey)
		r.Header.Set("X-Device-Secret", secret)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/device-registry/device/shadow"
	patch := map[string]any{"expectedDesiredVersion": 0, "confirmed": true, "desired": map[string]any{"target": float64(42)}}
	if w := call("PATCH", path, "tenant", "viewer", patch, ""); w.Code != 403 {
		t.Fatal("viewer modified desired", w.Code)
	}
	if w := call("GET", path, "other", "admin", nil, ""); w.Code != 404 {
		t.Fatal("foreign shadow", w.Code)
	}
	patch["confirmed"] = false
	if w := call("PATCH", path, "tenant", "operator", patch, ""); w.Code != 422 {
		t.Fatal("unconfirmed desired", w.Code)
	}
	patch["confirmed"] = true
	patch["desired"] = map[string]any{"battery": float64(85)}
	if w := call("PATCH", path, "tenant", "operator", patch, ""); w.Code != 422 {
		t.Fatal("read-only field accepted", w.Code)
	}
	patch["desired"] = map[string]any{"target": "42"}
	if w := call("PATCH", path, "tenant", "operator", patch, ""); w.Code != 422 {
		t.Fatal("wrong type accepted", w.Code)
	}
	patch["desired"] = map[string]any{"target": float64(42)}
	if w := call("PATCH", path, "tenant", "operator", patch, ""); w.Code != 200 {
		t.Fatalf("desired: %d %s", w.Code, w.Body.String())
	}
	if w := call("PATCH", path, "tenant", "operator", patch, ""); w.Code != 409 {
		t.Fatal("stale version accepted", w.Code)
	}
	if w := call("GET", "/api/v1/device-shadow", "", "", nil, "wrong"); w.Code != 401 {
		t.Fatal("wrong device credential", w.Code)
	}
	w := call("GET", "/api/v1/device-shadow", "", "", nil, created.Credential.Secret)
	var shadow model.DeviceShadow
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &shadow) != nil || shadow.Delta["target"] != float64(42) {
		t.Fatal("device delta", w.Code, w.Body.String())
	}
	now := time.Now().UnixMilli()
	report := map[string]any{"id": "report-1", "timestamp": now, "data": map[string]any{"target": float64(42)}}
	ingest := "/api/v1/device-ingest/standard/tenant/product/device/property"
	if w := call("POST", ingest, "", "", report, created.Credential.Secret); w.Code != 202 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	wait := func(check func(model.DeviceShadow) bool) {
		t.Helper()
		for {
			shadow, err := repo.GetDeviceShadow(ctx, "tenant", "device")
			if err == nil && check(shadow) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("shadow not reconciled")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	wait(func(s model.DeviceShadow) bool { return s.Reported["target"] == float64(42) && len(s.Delta) == 0 })
	report["id"], report["timestamp"], report["data"] = "late", now-10, map[string]any{"target": float64(1), "battery": float64(80)}
	if w := call("POST", ingest, "", "", report, created.Credential.Secret); w.Code != 202 {
		t.Fatalf("late report: %d %s", w.Code, w.Body.String())
	}
	wait(func(s model.DeviceShadow) bool {
		return s.Reported["target"] == float64(42) && s.Reported["battery"] == float64(80) && len(s.Delta) == 0
	})
	t.Run("named", func(t *testing.T) {
		patch := map[string]any{"expectedDesiredVersion": 0, "confirmed": true, "desired": map[string]any{"target": float64(7)}}
		if w := call("PATCH", path+"?name=control", "tenant", "operator", patch, ""); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := call("PATCH", path+"?name=control", "tenant", "operator", patch, ""); w.Code != 409 {
			t.Fatal("named version conflict", w.Code)
		}
		if w := call("GET", path+"?name=control", "other", "admin", nil, ""); w.Code != 404 {
			t.Fatal("foreign named shadow", w.Code)
		}
		if w := call("GET", path+"?name=one&name=two", "tenant", "admin", nil, ""); w.Code != 422 {
			t.Fatal("ambiguous name", w.Code)
		}
		if w := call("GET", "/api/v1/device-shadow?name=control", "", "", nil, "wrong"); w.Code != 401 {
			t.Fatal("named auth", w.Code)
		}
		report := map[string]any{"id": "named-report", "shadow": "control", "timestamp": now, "data": map[string]any{"target": float64(7)}}
		if w := call("POST", ingest, "", "", report, created.Credential.Secret); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		for {
			w := call("GET", "/api/v1/device-shadow?name=control", "", "", nil, created.Credential.Secret)
			var v model.DeviceShadow
			if w.Code == 200 && json.Unmarshal(w.Body.Bytes(), &v) == nil && v.Name == "control" && v.Reported["target"] == float64(7) && len(v.Delta) == 0 {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("named shadow did not reconcile")
			case <-time.After(10 * time.Millisecond):
			}
		}
		w := call("GET", path+"/history?name=control", "tenant", "viewer", nil, "")
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"target":7`)) || bytes.Contains(w.Body.Bytes(), []byte(`"target":42`)) {
			t.Fatal("mixed history", w.Body.String())
		}
		w = call("GET", "/api/v1/device-registry/device/shadows", "tenant", "viewer", nil, "")
		if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"control"`)) {
			t.Fatal("named list", w.Body.String())
		}
		v, _ := repo.GetDeviceShadow(ctx, "tenant", "device")
		if v.Reported["target"] != float64(42) || v.DesiredVersion != 1 {
			t.Fatal("named report changed default", v)
		}
	})
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "twin-peer", Name: "孪生邻居", ProductID: "product", Status: "ENABLED", AccessKey: "private-twin-peer-key"}); err != nil {
		t.Fatal(err)
	}
	update := map[string]any{"expectedVersion": 0, "add": []model.TwinRelation{{Source: "device", Target: "twin-peer", Kind: "contains"}}}
	if w := call("PATCH", "/api/v1/device-twin-topology", "tenant", "viewer", update, ""); w.Code != 403 {
		t.Fatal("viewer topology write", w.Code)
	}
	if w := call("PATCH", "/api/v1/device-twin-topology", "tenant", "operator", update, ""); w.Code != 200 {
		t.Fatal("create topology", w.Code, w.Body.String())
	}
	if w := call("PATCH", "/api/v1/device-twin-topology", "tenant", "operator", update, ""); w.Code != 409 {
		t.Fatal("stale topology edit", w.Code)
	}
	if w := call("GET", "/api/v1/device-twins/device", "other", "admin", nil, ""); w.Code != 404 {
		t.Fatal("foreign twin", w.Code)
	}
	w = call("GET", "/api/v1/device-twins/device", "tenant", "viewer", nil, "")
	var twin struct {
		Nodes     []model.TwinNode     `json:"nodes"`
		Relations []model.TwinRelation `json:"relations"`
		Shadow    model.DeviceShadow   `json:"shadow"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &twin) != nil || len(twin.Nodes) != 2 || len(twin.Relations) != 1 || twin.Shadow.Reported["target"] != float64(42) || len(twin.Shadow.Delta) != 0 {
		t.Fatal("actual twin projection", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("private-twin-peer-key")) || bytes.Contains(w.Body.Bytes(), []byte(created.Credential.Secret)) {
		t.Fatal("twin exposed credentials")
	}
	if w := call("GET", "/api/v1/device-twins/device?depth=1000", "tenant", "viewer", nil, ""); w.Code != 422 {
		t.Fatal("unbounded graph traversal", w.Code)
	}
	if w := call("GET", "/api/v1/device-registry/device/history?kind=property", "tenant", "viewer", nil, ""); w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"target":42`)) {
		t.Fatal("actual property history", w.Code, w.Body.String())
	}
	t.Run("twin-browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "twin-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+upstream.URL, "IOT_TEST_TOKEN="+token)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("browser: %v %s", err, output)
		}
		t.Log(string(output))
	})
	t.Run("named-shadow-browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "named-shadow-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+upstream.URL, "IOT_TEST_TOKEN="+token)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("browser: %v %s", err, output)
		} else {
			t.Log(string(output))
		}
	})

	if _, _, err := repo.ChangeDeviceCredential(ctx, "tenant", "device", "", "", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/v1/device-shadow", "", "", nil, created.Credential.Secret); w.Code != 401 {
		t.Fatal("revoked credential read shadow", w.Code)
	}
}
