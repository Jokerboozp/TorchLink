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
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeviceShadowAuthenticatedReconciliation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	if _, _, err := repo.ChangeDeviceCredential(ctx, "tenant", "device", "", "", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if w := call("GET", "/api/v1/device-shadow", "", "", nil, created.Credential.Secret); w.Code != 401 {
		t.Fatal("revoked credential read shadow", w.Code)
	}
}
