package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"iot-platform/internal/model"
)

// Large append-mostly tables are range partitioned by month (UTC) on a
// server-assigned time column, so retention drops whole months instead of
// deleting row by row. Migration 10 prepares each table while ingest keeps
// writing; migration 11 renames it to <table>_legacy and attaches it as the
// partition below the cutover, which needs no copy and no rescan. Later
// months get their own partitions; a default partition catches any key that
// arrives before its month was created.
//
// Message IDs stay unique although the primary keys now include the
// partition key: raw messages are reserved in raw_ingest_reservation before
// they are written, standard messages claim standard_message_key, and
// inserts skip IDs that already exist in any partition.

type partitionedTable struct {
	name, key string
	// timestamptz marks a timestamptz key; otherwise it holds epoch ms.
	timestamptz bool
	pk          []string
	// pending, when set, marks rows that must not be dropped with their
	// partition (unpublished raw messages, unprocessed standard messages).
	pending string
}

var partitionedTables = []partitionedTable{
	{name: "raw_archive_index", key: "received_at", pk: []string{"tenant_id", "message_id"}, pending: "published_at = 0"},
	{name: "raw_message_log", key: "received_at", pk: []string{"tenant_id", "message_id"}},
	{name: "standard_message", key: "created_at", pk: []string{"tenant_id", "message_id"}, pending: "processed_at = 0"},
	{name: "device_state_event", key: "created_at", timestamptz: true, pk: []string{"id"}},
	{name: "audit_log", key: "created_at", pk: []string{"id"}},
}

// partitionsAhead is how many months after the current one get partitions.
const partitionsAhead = 3

func partitionSpec(table string) (partitionedTable, bool) {
	for _, t := range partitionedTables {
		if t.name == table {
			return t, true
		}
	}
	return partitionedTable{}, false
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func (t partitionedTable) bound(at time.Time) string {
	if t.timestamptz {
		return "'" + at.UTC().Format(time.RFC3339) + "'::timestamptz"
	}
	return fmt.Sprint(at.UnixMilli())
}

func (t partitionedTable) monthName(month time.Time) string {
	return fmt.Sprintf("%s_p%04d%02d", t.name, month.Year(), int(month.Month()))
}

var indexOn = regexp.MustCompile(`^CREATE (UNIQUE )?INDEX (\S+) ON (?:ONLY )?(?:public\.)?(\S+) USING (.*)$`)

// switchToPartitions converts one prepared table inside the migration
// transaction.
func switchToPartitions(ctx context.Context, tx pgx.Tx, t partitionedTable) error {
	var kind string
	if err := tx.QueryRow(ctx, `SELECT relkind::text FROM pg_class WHERE oid=to_regclass($1)`, t.name).Scan(&kind); err != nil {
		return err
	}
	if kind == "p" {
		return nil
	}
	var cutoverMS int64
	if err := tx.QueryRow(ctx, `SELECT cutover_ms FROM partition_cutover WHERE table_name=$1`, t.name).Scan(&cutoverMS); err != nil {
		return fmt.Errorf("%s: cutover not prepared: %w", t.name, err)
	}
	cutover := time.UnixMilli(cutoverMS).UTC()
	legacy := t.name + "_legacy"
	type index struct{ name, def string }
	rows, err := tx.Query(ctx, `SELECT c.relname, pg_get_indexdef(i.indexrelid), i.indisprimary
FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE i.indrelid=to_regclass($1) ORDER BY c.relname`, t.name)
	if err != nil {
		return err
	}
	var indexes []index
	var primary string
	for rows.Next() {
		var ix index
		var isPrimary bool
		if err = rows.Scan(&ix.name, &ix.def, &isPrimary); err != nil {
			rows.Close()
			return err
		}
		switch {
		case isPrimary:
			primary = ix.name
		case ix.name == t.name+"_partition_key":
		default:
			indexes = append(indexes, ix)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	q := func(s string) string { return pgx.Identifier{s}.Sanitize() }
	exec := func(sql string) error {
		_, err := tx.Exec(ctx, sql)
		if err != nil {
			return fmt.Errorf("%s: %w: %s", t.name, err, sql)
		}
		return nil
	}
	if err = exec(`ALTER TABLE ` + q(t.name) + ` RENAME TO ` + q(legacy)); err != nil {
		return err
	}
	// The old primary key lacks the partition key; the unique index built by
	// migration 10 leads with the same columns and still serves lookups by ID.
	if primary != "" {
		if err = exec(`ALTER TABLE ` + q(legacy) + ` DROP CONSTRAINT ` + q(primary)); err != nil {
			return err
		}
	}
	// The prepared index becomes the legacy primary key without a rebuild,
	// so attaching reuses it as this partition's part of the parent key.
	if err = exec(`ALTER TABLE ` + q(legacy) + ` ADD CONSTRAINT ` + q(legacy+"_pkey") + ` PRIMARY KEY USING INDEX ` + q(t.name+"_partition_key")); err != nil {
		return err
	}
	for _, ix := range indexes {
		if err = exec(`ALTER INDEX ` + q(ix.name) + ` RENAME TO ` + q(ix.name+"_legacy")); err != nil {
			return err
		}
	}
	if err = exec(`CREATE TABLE ` + q(t.name) + ` (LIKE ` + q(legacy) + ` INCLUDING DEFAULTS) PARTITION BY RANGE (` + q(t.key) + `)`); err != nil {
		return err
	}
	if err = exec(`ALTER TABLE ` + q(t.name) + ` ADD CONSTRAINT ` + q(t.name+"_pkey") + ` PRIMARY KEY (` + strings.Join(append(append([]string(nil), t.pk...), t.key), ",") + `)`); err != nil {
		return err
	}
	// The parent keeps the original index names, so the idempotent baseline
	// schema finds them and never rebuilds them across every partition.
	for _, ix := range indexes {
		m := indexOn.FindStringSubmatch(ix.def)
		if m == nil || m[1] != "" {
			// A unique index without the partition key stays on the legacy
			// partition only.
			continue
		}
		if err = exec(`CREATE INDEX ` + q(ix.name) + ` ON ` + q(t.name) + ` USING ` + m[4]); err != nil {
			return err
		}
	}
	// Sequences owned by the legacy columns (bigserial) must survive when
	// the legacy partition is dropped.
	if _, err = tx.Exec(ctx, `DO $$ DECLARE s record; BEGIN
FOR s IN SELECT pg_get_serial_sequence('`+legacy+`', a.attname) AS seq, a.attname FROM pg_attribute a
  WHERE a.attrelid=to_regclass('`+legacy+`') AND a.attnum>0 AND NOT a.attisdropped AND pg_get_serial_sequence('`+legacy+`', a.attname) IS NOT NULL
LOOP EXECUTE format('ALTER SEQUENCE %s OWNED BY %I.%I', s.seq, '`+t.name+`', s.attname); END LOOP; END $$`); err != nil {
		return fmt.Errorf("%s: sequence ownership: %w", t.name, err)
	}
	if err = exec(`ALTER TABLE ` + q(t.name) + ` ATTACH PARTITION ` + q(legacy) + ` FOR VALUES FROM (MINVALUE) TO (` + t.bound(cutover) + `)`); err != nil {
		return err
	}
	if err = exec(`CREATE TABLE ` + q(t.name+"_default") + ` PARTITION OF ` + q(t.name) + ` DEFAULT`); err != nil {
		return err
	}
	for month := cutover; month.Before(monthStart(time.Now()).AddDate(0, partitionsAhead+1, 0)) || month.Equal(cutover); month = month.AddDate(0, 1, 0) {
		if err = createMonth(ctx, tx, t, month); err != nil {
			return err
		}
	}
	return nil
}

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// createMonth adds the partition for one month at or after the cutover.
func createMonth(ctx context.Context, db execer, t partitionedTable, month time.Time) error {
	q := func(s string) string { return pgx.Identifier{s}.Sanitize() }
	_, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+q(t.monthName(month))+` PARTITION OF `+q(t.name)+
		` FOR VALUES FROM (`+t.bound(month)+`) TO (`+t.bound(month.AddDate(0, 1, 0))+`)`)
	if err != nil {
		return fmt.Errorf("create partition %s: %w", t.monthName(month), err)
	}
	return nil
}

func migratePartitions(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '60s'`); err != nil {
		return err
	}
	for _, t := range partitionedTables {
		if err := switchToPartitions(ctx, tx, t); err != nil {
			return err
		}
	}
	return nil
}

func init() { registerMigration(11, "partition_large_tables", migratePartitions) }

// isPartitioned reports whether table has been switched.
func (r *Repository) isPartitioned(ctx context.Context, table string) (bool, error) {
	var kind *string
	err := r.pool.QueryRow(ctx, `SELECT relkind::text FROM pg_class WHERE oid=to_regclass($1)`, table).Scan(&kind)
	return err == nil && kind != nil && *kind == "p", err
}

// EnsurePartitions creates the partitions of the current month and the
// next months, so rows rarely fall into the default partition. A month
// whose rows already reached the default partition cannot be created and
// is reported; those rows stay queryable in the default partition.
func (r *Repository) EnsurePartitions(ctx context.Context, now time.Time) error {
	var failures []error
	for _, t := range partitionedTables {
		partitioned, err := r.isPartitioned(ctx, t.name)
		if err != nil || !partitioned {
			failures = append(failures, err)
			continue
		}
		var cutoverMS int64
		if err = r.pool.QueryRow(ctx, `SELECT cutover_ms FROM partition_cutover WHERE table_name=$1`, t.name).Scan(&cutoverMS); err != nil {
			failures = append(failures, err)
			continue
		}
		cutover := time.UnixMilli(cutoverMS).UTC()
		for month := monthStart(now); !month.After(monthStart(now).AddDate(0, partitionsAhead, 0)); month = month.AddDate(0, 1, 0) {
			if month.Before(cutover) {
				continue
			}
			if err = createMonth(ctx, r.pool, t, month); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

var monthPartition = regexp.MustCompile(`_p(\d{4})(\d{2})$`)

// ExpiredPartitions lists the monthly partitions of table that end at or
// before cutoff, oldest first.
func (r *Repository) ExpiredPartitions(ctx context.Context, table string, cutoff time.Time) ([]model.TablePartition, error) {
	spec, ok := partitionSpec(table)
	if !ok {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid WHERE i.inhparent=to_regclass($1) ORDER BY c.relname`, spec.name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TablePartition{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		m := monthPartition.FindStringSubmatch(name)
		if m == nil || !strings.HasPrefix(name, spec.name+"_p") {
			continue
		}
		var year, month int
		if _, err := fmt.Sscanf(m[1]+" "+m[2], "%d %d", &year, &month); err != nil {
			continue
		}
		from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		if to := from.AddDate(0, 1, 0); !to.After(cutoff) {
			out = append(out, model.TablePartition{Name: name, From: from, To: to})
		}
	}
	return out, rows.Err()
}

// DropPartition drops one monthly partition of table unless it still holds
// rows that must be kept (unpublished or unprocessed messages).
func (r *Repository) DropPartition(ctx context.Context, table, partition string) (bool, error) {
	spec, ok := partitionSpec(table)
	if !ok || !strings.HasPrefix(partition, spec.name) {
		return false, fmt.Errorf("unknown partition %s of %s", partition, table)
	}
	return r.dropChild(ctx, spec, partition)
}

// DropEmptyLegacy drops the legacy partition once retention emptied it.
func (r *Repository) DropEmptyLegacy(ctx context.Context, table string) (bool, error) {
	spec, ok := partitionSpec(table)
	if !ok {
		return false, nil
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, spec.name+"_legacy").Scan(&exists); err != nil || !exists {
		return false, err
	}
	var empty bool
	if err := r.pool.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM `+pgx.Identifier{spec.name + "_legacy"}.Sanitize()+`)`).Scan(&empty); err != nil || !empty {
		return false, err
	}
	return r.dropChild(ctx, spec, spec.name+"_legacy")
}

func (r *Repository) dropChild(ctx context.Context, spec partitionedTable, child string) (bool, error) {
	dropped := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
			return err
		}
		if spec.pending != "" {
			var pending bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+pgx.Identifier{child}.Sanitize()+` WHERE `+spec.pending+`)`).Scan(&pending); err != nil || pending {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DROP TABLE `+pgx.Identifier{child}.Sanitize()); err != nil {
			return err
		}
		dropped = true
		return nil
	})
	return dropped, err
}

// leafTables lists the tables holding table's rows: its partitions, or
// the table itself when it is not partitioned.
func (r *Repository) leafTables(ctx context.Context, table string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid WHERE i.inhparent=to_regclass($1) AND c.relkind='r' ORDER BY c.relname`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		out = []string{table}
	}
	return out, rows.Err()
}
