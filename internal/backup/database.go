package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
)

// DATABASE backups dump the whole platform PostgreSQL database with pg_dump
// (custom format), so users, roles, device templates and credentials, fire
// safety records, alarms, notification settings and every other business
// table are included, plus the ClickHouse telemetry and raw tables in their
// native format. Device-message backups (DEVICE_DAILY, FULL) remain the
// day-by-day exports; this type is the disaster-recovery copy.
const (
	databaseDumpFile = "postgresql.dump"
	databaseType     = "DATABASE"
)

var clickHouseTables = []string{"iot_telemetry", "iot_raw_message"}

// pgEnv turns a DSN into libpq environment variables so the password never
// appears on a command line.
func pgEnv(dsn string) ([]string, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("PostgreSQL connection string is invalid")
	}
	env := []string{"PGHOST=" + cfg.Host, "PGPORT=" + strconv.Itoa(int(cfg.Port)), "PGUSER=" + cfg.User, "PGDATABASE=" + cfg.Database, "PGCONNECT_TIMEOUT=10"}
	if cfg.Password != "" {
		env = append(env, "PGPASSWORD="+cfg.Password)
	}
	sslmode := "prefer"
	if cfg.TLSConfig == nil {
		sslmode = "disable"
	}
	if u, err := url.Parse(dsn); err == nil && u.Query().Get("sslmode") != "" {
		sslmode = u.Query().Get("sslmode")
	}
	env = append(env, "PGSSLMODE="+sslmode)
	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	return env, nil
}

func (s *Service) tool(name string) string {
	if dir := s.cfg.PostgresToolsDir; dir != "" {
		return filepath.Join(dir, name)
	}
	return name
}

func runTool(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		text := strings.TrimSpace(stderr.String())
		if len(text) > 500 {
			text = text[:500]
		}
		return out, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, text)
	}
	return out, nil
}

// dumpPostgres writes a pg_dump custom archive and returns its table count.
func (s *Service) dumpPostgres(ctx context.Context, path string) (int, error) {
	env, err := pgEnv(s.cfg.PostgresDSN)
	if err != nil {
		return 0, err
	}
	if _, err = runTool(ctx, env, s.tool("pg_dump"), "--format=custom", "--no-owner", "--no-privileges", "--file="+path); err != nil {
		return 0, err
	}
	return s.archiveTables(ctx, path)
}

// archiveTables lists the archive's table data entries with pg_restore,
// which also proves the archive is readable.
func (s *Service) archiveTables(ctx context.Context, path string) (int, error) {
	out, err := runTool(ctx, []string{"PATH=" + os.Getenv("PATH")}, s.tool("pg_restore"), "--list", path)
	if err != nil {
		return 0, err
	}
	tables := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, " TABLE DATA ") {
			tables++
		}
	}
	return tables, nil
}

// exportClickHouseNative streams one table in ClickHouse's Native format.
func (s *Service) exportClickHouseNative(ctx context.Context, table, path string) (int64, error) {
	u, err := url.Parse(strings.TrimRight(s.cfg.ClickHouseURL, "/") + "/")
	if err != nil {
		return 0, err
	}
	q := u.Query()
	q.Set("query", "SELECT * FROM "+table+" FORMAT Native")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := (&http.Client{Timeout: 6 * time.Hour}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("clickhouse %s: %s", resp.Status, body)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	gz := gzip.NewWriter(f)
	n, copyErr := io.Copy(gz, resp.Body)
	closeErr := errors.Join(gz.Close(), f.Close())
	return n, errors.Join(copyErr, closeErr)
}

func (s *Service) runDatabase(ctx context.Context) (manifest Manifest, err error) {
	if !s.mu.TryLock() {
		return manifest, fmt.Errorf("a backup is already running")
	}
	defer s.mu.Unlock()
	id := newBackupID(databaseType, time.Now())
	manifest = Manifest{FormatVersion: 2, ID: id, Type: databaseType, CreatedAt: time.Now().UTC(), Components: map[string]any{
		"scope": "whole PostgreSQL database (pg_dump custom format) and ClickHouse telemetry/raw tables (Native)",
	}}
	if _, err = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,$2,'RUNNING',now())`, id, databaseType); err != nil {
		return manifest, err
	}
	defer s.finishFailed(id, &manifest, &err)
	dir := filepath.Join(s.cfg.BackupDir, id)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return manifest, err
	}
	defer os.RemoveAll(dir)
	dump := filepath.Join(dir, databaseDumpFile)
	tables, err := s.dumpPostgres(ctx, dump)
	if err != nil {
		return manifest, fmt.Errorf("pg_dump: %w", err)
	}
	manifest.Components["postgresql"] = map[string]any{"tables": tables, "format": "pg_dump custom"}
	paths := []string{dump}
	if s.cfg.ClickHouseURL != "" {
		exported := map[string]any{}
		for _, table := range clickHouseTables {
			path := filepath.Join(dir, "clickhouse-"+table+".native.gz")
			bytes, exportErr := s.exportClickHouseNative(ctx, table, path)
			if exportErr != nil {
				return manifest, fmt.Errorf("clickhouse %s: %w", table, exportErr)
			}
			exported[table] = map[string]any{"bytes": bytes}
			paths = append(paths, path)
		}
		manifest.Components["clickhouse"] = exported
	}
	return manifest, s.publish(ctx, id, dir, paths, &manifest)
}

// finishFailed records a failed run; shared by every backup type.
func (s *Service) finishFailed(id string, manifest *Manifest, err *error) {
	if *err == nil {
		return
	}
	s.failed.Add(1)
	s.lastError.Store((*err).Error())
	details, _ := jsonMarshal(map[string]any{"error": (*err).Error(), "components": manifest.Components})
	updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.pool.Exec(updateCtx, `UPDATE backup_task SET status='FAILED',details=$2,completed_at=now() WHERE id=$1`, id, details)
}

// publish uploads the artifacts and manifest, copies them off site when
// configured and marks the task completed.
func (s *Service) publish(ctx context.Context, id, dir string, paths []string, manifest *Manifest) error {
	if err := s.ensureBucket(ctx, s.store, s.cfg.BackupBucket); err != nil {
		return err
	}
	for _, path := range paths {
		artifact, err := s.uploadAndVerify(ctx, id, path)
		if err != nil {
			return err
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	if s.offsite != nil {
		manifest.Components["offsite"] = map[string]any{"endpoint": s.cfg.OffsiteEndpoint, "bucket": s.cfg.OffsiteBucket}
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := writeJSON(manifestPath, manifest); err != nil {
		return err
	}
	artifact, err := s.uploadAndVerify(ctx, id, manifestPath)
	if err != nil {
		return err
	}
	manifest.Artifacts = append(manifest.Artifacts, artifact)
	if err = s.copyOffsite(ctx, id, append(append([]string(nil), paths...), manifestPath)); err != nil {
		return fmt.Errorf("off-site copy: %w", err)
	}
	details, _ := jsonMarshal(manifest)
	if _, err = s.pool.Exec(ctx, `UPDATE backup_task SET status='COMPLETED',object_key=$2,checksum=$3,details=$4,completed_at=now() WHERE id=$1`, id, artifact.ObjectKey, artifact.SHA256, details); err != nil {
		return err
	}
	s.success.Add(1)
	s.lastOK.Store(time.Now().Unix())
	return nil
}

// copyOffsite writes the artifacts to a second, independent object store
// and reads each back to compare its checksum.
func (s *Service) copyOffsite(ctx context.Context, id string, paths []string) error {
	if s.offsite == nil {
		return nil
	}
	if err := s.ensureBucket(ctx, s.offsite, s.cfg.OffsiteBucket); err != nil {
		return err
	}
	for _, path := range paths {
		checksum, size, err := hashFile(path)
		if err != nil {
			return err
		}
		key := "backup/" + id + "/" + filepath.Base(path)
		if _, err = s.offsite.FPutObject(ctx, s.cfg.OffsiteBucket, key, path, minio.PutObjectOptions{ContentType: "application/octet-stream", UserMetadata: map[string]string{"sha256": checksum}}); err != nil {
			return err
		}
		stat, err := s.offsite.StatObject(ctx, s.cfg.OffsiteBucket, key, minio.StatObjectOptions{})
		if err != nil || stat.Size != size || stat.UserMetadata["Sha256"] != checksum {
			return fmt.Errorf("verify %s", filepath.Base(path))
		}
	}
	return nil
}

// restoreDatabase restores a DATABASE backup into the dedicated drill
// database: its public schema is replaced, then the restored table count is
// compared with the manifest. The live database is never touched.
func (s *Service) restoreDatabase(ctx context.Context, res *RestoreResult, manifest Manifest, stage string) error {
	if s.cfg.RestoreDatabaseDSN == "" {
		return ErrRestoreDatabaseNotConfigured
	}
	if err := restoreTargetSafe(s.cfg.PostgresDSN, s.cfg.RestoreDatabaseDSN); err != nil {
		return err
	}
	if s.cfg.RestoreTargetDSN != "" && restoreTargetSafe(s.cfg.RestoreTargetDSN, s.cfg.RestoreDatabaseDSN) != nil {
		return errors.New("the whole-database drill target must differ from the message restore target")
	}
	file, err := s.downloadVerifiedArtifact(ctx, manifest, res.BackupID, databaseDumpFile, stage)
	if err != nil {
		return err
	}
	target, err := pgx.Connect(ctx, s.cfg.RestoreDatabaseDSN)
	if err != nil {
		return errors.New("whole-database drill target is unavailable")
	}
	_, err = target.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public`)
	if err == nil {
		_, err = target.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`)
	}
	target.Close(context.WithoutCancel(ctx))
	if err != nil {
		return fmt.Errorf("prepare drill database: %w", err)
	}
	env, err := pgEnv(s.cfg.RestoreDatabaseDSN)
	if err != nil {
		return err
	}
	if _, err = runTool(ctx, env, s.tool("pg_restore"), "--no-owner", "--no-privileges", "--exit-on-error", "--dbname="+envValue(env, "PGDATABASE"), file); err != nil {
		return err
	}
	target, err = pgx.Connect(ctx, s.cfg.RestoreDatabaseDSN)
	if err != nil {
		return errors.New("whole-database drill target is unavailable")
	}
	defer target.Close(context.WithoutCancel(ctx))
	var restored int64
	if err = target.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public'`).Scan(&restored); err != nil {
		return err
	}
	expected := manifestRecords(map[string]any{"records": tableCount(manifest.Components["postgresql"])})
	res.Kinds["postgresql"] = RestoreSummary{Expected: expected, Restored: restored, Matches: restored >= expected && expected > 0}
	res.Components["postgresql"] = map[string]any{"target": "IOT_BACKUP_RESTORE_DATABASE_DSN", "tables": restored}
	return nil
}

func tableCount(component any) int64 {
	if m, ok := component.(map[string]any); ok {
		switch v := m["tables"].(type) {
		case float64:
			return int64(v)
		case int:
			return int64(v)
		}
	}
	return 0
}

func envValue(env []string, key string) string {
	for _, item := range env {
		if v, ok := strings.CutPrefix(item, key+"="); ok {
			return v
		}
	}
	return ""
}

// PruneDatabaseBackups deletes completed DATABASE backups beyond the newest
// keep ones, with their artifacts. Off-site copies follow the off-site
// store's own lifecycle rules.
func (s *Service) PruneDatabaseBackups(ctx context.Context, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM backup_task WHERE backup_type=$1 AND status='COMPLETED' ORDER BY completed_at DESC NULLS LAST OFFSET $2`, databaseType, keep)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	deleted := 0
	for _, id := range ids {
		if err = s.DeleteTask(ctx, id); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
