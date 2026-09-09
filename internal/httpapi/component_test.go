package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
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

func TestComponentAlarmAPIAndBrowser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", ParserType: parser.StandardParserName, Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ProductID: "p", ID: "controller", Name: "一号消防控制器", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	send := func(id string, at int64, part string, fire bool) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"id": id, "timestamp": at, "data": map[string]any{"components": []model.ComponentStatus{{ID: part, Name: "烟感探测器", Location: "二楼走廊", Alarms: map[string]bool{"FIRE": fire, "DEVICE_FAULT": fire}}}}})
		raw, err := onboarding.StandardRaw("tenant", "p", "controller", "event", "HTTP", payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = engine.IngestRaw(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}
	send("fire-B", 1000, "loop-1/node-7", true)
	send("normal-A", 2000, "loop-1/node-8", false)
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant", Status: "ACTIVE", Limit: 100})
	if err != nil || len(alarms) != 2 {
		t.Fatal(alarms, err)
	}
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)
	detail := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/alarms/"+alarms[0].ID, token, nil, 200)
	if detail["componentId"] != "loop-1/node-7" || detail["componentLocation"] != "二楼走廊" {
		t.Fatal(detail)
	}
	other, _ := api.auth.Issue("other", "other-tenant", "operator", nil, time.Hour)
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/alarms/"+alarms[0].ID, other, nil, 404)
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("Chrome not configured")
		}
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "component-alarm-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("browser: %v %s", err, out)
		}
		t.Log(string(out))
	})
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/alarms/"+alarms[0].ID+"/actions", token, map[string]any{"action": "ACKED"}, 200)
	send("recover-B", 3000, "loop-1/node-7", false)
	saved, err := repo.GetAlarm(ctx, "tenant", alarms[0].ID)
	if err != nil || saved.Status != "RECOVERED" {
		t.Fatal(saved, err)
	}
}
