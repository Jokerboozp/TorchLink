package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func verifyRetiredDeviceTablesUntouched(t *testing.T, r *Repository) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"device_shadow", "device_shadow_change", "device_twin_topology"} {
		var count int
		if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("fresh schema creates retired table %s: %d %v", table, count, err)
		}
		// An arbitrary old schema must remain untouched; no runtime reads it anymore.
		ident := pgx.Identifier{table}.Sanitize()
		if _, err := r.pool.Exec(ctx, "CREATE TABLE "+ident+" (id text PRIMARY KEY, body jsonb NOT NULL)"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.pool.Exec(ctx, "INSERT INTO "+ident+" VALUES ('retained', '{\"version\":7}')"); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"device_shadow", "device_shadow_change", "device_twin_topology"} {
		var version int
		if err := r.pool.QueryRow(ctx, "SELECT (body->>'version')::int FROM "+pgx.Identifier{table}.Sanitize()+" WHERE id='retained'").Scan(&version); err != nil || version != 7 {
			t.Fatalf("retired data changed in %s: %d %v", table, version, err)
		}
	}
}
