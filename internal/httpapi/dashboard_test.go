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
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestDashboard(t *testing.T) {
	t.Run("memory", func(t *testing.T) { checkDashboard(t, memory.NewRepository()) })
	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("IOT_TEST_POSTGRES_DSN not configured")
		}
		ctx := context.Background()
		admin, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		schema := fmt.Sprintf("dashboard_test_%d", time.Now().UnixNano())
		ident := pgx.Identifier{schema}.Sanitize()
		if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := admin.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
				t.Error(err)
			}
		}()
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
		defer repo.Close()
		checkDashboard(t, repo)
	})
}
func checkDashboard(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	location := time.FixedZone("test", 8*3600)
	now := time.Now().In(location)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -6).UnixMilli()
	for _, tenant := range []string{"tenant", "other"} {
		must(repo.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "p", Name: "烟感", Status: "ENABLED"}))
		for i, status := range []string{"ONLINE", "ALARM", "OFFLINE", "SUSPECTED_OFFLINE", ""} {
			id := fmt.Sprint(i)
			must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: id, ProductID: "p", Status: "ENABLED", AccessKey: tenant + id}))
			if status != "" {
				must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: id, ProductID: "p", BusinessStatus: status, ConnectionStatus: "CONNECTED"}))
			}
		}
		must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: "unregistered", ProductID: "p", BusinessStatus: "ONLINE"}))
		for i := 0; i < 125; i++ {
			first := start
			level := "HIGH"
			status := "ACTIVE"
			if i == 0 {
				first = start - 1
			}
			if i == 1 {
				first = start + 86400000
			}
			if i == 2 {
				first = now.UnixMilli() + 86400000
			}
			if i == 3 {
				status = "ACKED"
			}
			if i == 4 {
				status = "RECOVERED"
			}
			if i == 5 {
				level = "CRITICAL"
			}
			_, _, err := repo.UpsertAlarm(ctx, model.Alarm{TenantID: tenant, ID: fmt.Sprint(i), DeviceID: fmt.Sprint(i), RuleID: fmt.Sprint(i), AlarmLevel: level, Status: status, FirstTriggeredAt: first, LastTriggeredAt: now.UnixMilli(), TriggerCount: 99})
			must(err)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "dashboard-test-secret-32-characters"
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	result := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?days=7&offset=480", token, nil, 200)
	if result["devices"] != float64(5) || result["online"] != float64(2) || result["activeAlarms"] != float64(123) || result["highAlarms"] != float64(123) {
		t.Fatalf("wrong totals: %v", result)
	}
	states := result["states"].(map[string]any)
	if states["SUSPECTED_OFFLINE"] != float64(1) || states["NEVER_SEEN"] != float64(1) {
		t.Fatal(states)
	}
	trend := result["trend"].([]any)
	if len(trend) != 7 || trend[0].(map[string]any)["count"] != float64(122) || trend[1].(map[string]any)["count"] != float64(1) || trend[2].(map[string]any)["count"] != float64(0) {
		t.Fatal(trend)
	}
	products := result["products"].([]any)
	if len(products) != 1 || products[0].(map[string]any)["count"] != float64(5) {
		t.Fatal(products)
	}
	result = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?days=30&offset=-720", token, nil, 200)
	if len(result["trend"].([]any)) != 30 {
		t.Fatal(result)
	}
	emptyToken, _ := api.auth.Issue("viewer", "empty", "viewer", nil, time.Hour)
	result = requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard", emptyToken, nil, 200)
	if result["devices"] != float64(0) || result["activeAlarms"] != float64(0) || len(result["products"].([]any)) != 0 {
		t.Fatal(result)
	}
	for _, query := range []string{"days=10000", "days=abc", "offset=841", "offset=-721", "offset=abc"} {
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard?"+query, token, nil, 400)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/dashboard", "", nil, 401)
}
