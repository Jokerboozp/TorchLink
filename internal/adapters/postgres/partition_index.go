package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// createPartitionedIndex builds index on a (possibly partitioned) table
// without blocking writes: each partition gets its own index concurrently,
// then the parent index is created ON ONLY and the partition indexes are
// attached. Partitions created later inherit the parent index. Every step
// is idempotent, so an interrupted migration can simply run again.
func createPartitionedIndex(ctx context.Context, conn *pgx.Conn, table, index, definition string) error {
	q := func(s string) string { return pgx.Identifier{s}.Sanitize() }
	var kind string
	if err := conn.QueryRow(ctx, `SELECT relkind::text FROM pg_class WHERE oid=to_regclass($1)`, table).Scan(&kind); err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	if kind != "p" {
		statement := `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + index + ` ON ` + q(table) + ` ` + definition
		if err := dropInvalidIndex(ctx, conn, statement); err != nil {
			return err
		}
		_, err := conn.Exec(ctx, statement)
		return err
	}
	rows, err := conn.Query(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid WHERE i.inhparent=to_regclass($1) AND c.relkind='r' ORDER BY c.relname`, table)
	if err != nil {
		return err
	}
	leaves, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	leafIndex := func(leaf string) string {
		name := leaf + "_" + index
		if len(name) > 63 {
			name = name[:63]
		}
		return name
	}
	for _, leaf := range leaves {
		statement := `CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + leafIndex(leaf) + ` ON ` + q(leaf) + ` ` + definition
		if err = dropInvalidIndex(ctx, conn, statement); err != nil {
			return err
		}
		if _, err = conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("index partition %s: %w", leaf, err)
		}
	}
	if _, err = conn.Exec(ctx, `CREATE INDEX IF NOT EXISTS `+index+` ON ONLY `+q(table)+` `+definition); err != nil {
		return err
	}
	for _, leaf := range leaves {
		var attached bool
		if err = conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_inherits WHERE inhrelid=to_regclass($1) AND inhparent=to_regclass($2))`, leafIndex(leaf), index).Scan(&attached); err != nil {
			return err
		}
		if attached {
			continue
		}
		if _, err = conn.Exec(ctx, `ALTER INDEX `+index+` ATTACH PARTITION `+leafIndex(leaf)); err != nil {
			return fmt.Errorf("attach index of %s: %w", leaf, err)
		}
	}
	return nil
}

// The raw publish retry runs every few seconds looking for archived but
// unpublished messages, and partition removal checks the same condition.
// Nearly every row is published, so a partial index keeps both lookups
// cheap instead of scanning every monthly partition.
func init() {
	registerConnMigration(18, "raw_pending_publish_index", func(ctx context.Context, conn *pgx.Conn) error {
		return createPartitionedIndex(ctx, conn, "raw_archive_index", "raw_archive_index_pending_idx", "(archived_at) WHERE published_at = 0")
	})
}

// The raw message listing pages newest first by (received_at, message_id)
// within a tenant, and its cursor continues after one such key; this index
// serves both without sorting the tenant's rows.
func init() {
	registerConnMigration(23, "raw_listing_index", func(ctx context.Context, conn *pgx.Conn) error {
		return createPartitionedIndex(ctx, conn, "raw_archive_index", "raw_archive_index_listing_idx", "(tenant_id, received_at DESC, message_id DESC)")
	})
}
