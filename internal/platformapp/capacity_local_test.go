package platformapp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/httpapi"
	"iot-platform/internal/metrics"
)

func TestLocalCapacityStartupExposesMenuAndEnvironment(t *testing.T) {
	t.Setenv("IOT_OPS_CAPACITY_LOCAL", "true")
	t.Setenv("IOT_CAPACITY_MODULE", "on")
	t.Setenv("IOT_OPS_CAPACITY_URL", "")
	t.Setenv("IOT_OPS_CAPACITY_TOKEN", "")
	cfg := config.Load()
	cfg.ProcessRole = config.RoleCombined
	cfg.HTTPAddr, cfg.InstanceID = ":8081", "local"
	cfg.PostgresDSN = "postgres://test@localhost/test"
	cfg.DataDir = t.TempDir()
	cfg.AdminUser, cfg.AdminPassword = "root", "root-password-test"
	cfg.JWTSecret, cfg.DevMode = strings.Repeat("j", 48), true
	cfg.AdminTenants = []string{"tenant_001"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stop, err := startLocalCapacity(&cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	api := httpapi.New(cfg, &core.Engine{Repo: memory.NewRepository()}, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	request := func(path, token string, body io.Reader) map[string]any {
		t.Helper()
		method := http.MethodGet
		if body != nil {
			method = http.MethodPost
		}
		req, err := http.NewRequest(method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s returned HTTP %d", path, resp.StatusCode)
		}
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	login := request("/api/v1/auth/login", "", strings.NewReader(`{"username":"root","password":"root-password-test","tenantId":"tenant_001"}`))
	token := login["accessToken"].(string)
	status := request("/api/v1/ops/capacity/status", token, nil)
	if status["enabled"] != true || status["reachable"] != true {
		t.Fatalf("local source startup hides capacity menu: enabled=%v reachable=%v", status["enabled"], status["reachable"])
	}
	environments := request("/api/v1/ops/capacity/environments", token, nil)["items"].([]any)
	if len(environments) != 1 || environments[0].(map[string]any)["name"] != "self" || environments[0].(map[string]any)["error"] != nil {
		t.Fatal("local capacity environment unavailable")
	}
	if runs := request("/api/v1/ops/capacity/runs", token, nil)["items"].([]any); len(runs) != 0 {
		t.Fatal("starting the platform started a capacity run")
	}
	if !strings.HasPrefix(cfg.Ops.CapacityURL, "http://127.0.0.1:") || len(cfg.Ops.CapacityToken) < 32 {
		t.Fatal("controller must use authenticated loopback access")
	}
	resp, err := http.Get(cfg.Ops.CapacityURL + "/v1/environments")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("loopback controller accepted a request without its service token")
	}
	stop()
	if resp, err := http.Get(cfg.Ops.CapacityURL + "/health"); err == nil {
		resp.Body.Close()
		t.Fatal("controller still running after platform shutdown")
	}
}

func TestLocalCapacityPreservesDisabledExternalAndWorkerModes(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for name, cfg := range map[string]config.Config{
		"disabled":  {},
		"external":  {Ops: config.OpsConfig{CapacityLocal: true, CapacityURL: "http://capacity:7080", CapacityToken: strings.Repeat("s", 32)}},
		"worker":    {ProcessRole: config.RoleParser, Ops: config.OpsConfig{CapacityLocal: true}},
		"split api": {ProcessRole: config.RoleAPI, Ops: config.OpsConfig{CapacityLocal: true}},
	} {
		t.Run(name, func(t *testing.T) {
			before := cfg.Ops
			stop, err := startLocalCapacity(&cfg, log)
			if err != nil {
				t.Fatal(err)
			}
			stop()
			if cfg.Ops.CapacityURL != before.CapacityURL || cfg.Ops.CapacityToken != before.CapacityToken {
				t.Fatal("local startup changed an independently configured capacity service")
			}
		})
	}
	for _, cfg := range []config.Config{
		{HTTPAddr: ":8081", Ops: config.OpsConfig{CapacityLocal: true}},
		{HTTPAddr: "invalid", PostgresDSN: "postgres://test@localhost/test", Ops: config.OpsConfig{CapacityLocal: true}},
	} {
		stop, err := startLocalCapacity(&cfg, log)
		stop()
		if err == nil || cfg.Ops.CapacityURL != "" || cfg.Ops.CapacityToken != "" {
			t.Fatal("invalid local configuration started a controller")
		}
	}
}
