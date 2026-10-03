package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMigrationFilesAreWellFormed(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.run == nil && strings.TrimSpace(m.sql) == "" {
			t.Fatalf("migration %04d_%s is empty", m.version, m.name)
		}
	}
}

func TestSplitStatementsUsesSeparatorLines(t *testing.T) {
	got := splitStatements("-- migrate:no-transaction\nCREATE INDEX CONCURRENTLY a ON t(x);\n;\n-- note\nCREATE INDEX CONCURRENTLY b ON t(y)\n")
	if len(got) != 2 || !strings.HasPrefix(got[0], "CREATE INDEX CONCURRENTLY a") || !strings.HasPrefix(got[1], "CREATE INDEX CONCURRENTLY b") {
		t.Fatalf("statements: %q", got)
	}
}

func TestVersionedMigrationsRunOnceInOrder(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	order := []string{}
	var mu sync.Mutex
	step := func(name string) func(context.Context, pgx.Tx) error {
		return func(ctx context.Context, tx pgx.Tx) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			_, err := tx.Exec(ctx, `INSERT INTO migration_probe(step) VALUES($1)`, name)
			return err
		}
	}
	list := []migration{
		{version: 1, name: "probe", checksum: "a", sql: `CREATE TABLE migration_probe(step text NOT NULL)`},
		{version: 2, name: "first", checksum: "go:first", run: step("first")},
		{version: 3, name: "index", checksum: "c", noTx: true, sql: "CREATE INDEX CONCURRENTLY migration_probe_idx ON migration_probe(step)\n;\n"},
		{version: 4, name: "second", checksum: "go:second", run: step("second")},
	}
	// Concurrent starters serialize on the lock and apply each step once.
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- migrateWith(ctx, pool, list) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(order, ",") != "first,second" {
		t.Fatalf("steps ran %v", order)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migration WHERE version BETWEEN 1 AND 4`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("recorded=%d err=%v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname='migration_probe_idx'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent index missing: %d %v", count, err)
	}
	// An applied migration whose content changed is refused.
	changed := append([]migration(nil), list...)
	changed[0].checksum = "edited"
	if err := migrateWith(ctx, pool, changed); err == nil || !strings.Contains(err.Error(), "changed after it was applied") {
		t.Fatalf("edited migration accepted: %v", err)
	}
	// A failing migration leaves no partial result and is retried next start.
	failing := append(append([]migration(nil), list...), migration{version: 5, name: "broken", checksum: "go:broken", run: func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO migration_probe(step) VALUES('partial')`); err != nil {
			return err
		}
		return errors.New("boom")
	}})
	if err := migrateWith(ctx, pool, failing); err == nil {
		t.Fatal("failure not reported")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM migration_probe WHERE step='partial'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial migration kept: %d %v", count, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migration WHERE version=5`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration recorded: %d %v", count, err)
	}
}

func TestInvalidConcurrentIndexIsRebuilt(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	// A unique index over duplicates fails and stays behind as invalid.
	list := []migration{{version: 1, name: "table", checksum: "a", sql: `CREATE TABLE dup_probe(v int); INSERT INTO dup_probe VALUES (1),(1)`}}
	if err := migrateWith(ctx, pool, list); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `CREATE UNIQUE INDEX CONCURRENTLY dup_probe_idx ON dup_probe(v)`); err == nil {
		t.Fatal("expected the duplicate rows to fail the unique index")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM dup_probe WHERE ctid=(SELECT min(ctid) FROM dup_probe)`); err != nil {
		t.Fatal(err)
	}
	list = append(list, migration{version: 2, name: "index", checksum: "b", noTx: true, sql: "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS dup_probe_idx ON dup_probe(v)\n;\n"})
	if err := migrateWith(ctx, pool, list); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := pool.QueryRow(ctx, `SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='dup_probe_idx' AND c.relnamespace=current_schema()::regnamespace`).Scan(&valid); err != nil || !valid {
		t.Fatalf("index valid=%v err=%v", valid, err)
	}
}
