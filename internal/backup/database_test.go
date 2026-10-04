package backup

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestPGEnvKeepsPasswordOffTheCommandLine(t *testing.T) {
	env, err := pgEnv("postgres://iot:s3cret@db.example:5433/iot?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	for _, want := range []string{"PGHOST=db.example", "PGPORT=5433", "PGUSER=iot", "PGPASSWORD=s3cret", "PGDATABASE=iot", "PGSSLMODE=require"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, env)
		}
	}
}

// IOT_TEST_PG_TOOLS_DIR points at pg_dump/pg_restore of PostgreSQL 17 or
// newer; IOT_TEST_POSTGRES_DSN and IOT_TEST_MINIO_* at disposable services.
// The test creates and drops its own databases and bucket.
func TestDatabaseBackupAndDrillRestore(t *testing.T) {
	tools, dsn := os.Getenv("IOT_TEST_PG_TOOLS_DIR"), os.Getenv("IOT_TEST_POSTGRES_DSN")
	endpoint := os.Getenv("IOT_TEST_MINIO_ENDPOINT")
	if tools == "" || dsn == "" || endpoint == "" {
		t.Skip("IOT_TEST_PG_TOOLS_DIR, IOT_TEST_POSTGRES_DSN and IOT_TEST_MINIO_ENDPOINT are required")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	suffix := fmt.Sprint(time.Now().UnixNano())
	source, drill := "backup_src_"+suffix, "backup_drill_"+suffix
	dbDSN := func(name string) string {
		u, _ := url.Parse(dsn)
		u.Path = "/" + name
		return u.String()
	}
	for _, name := range []string{source, drill} {
		if _, err = admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		defer admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	}
	src, err := pgxpool.New(ctx, dbDSN(source))
	if err != nil {
		t.Fatal(err)
	}
	// backup_task is the backup service's own table; two business tables
	// stand in for the platform schema.
	_, err = src.Exec(ctx, `CREATE TABLE backup_task (id text PRIMARY KEY, backup_type text NOT NULL, status text NOT NULL, object_key text, checksum text, details jsonb NOT NULL DEFAULT '{}', started_at timestamptz, completed_at timestamptz);
CREATE TABLE platform_user_probe (id text PRIMARY KEY, name text);
INSERT INTO platform_user_probe VALUES ('u1','张三'),('u2','李四');
CREATE TABLE fire_probe (id serial PRIMARY KEY, body jsonb);
INSERT INTO fire_probe(body) VALUES ('{"x":1}')`)
	src.Close()
	if err != nil {
		t.Fatal(err)
	}
	bucket := "backup-test-" + suffix
	cfg := Config{PostgresDSN: dbDSN(source), BackupDir: t.TempDir(), BackupBucket: bucket, MinIOEndpoint: endpoint, MinIOAccessKey: os.Getenv("IOT_TEST_MINIO_ACCESS_KEY"), MinIOSecretKey: os.Getenv("IOT_TEST_MINIO_SECRET_KEY"),
		RestoreDatabaseDSN: dbDSN(drill), PostgresToolsDir: tools, OffsiteEndpoint: endpoint, OffsiteBucket: bucket + "-offsite", OffsiteAccessKey: os.Getenv("IOT_TEST_MINIO_ACCESS_KEY"), OffsiteSecretKey: os.Getenv("IOT_TEST_MINIO_SECRET_KEY")}
	service, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	store, _ := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, "")})
	defer func() {
		for _, b := range []string{bucket, bucket + "-offsite"} {
			for obj := range store.ListObjects(context.Background(), b, minio.ListObjectsOptions{Recursive: true}) {
				_ = store.RemoveObject(context.Background(), b, obj.Key, minio.RemoveObjectOptions{})
			}
			_ = store.RemoveBucket(context.Background(), b)
		}
	}()
	manifest, err := service.Run(ctx, "DATABASE")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(manifest.ID, "backup_database_") || tableCount(manifest.Components["postgresql"]) != 3 {
		t.Fatalf("manifest %+v", manifest)
	}
	offsite := 0
	for obj := range store.ListObjects(ctx, bucket+"-offsite", minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err == nil {
			offsite++
		}
	}
	if offsite != len(manifest.Artifacts) {
		t.Fatalf("off-site copies %d, artifacts %d", offsite, len(manifest.Artifacts))
	}
	result, err := service.Restore(ctx, manifest.ID)
	if err != nil || result.Status != "COMPLETED" {
		t.Fatalf("restore %+v %v", result, err)
	}
	restored, err := pgx.Connect(ctx, dbDSN(drill))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(ctx)
	var name string
	if err = restored.QueryRow(ctx, `SELECT name FROM platform_user_probe WHERE id='u2'`).Scan(&name); err != nil || name != "李四" {
		t.Fatalf("restored row %q %v", name, err)
	}
	// The drill target must never be the live database.
	service.cfg.RestoreDatabaseDSN = cfg.PostgresDSN
	if _, err = service.Restore(ctx, manifest.ID); err == nil {
		t.Fatal("restoring over the source database was accepted")
	}
}
