package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/adapters/rawstore"
	"iot-platform/internal/analytics/dataquality/testkit"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

// This optional fixture serves the real API against an isolated PostgreSQL
// schema for browser checks. It never starts ingress, production processors or
// scheduled jobs. The stop file permits cleanup after a separate browser run.
func TestAnalyticsBrowserFixture(t *testing.T) {
	if os.Getenv("IOT_TEST_ANALYTICS_BROWSER") != "1" {
		t.Skip("explicit browser fixture required")
	}
	dsn, password, stopFile := os.Getenv("IOT_TEST_POSTGRES_DSN"), os.Getenv("IOT_TEST_ADMIN_PASSWORD"), os.Getenv("IOT_TEST_ANALYTICS_BROWSER_STOP_FILE")
	if dsn == "" || password == "" || stopFile == "" {
		t.Fatal("database, fixture password and stop file required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("fixture database configuration failed")
	}
	defer pool.Close()
	schema := fmt.Sprintf("analytics_browser_%d", time.Now().UnixNano())
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal("fixture schema creation failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("fixture schema cleanup failed")
		}
	}()
	fixtureDSN := analyticsBrowserDSN(dsn, schema)
	seedConfig, err := pgxpool.ParseConfig(fixtureDSN)
	if err != nil {
		t.Fatal("fixture database configuration failed")
	}
	seed, err := pgxpool.NewWithConfig(ctx, seedConfig)
	if err != nil {
		t.Fatal("fixture connection failed")
	}
	defer seed.Close()
	var activeSchema string
	if err = seed.QueryRow(ctx, "SELECT current_schema()").Scan(&activeSchema); err != nil || activeSchema != schema {
		t.Fatal("browser fixture schema isolation failed")
	}
	repo, err := postgres.New(ctx, fixtureDSN)
	if err != nil {
		t.Fatal("fixture repository initialization failed")
	}
	defer repo.Close()
	const tenant = "ai-app-e2e"
	data, err := testkit.Seed(ctx, repo, tenant, 40)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "quality-device-hidden", TenantID: tenant, ProductID: data.ProductID, Name: "范围隔离验证设备", AccessKey: "fixture-hidden"}); err != nil {
		t.Fatal(err)
	}
	if _, err = seed.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=$1,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC'`, data.Start-1000); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{AdminUser: "admin", AdminPassword: password, AdminTenants: []string{tenant}, JWTSecret: "analytics-browser-fixture-secret-32-characters", DevMode: true, InstanceID: "analytics-browser-fixture", Analytics: config.AnalyticsConfig{Poll: 100 * time.Millisecond}}
	engine := &core.Engine{Repo: repo, Parsers: parser.NewPlatformRegistry(t.TempDir())}
	engine.Archive, err = local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine.RawStore = rawstore.New(rawstore.Config{PostgreSQL: repo, Resolver: repo})
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAnalysisStorage(repo, repo)
	go api.RunAnalysisWorkers(ctx)
	go api.RunAnalysisAIWorkers(ctx)
	addr := os.Getenv("IOT_TEST_ANALYTICS_BROWSER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8092"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	if path := os.Getenv("IOT_TEST_ANALYTICS_BROWSER_INFO"); path != "" {
		info, _ := json.Marshal(map[string]any{"tenantId": tenant, "deviceId": data.DeviceID, "start": data.Start, "end": data.End, "periodMs": 1000, "unit": "kPa", "sampleCount": 40, "address": listener.Addr().String(), "profileRequest": data.ProfileRequest, "baselineRequest": data.BaselineRequest})
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, info, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("isolated PostgreSQL browser fixture ready", listener.Addr().String())
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("browser fixture exceeded time budget")
		case err := <-serveErr:
			if err != http.ErrServerClosed {
				t.Fatal(err)
			}
			return
		case <-ticker.C:
			if _, err := os.Stat(stopFile); err == nil {
				cancel()
				return
			}
		}
	}
}

func analyticsBrowserDSN(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, _ := url.Parse(dsn)
		q := u.Query()
		q.Set("search_path", schema+",public")
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + " search_path='" + schema + ",public'"
}

func TestAnalyticsBrowserDSNIncludesActualIsolatedSearchPath(t *testing.T) {
	for _, dsn := range []string{"postgres://test-user:fixture-only@localhost/test-db?sslmode=disable", "host=localhost user=test-user dbname=test-db"} {
		cfg, err := pgx.ParseConfig(analyticsBrowserDSN(dsn, "owned_fixture"))
		if err != nil || cfg.RuntimeParams["search_path"] != "owned_fixture,public" {
			t.Fatal("test fixture does not preserve its database isolation")
		}
	}
}
