package httpapi

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/ports"
)

// testAPI is a Server behind an httptest server that closes with the test.
type testAPI struct {
	*Server
	repo   ports.Repository
	server *httptest.Server
}

// newTestAPI serves repo with the defaults most tests share: built-in admin
// root / root-password-test for tenant_a, a fixed signing key and development
// mode. configure adjusts the configuration before the server is built.
func newTestAPI(t *testing.T, repo ports.Repository, configure func(*config.Config)) *testAPI {
	t.Helper()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "root-password-test"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.JWTSecret = "test-only-signing-key-at-least-32-characters"
	cfg.DevMode = true
	if configure != nil {
		configure(&cfg)
	}
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	return &testAPI{Server: api, repo: repo, server: server}
}

// request sends a JSON request and checks the status; see requestJSON.
func (a *testAPI) request(t *testing.T, method, path, token string, body any, status int) map[string]any {
	t.Helper()
	return requestJSON(t, a.server.Client(), method, a.server.URL+path, token, body, status)
}

// adminToken is a session token of the built-in administrator for tenant.
func (a *testAPI) adminToken(t *testing.T, tenant string) string {
	t.Helper()
	token, err := a.auth.IssueWithVersion(a.cfg.AdminUser, tenant, "admin", a.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// login signs in through the API and returns the access token.
func (a *testAPI) login(t *testing.T, username, password, tenant string) string {
	t.Helper()
	return a.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"username": username, "password": password, "tenantId": tenant}, 200)["accessToken"].(string)
}
