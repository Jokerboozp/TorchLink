package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/adapters/postgres"
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

// postgresTestRepo is a migrated PostgreSQL repository in its own schema,
// dropped with the test; the test is skipped without IOT_TEST_POSTGRES_DSN.
func postgresTestRepo(t *testing.T) ports.Repository {
	t.Helper()
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("httpapi_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	repo, err := postgres.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		repo.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
		admin.Close()
	})
	return repo
}

// forEachStore runs check against the memory repository and, when
// IOT_TEST_POSTGRES_DSN is set, against PostgreSQL.
func forEachStore(t *testing.T, check func(t *testing.T, repo ports.Repository)) {
	t.Run("memory", func(t *testing.T) { check(t, memory.NewRepository()) })
	t.Run("postgres", func(t *testing.T) { check(t, postgresTestRepo(t)) })
}
