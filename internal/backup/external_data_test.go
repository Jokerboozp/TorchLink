package backup

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExternalDataLegacyBackupCompatibility(t *testing.T) {
	res := &RestoreResult{Components: map[string]any{}}
	if err := (&Service{}).restoreExternalData(context.Background(), nil, Manifest{FormatVersion: 2, Type: "FULL", Components: map[string]any{}}, res, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if res.Components["externalData"].(map[string]any)["status"] != "not_included" {
		t.Fatal("legacy backup falsely claimed external data recovery")
	}
	for _, component := range []any{"invalid", map[string]any{"status": "unknown"}} {
		if err := (&Service{}).restoreExternalData(context.Background(), nil, Manifest{Components: map[string]any{"externalData": component}}, res, t.TempDir()); err == nil {
			t.Fatal("invalid component accepted")
		}
	}
}

func TestExternalDataBackupRestorePostgres(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("test PostgreSQL connection failed")
	}
	defer admin.Close(ctx)
	schema := "external_backup_test_" + time.Now().UTC().Format("20060102150405_000000000")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	if _, err = admin.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schema, "external_data_entry"}.Sanitize()+externalDataTableDefinition); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("test PostgreSQL configuration invalid")
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `INSERT INTO external_data_entry(tenant_id,kind,id,status,owner,lease_until,revision,created_at,updated_at,body) VALUES('t','record','r','RUNNING','worker-private',10,7,1,2,'{"raw":{"alarm":"火警"},"credential":"encrypted-fixture"}'),('other','job','j','PENDING','',0,2,1,1,'{"cursor":"next"}')`); err != nil {
		t.Fatal(err)
	}
	m := Manifest{Components: map[string]any{}}
	path, err := (&Service{pool: pool}).exportExternalData(ctx, t.TempDir(), &m)
	if err != nil || manifestRecords(m.Components["externalData"]) != 2 {
		t.Fatalf("export: %v %v", m.Components, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM external_data_entry`); err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, err := restoreExternalDataRows(ctx, tx, schema, path)
	if err != nil || n != 2 {
		tx.Rollback(ctx)
		t.Fatalf("restore rows=%d err=%v", n, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var owner, alarm string
	var revision, lease int64
	if err = pool.QueryRow(ctx, `SELECT owner,revision,lease_until,body#>>'{raw,alarm}' FROM external_data_entry WHERE tenant_id='t' AND kind='record' AND id='r'`).Scan(&owner, &revision, &lease, &alarm); err != nil {
		t.Fatal(err)
	}
	if owner != "worker-private" || revision != 7 || lease != 10 || alarm != "火警" {
		t.Fatalf("restore changed worker fence or body: %s %d %d %s", owner, revision, lease, alarm)
	}
	for _, bad := range []externalDataBackupRow{
		{Kind: "record", ID: "bad", Revision: 1, Body: json.RawMessage(`{}`)},
		{TenantID: "t", Kind: "record", ID: "bad", Revision: 0, Body: json.RawMessage(`{}`)},
	} {
		badPath := filepath.Join(t.TempDir(), "invalid.gz")
		if err = writeGzip(badPath, func(w io.Writer) error { return json.NewEncoder(w).Encode(bad) }); err != nil {
			t.Fatal(err)
		}
		badTx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = restoreExternalDataRows(ctx, badTx, schema, badPath)
		badTx.Rollback(ctx)
		if err == nil {
			t.Fatal("invalid backup row accepted")
		}
	}
}
