package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/repositorytest"
	"os"
	"testing"
	"time"
)

func TestRawFiltersPostgres(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires IOT_TEST_POSTGRES_DSN; uses an isolated temporary schema")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("raw_filter_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := &Repository{pool: pool}
	if err = repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repositorytest.RawFilters(t, repo)
}
