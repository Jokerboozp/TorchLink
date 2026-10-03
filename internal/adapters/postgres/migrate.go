package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schema.sql is the idempotent baseline (version 0). Every later change is a
// numbered migration that runs exactly once, in order, after the baseline:
//
//   - migrations/NNNN_name.sql runs in one transaction; a file whose first
//     line is "-- migrate:no-transaction" runs statement by statement outside
//     a transaction (for CREATE INDEX CONCURRENTLY on large tables).
//   - Go migrations registered with registerMigration move or reshape data.
//
// Applied versions are recorded in schema_migration with a checksum, so an
// edited migration is refused instead of silently diverging between hosts.

//go:embed migrations
var migrationFiles embed.FS

const migrationLock = 728194602

const noTransactionDirective = "-- migrate:no-transaction"

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
	noTx     bool
	run      func(context.Context, pgx.Tx) error
}

var goMigrations []migration

// registerMigration adds a data migration implemented in Go. It runs in its
// own transaction after the SQL migrations with smaller versions.
func registerMigration(version int, name string, run func(context.Context, pgx.Tx) error) {
	goMigrations = append(goMigrations, migration{version: version, name: name, checksum: "go:" + name, run: run})
}

func loadMigrations() ([]migration, error) {
	out := append([]migration(nil), goMigrations...)
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		number, rest, ok := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		version, convErr := strconv.Atoi(number)
		if !ok || convErr != nil || version <= 0 {
			return nil, fmt.Errorf("migration file %s must be named NNNN_name.sql", name)
		}
		data, err := migrationFiles.ReadFile(path.Join("migrations", name))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		text := string(data)
		out = append(out, migration{version: version, name: rest, checksum: hex.EncodeToString(sum[:]), sql: text, noTx: strings.HasPrefix(strings.TrimSpace(text), noTransactionDirective)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d (%s, %s)", out[i].version, out[i-1].name, out[i].name)
		}
	}
	return out, nil
}

// Migrate applies the baseline schema and every pending migration. A
// session-level advisory lock serializes concurrent starters (API replicas,
// workers, cluster-init); the others wait and then find nothing to do.
func (r *Repository) Migrate(ctx context.Context) error {
	return migrate(ctx, r.pool)
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	return migrateWith(ctx, pool, migrations)
}

func migrateWith(ctx context.Context, pool *pgxpool.Pool, migrations []migration) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	// Waiters poll instead of blocking in pg_advisory_lock: a blocked
	// statement holds a snapshot, which CREATE INDEX CONCURRENTLY in the
	// running migrator would wait for, deadlocking both.
	for {
		var locked bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, int64(migrationLock)).Scan(&locked); err != nil {
			return err
		}
		if locked {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(migrationLock)) }()
	if err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, schema); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migration (
  version integer PRIMARY KEY, name text NOT NULL, checksum text NOT NULL,
  applied_at timestamptz NOT NULL DEFAULT now()
)`)
		return err
	}); err != nil {
		return fmt.Errorf("baseline schema: %w", err)
	}
	applied := map[int]string{}
	rows, err := conn.Query(ctx, `SELECT version,checksum FROM schema_migration`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var version int
		var checksum string
		if err = rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return err
		}
		applied[version] = checksum
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, m := range migrations {
		if checksum, ok := applied[m.version]; ok {
			if checksum != m.checksum {
				return fmt.Errorf("migration %04d_%s was changed after it was applied", m.version, m.name)
			}
			continue
		}
		if err = applyMigration(ctx, conn.Conn(), m); err != nil {
			return fmt.Errorf("migration %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *pgx.Conn, m migration) error {
	if m.noTx {
		// Statements are separated by lines containing only ";" so that
		// bodies may contain semicolons; each runs on its own.
		for _, statement := range splitStatements(m.sql) {
			if err := dropInvalidIndex(ctx, conn, statement); err != nil {
				return err
			}
			if _, err := conn.Exec(ctx, statement); err != nil {
				return err
			}
		}
		_, err := conn.Exec(ctx, `INSERT INTO schema_migration(version,name,checksum) VALUES($1,$2,$3)`, m.version, m.name, m.checksum)
		return err
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if m.run != nil {
			if err := m.run(ctx, tx); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, m.sql); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migration(version,name,checksum) VALUES($1,$2,$3)`, m.version, m.name, m.checksum)
		return err
	})
}

var concurrentIndex = regexp.MustCompile(`(?i)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+IF\s+NOT\s+EXISTS\s+([a-z_][a-z0-9_]*)\s`)

// dropInvalidIndex removes an index left invalid by an interrupted CREATE
// INDEX CONCURRENTLY, which IF NOT EXISTS would otherwise keep forever.
func dropInvalidIndex(ctx context.Context, conn *pgx.Conn, statement string) error {
	match := concurrentIndex.FindStringSubmatch(statement)
	if match == nil {
		return nil
	}
	var invalid bool
	err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
  WHERE c.relname=$1 AND c.relnamespace=current_schema()::regnamespace AND NOT i.indisvalid)`, match[1]).Scan(&invalid)
	if err != nil || !invalid {
		return err
	}
	_, err = conn.Exec(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+pgx.Identifier{match[1]}.Sanitize())
	return err
}

func splitStatements(sql string) []string {
	out := []string{}
	var current strings.Builder
	flush := func() {
		if statement := strings.TrimSpace(current.String()); statement != "" {
			out = append(out, statement)
		}
		current.Reset()
	}
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == ";" {
			flush()
			continue
		}
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	flush()
	return out
}
