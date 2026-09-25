package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestDeviceConnectionCheckUsesCurrentFieldEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UnixMilli()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	cfg := config.Load()
	cfg.JWTSecret = "device-check-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	srv := New(cfg, engine, metrics.New(), log)
	token, _ := srv.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED", ProtocolPackageID: "iot-standard@1.0.0", Transport: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "device", ProductID: "product", Name: "设备", Status: "ENABLED", UpdatedAt: now - 2000, Connector: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	add := func(id, source string, at int64, parseError string, parsed bool) {
		t.Helper()
		raw := model.RawMessage{MessageID: id, TenantID: "tenant", ProductID: "product", DeviceID: "device", Source: source, Protocol: "iot-standard", ProtocolID: "iot-standard", ProtocolVersion: "1.0.0", Transport: "HTTP", PayloadFormat: "json", Payload: json.RawMessage(`{"data":{"temperature":22}}`), ReceivedAt: at}
		idx, err := archive.PutRaw(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		idx.ParseError = parseError
		if _, err = repo.SaveRawIndex(ctx, idx); err != nil {
			t.Fatal(err)
		}
		if parsed {
			if err = repo.SaveStandardMessage(ctx, model.StandardMessage{MessageID: "parsed-" + id, RawMessageID: id, TenantID: "tenant", ProductID: "product", DeviceID: "device", MessageType: model.PropertyReport, Timestamp: at, Properties: map[string]any{"temperature": 22}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(since int64) map[string]any {
		t.Helper()
		path := "/api/v1/device-registry/device/connection?since=" + strconv.FormatInt(since, 10)
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("check %d: %s", w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result["ingest"].(map[string]any)
	}
	add("historical", "standard-http", now-4000, "", true)
	if got := check(now - 1000)["stage"]; got != "WAITING_FOR_DATA" {
		t.Fatalf("historical counted: %v", got)
	}
	if check(now - 1000)["previousParsedAt"] == nil {
		t.Fatal("historical success not distinguished")
	}
	add("debug", "managed-device", now-500, "", true)
	if got := check(now - 1000)["stage"]; got != "WAITING_FOR_DATA" {
		t.Fatalf("simulated counted: %v", got)
	}
	if got := check(now - 1000)["simulationCount"]; got != float64(1) {
		t.Fatalf("simulation not identified: %v", got)
	}
	add("bad", "standard-http", now-300, "invalid payload", false)
	if got := check(now - 1000)["stage"]; got != "PARSE_FAILED" {
		t.Fatalf("parse failure hidden: %v", got)
	}
	add("good", "standard-http", now-100, "", true)
	if got := check(now - 1000)["stage"]; got != "PARSED" {
		t.Fatalf("parsed evidence missing: %v", got)
	}
	if check(now - 1000)["continuouslyUpdating"] != false {
		t.Fatal("one parsed message is not continuous")
	}
	add("good-again", "standard-http", now-50, "", true)
	if check(now - 1000)["continuouslyUpdating"] != true {
		t.Fatal("second parsed report missing")
	}
}
