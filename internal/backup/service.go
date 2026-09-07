package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	PostgresDSN, BackupDir, BackupBucket, MinIOEndpoint, MinIOAccessKey, MinIOSecretKey string
	MinIODREndpoint, MinIODRAccessKey, MinIODRSecretKey, ClickHouseURL, RedisAddr       string
	RedisPassword, RedpandaAdminURL, WeaviateURL, ConfigPaths                           string
	BackupTimezone                                                                      string
	MinIOUseTLS, MinIODRUseTLS                                                          bool
}

type Artifact struct {
	Component string `json:"component"`
	Filename  string `json:"filename"`
	ObjectKey string `json:"objectKey"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
}

type Manifest struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	CreatedAt  time.Time      `json:"createdAt"`
	Artifacts  []Artifact     `json:"artifacts"`
	Components map[string]any `json:"components"`
}

type rawLogRecord struct {
	Storage string          `json:"storage"`
	Message json.RawMessage `json:"message"`
}

type rawLogStats struct {
	Date       string
	Start      time.Time
	End        time.Time
	PostgreSQL int64
	ClickHouse int64
	Total      int64
}

type Service struct {
	cfg       Config
	pool      *pgxpool.Pool
	store     *minio.Client
	dr        *minio.Client
	mu        sync.Mutex
	success   atomic.Uint64
	failed    atomic.Uint64
	lastOK    atomic.Int64
	lastError atomic.Value
}

func New(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.PostgresDSN == "" || cfg.MinIOEndpoint == "" {
		return nil, fmt.Errorf("postgres and minio configuration are required")
	}
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	store, err := minio.New(cfg.MinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""), Secure: cfg.MinIOUseTLS})
	if err != nil {
		pool.Close()
		return nil, err
	}
	var dr *minio.Client
	if cfg.MinIODREndpoint != "" {
		dr, err = minio.New(cfg.MinIODREndpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.MinIODRAccessKey, cfg.MinIODRSecretKey, ""), Secure: cfg.MinIODRUseTLS})
		if err != nil {
			pool.Close()
			return nil, err
		}
	}
	if cfg.BackupDir == "" {
		cfg.BackupDir = "./data/backups"
	}
	if cfg.BackupBucket == "" {
		cfg.BackupBucket = "iot-backups"
	}
	if cfg.BackupTimezone == "" {
		cfg.BackupTimezone = "Asia/Shanghai"
	}
	return &Service{cfg: cfg, pool: pool, store: store, dr: dr}, nil
}

func (s *Service) Close() { s.pool.Close() }

// Ready checks the catalog created by the platform API and the backup stores.
func (s *Service) Ready(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, "SELECT 1 FROM backup_task LIMIT 0"); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if _, err := s.store.ListBuckets(ctx); err != nil {
		return fmt.Errorf("minio: %w", err)
	}
	if s.dr != nil {
		if _, err := s.dr.ListBuckets(ctx); err != nil {
			return fmt.Errorf("minio DR: %w", err)
		}
	}
	if err := backupToolAvailability(); err != nil {
		return err
	}
	return nil
}

func (s *Service) Run(ctx context.Context, backupType string) (manifest Manifest, err error) {
	if !s.mu.TryLock() {
		return manifest, fmt.Errorf("a backup is already running")
	}
	defer s.mu.Unlock()
	backupType = strings.ToUpper(backupType)
	if backupType != "FULL" && backupType != "INCREMENTAL" && backupType != "RAW_LOGS" {
		return manifest, fmt.Errorf("backup type must be FULL, INCREMENTAL or RAW_LOGS")
	}
	if backupType == "RAW_LOGS" {
		return s.runRawLogsLocked(ctx, time.Now().In(s.rawBackupLocation()).AddDate(0, 0, -1))
	}
	id := newBackupID(backupType, time.Now())
	manifest = Manifest{ID: id, Type: backupType, CreatedAt: time.Now().UTC(), Components: map[string]any{}}
	_, _ = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,$2,'RUNNING',now()) ON CONFLICT DO NOTHING`, id, backupType)
	defer func() {
		if err != nil {
			s.failed.Add(1)
			s.lastError.Store(err.Error())
			details, _ := json.Marshal(map[string]any{"error": err.Error(), "components": manifest.Components})
			_, _ = s.pool.Exec(context.Background(), `UPDATE backup_task SET status='FAILED',details=$2,completed_at=now() WHERE id=$1`, id, details)
		}
	}()
	dir := filepath.Join(s.cfg.BackupDir, id)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return manifest, err
	}

	if backupType == "FULL" {
		if err = s.postgresDump(ctx, filepath.Join(dir, "postgres.dump")); err != nil {
			return manifest, fmt.Errorf("postgres: %w", err)
		}
		manifest.Components["postgres"] = "full logical dump"
	} else if exists("/sources/postgres-wal") {
		if err = tarPaths(filepath.Join(dir, "postgres-wal.tar.gz"), []string{"/sources/postgres-wal"}); err != nil {
			return manifest, fmt.Errorf("postgres wal: %w", err)
		}
		manifest.Components["postgres"] = "WAL archive"
	}
	if s.cfg.ClickHouseURL != "" {
		count, clickErr := s.clickHouseExport(ctx, dir)
		if clickErr != nil {
			return manifest, fmt.Errorf("clickhouse: %w", clickErr)
		}
		manifest.Components["clickhouse"] = map[string]any{"format": "Native", "tables": count}
	}
	if s.cfg.RedisAddr != "" {
		if err = s.redisSnapshot(ctx, filepath.Join(dir, "redis.rdb")); err != nil {
			return manifest, fmt.Errorf("redis: %w", err)
		}
		manifest.Components["redis"] = "RDB snapshot; server AOF remains enabled"
	}
	if s.cfg.RedpandaAdminURL != "" {
		if err = downloadJSON(ctx, strings.TrimRight(s.cfg.RedpandaAdminURL, "/")+"/v1/partitions", filepath.Join(dir, "redpanda-partitions.json")); err != nil {
			return manifest, fmt.Errorf("redpanda inventory: %w", err)
		}
		manifest.Components["redpanda"] = "topic and partition inventory; messages retained by broker and raw archive"
	}
	if backupType == "FULL" {
		paths := splitPaths(s.cfg.ConfigPaths)
		if len(paths) > 0 {
			if err = tarPaths(filepath.Join(dir, "platform-config.tar.gz"), paths); err != nil {
				return manifest, fmt.Errorf("configuration: %w", err)
			}
			manifest.Components["configuration"] = paths
		}
	}
	inv, invErr := s.minioInventory(ctx)
	if invErr != nil {
		return manifest, fmt.Errorf("minio inventory: %w", invErr)
	}
	if err = writeJSON(filepath.Join(dir, "minio-inventory.json"), inv); err != nil {
		return manifest, err
	}
	manifest.Components["minio"] = map[string]any{"versioning": "enabled", "inventory": inv}
	if s.cfg.WeaviateURL != "" {
		if err = s.weaviateBackup(ctx, id); err != nil {
			return manifest, fmt.Errorf("weaviate: %w", err)
		}
		manifest.Components["weaviate"] = "filesystem snapshot " + id
	}

	if err = s.ensureBucket(ctx, s.store, s.cfg.BackupBucket); err != nil {
		return manifest, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	sort.Strings(files)
	for _, path := range files {
		if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
			continue
		}
		artifact, uploadErr := s.uploadAndVerify(ctx, id, path)
		if uploadErr != nil {
			return manifest, uploadErr
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	if s.dr != nil {
		stats, syncErr := s.replicateMinIO(ctx)
		if syncErr != nil {
			return manifest, fmt.Errorf("minio DR replication: %w", syncErr)
		}
		manifest.Components["minioDR"] = stats
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err = writeJSON(manifestPath, manifest); err != nil {
		return manifest, err
	}
	manifestArtifact, err := s.uploadAndVerify(ctx, id, manifestPath)
	if err != nil {
		return manifest, err
	}
	manifest.Artifacts = append(manifest.Artifacts, manifestArtifact)
	// The first replication pass covers every data artifact. Run a second
	// incremental pass so the final manifest, including DR statistics, is also
	// available at the recovery site.
	if s.dr != nil {
		if _, syncErr := s.replicateMinIO(ctx); syncErr != nil {
			return manifest, fmt.Errorf("minio DR manifest replication: %w", syncErr)
		}
	}
	details, _ := json.Marshal(manifest)
	_, err = s.pool.Exec(ctx, `UPDATE backup_task SET status='COMPLETED',object_key=$2,checksum=$3,details=$4,completed_at=now() WHERE id=$1`, id, manifestArtifact.ObjectKey, manifestArtifact.SHA256, details)
	if err != nil {
		return manifest, err
	}
	s.success.Add(1)
	s.lastOK.Store(time.Now().Unix())
	return manifest, nil
}

// RunRawLogs creates one completed-day raw-log backup. Raw payloads are read
// from their database tier and written as a single compressed JSONL artifact;
// no raw message is written to MinIO during device ingest.
func (s *Service) RunRawLogs(ctx context.Context, day time.Time) (manifest Manifest, err error) {
	if !s.mu.TryLock() {
		return manifest, fmt.Errorf("a backup is already running")
	}
	defer s.mu.Unlock()
	return s.runRawLogsLocked(ctx, day)
}

func (s *Service) runRawLogsLocked(ctx context.Context, day time.Time) (manifest Manifest, err error) {
	location := s.rawBackupLocation()
	day = day.In(location)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	date := start.Format("20060102")
	id := "backup_raw_logs_" + date + "_" + time.Now().UTC().Format("150405.000Z")
	manifest = Manifest{ID: id, Type: "RAW_LOGS", CreatedAt: time.Now().UTC(), Components: map[string]any{}}
	_, _ = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,'RAW_LOGS','RUNNING',now()) ON CONFLICT DO NOTHING`, id)
	defer func() {
		if err != nil {
			s.failed.Add(1)
			s.lastError.Store(err.Error())
			details, _ := json.Marshal(map[string]any{"error": err.Error(), "components": manifest.Components})
			_, _ = s.pool.Exec(context.Background(), `UPDATE backup_task SET status='FAILED',details=$2,completed_at=now() WHERE id=$1`, id, details)
		}
	}()

	dir := filepath.Join(s.cfg.BackupDir, id)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return manifest, err
	}
	rawPath := filepath.Join(dir, "raw-logs-"+date+".jsonl.gz")
	stats, exportErr := s.exportRawLogs(ctx, rawPath, start, end)
	if exportErr != nil {
		return manifest, fmt.Errorf("raw logs: %w", exportErr)
	}
	manifest.Components["rawLogs"] = map[string]any{
		"date":       stats.Date,
		"timezone":   location.String(),
		"start":      stats.Start,
		"end":        stats.End,
		"records":    stats.Total,
		"postgresql": stats.PostgreSQL,
		"clickhouse": stats.ClickHouse,
		"format":     "gzip JSONL; each line has storage and message",
	}
	if err = s.ensureBucket(ctx, s.store, s.cfg.BackupBucket); err != nil {
		return manifest, err
	}
	artifact, uploadErr := s.uploadAndVerify(ctx, id, rawPath)
	if uploadErr != nil {
		return manifest, uploadErr
	}
	manifest.Artifacts = append(manifest.Artifacts, artifact)
	if s.dr != nil {
		stats, syncErr := s.replicateMinIO(ctx)
		if syncErr != nil {
			return manifest, fmt.Errorf("minio DR replication: %w", syncErr)
		}
		manifest.Components["minioDR"] = stats
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err = writeJSON(manifestPath, manifest); err != nil {
		return manifest, err
	}
	manifestArtifact, err := s.uploadAndVerify(ctx, id, manifestPath)
	if err != nil {
		return manifest, err
	}
	manifest.Artifacts = append(manifest.Artifacts, manifestArtifact)
	if s.dr != nil {
		if _, syncErr := s.replicateMinIO(ctx); syncErr != nil {
			return manifest, fmt.Errorf("minio DR manifest replication: %w", syncErr)
		}
	}
	details, _ := json.Marshal(manifest)
	_, err = s.pool.Exec(ctx, `UPDATE backup_task SET status='COMPLETED',object_key=$2,checksum=$3,details=$4,completed_at=now() WHERE id=$1`, id, manifestArtifact.ObjectKey, manifestArtifact.SHA256, details)
	if err != nil {
		return manifest, err
	}
	s.success.Add(1)
	s.lastOK.Store(time.Now().Unix())
	return manifest, nil
}

func (s *Service) rawBackupLocation() *time.Location {
	location, err := time.LoadLocation(strings.TrimSpace(s.cfg.BackupTimezone))
	if err != nil {
		return time.UTC
	}
	return location
}

func (s *Service) exportRawLogs(ctx context.Context, path string, start, end time.Time) (stats rawLogStats, err error) {
	stats = rawLogStats{Date: start.Format("2006-01-02"), Start: start, End: end}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return stats, err
	}
	gz := gzip.NewWriter(f)
	encoder := json.NewEncoder(gz)
	defer func() {
		if closeErr := gz.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	rows, err := s.pool.Query(ctx, `SELECT body FROM raw_message_log WHERE received_at >= $1 AND received_at < $2 ORDER BY received_at,message_id`, start.UnixMilli(), end.UnixMilli())
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			rows.Close()
			return stats, err
		}
		if err = encoder.Encode(rawLogRecord{Storage: "postgres", Message: json.RawMessage(body)}); err != nil {
			rows.Close()
			return stats, err
		}
		stats.PostgreSQL++
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return stats, err
	}
	rows.Close()

	if s.cfg.ClickHouseURL != "" {
		stats.ClickHouse, err = s.exportClickHouseRawLogs(ctx, start, end, encoder)
		if err != nil {
			return stats, err
		}
	}
	stats.Total = stats.PostgreSQL + stats.ClickHouse
	return stats, nil
}

func (s *Service) exportClickHouseRawLogs(ctx context.Context, start, end time.Time, encoder *json.Encoder) (int64, error) {
	u, err := url.Parse(s.cfg.ClickHouseURL)
	if err != nil {
		return 0, err
	}
	user := u.User
	u.User = nil
	query := u.Query()
	query.Set("query", fmt.Sprintf(`SELECT body FROM iot_raw_message WHERE received_at >= %d AND received_at < %d ORDER BY received_at,message_id FORMAT JSONEachRow`, start.UnixMilli(), end.UnixMilli()))
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return 0, err
	}
	if user != nil {
		password, _ := user.Password()
		req.SetBasicAuth(user.Username(), password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return 0, fmt.Errorf("clickhouse HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	decoder := json.NewDecoder(resp.Body)
	var count int64
	for {
		var row struct {
			Body string `json:"body"`
		}
		if err = decoder.Decode(&row); err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			return count, err
		}
		if !json.Valid([]byte(row.Body)) {
			return count, fmt.Errorf("clickhouse raw message body is not valid JSON")
		}
		if err = encoder.Encode(rawLogRecord{Storage: "clickhouse", Message: json.RawMessage(row.Body)}); err != nil {
			return count, err
		}
		count++
	}
}

func (s *Service) Verify(ctx context.Context, backupID string) (map[string]any, error) {
	if backupID == "" || backupID == "latest" {
		if err := s.pool.QueryRow(ctx, `SELECT id FROM backup_task WHERE status='COMPLETED' AND backup_type IN ('FULL','INCREMENTAL','RAW_LOGS') ORDER BY completed_at DESC LIMIT 1`).Scan(&backupID); err != nil {
			return nil, err
		}
	}
	drillID := "drill_" + time.Now().UTC().Format("20060102T150405.000Z")
	_, _ = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,'RESTORE_DRILL','RUNNING',now())`, drillID)
	key := "backup/" + backupID + "/manifest.json"
	object, err := s.store.GetObject(ctx, s.cfg.BackupBucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	var manifest Manifest
	if err = json.NewDecoder(object).Decode(&manifest); err != nil {
		return nil, err
	}
	checked := 0
	for _, artifact := range manifest.Artifacts {
		if artifact.Filename == "manifest.json" {
			continue
		}
		obj, getErr := s.store.GetObject(ctx, s.cfg.BackupBucket, artifact.ObjectKey, minio.GetObjectOptions{})
		if getErr != nil {
			err = getErr
			break
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, obj)
		obj.Close()
		if copyErr != nil || hex.EncodeToString(h.Sum(nil)) != artifact.SHA256 {
			err = fmt.Errorf("checksum mismatch: %s", artifact.Filename)
			break
		}
		checked++
	}
	status := "COMPLETED"
	if err != nil {
		status = "FAILED"
	}
	details, _ := json.Marshal(map[string]any{"backupId": backupID, "artifactsChecked": checked, "error": errorString(err)})
	_, _ = s.pool.Exec(ctx, `UPDATE backup_task SET status=$2,details=$3,completed_at=now() WHERE id=$1`, drillID, status, details)
	return map[string]any{"drillId": drillID, "backupId": backupID, "status": status, "artifactsChecked": checked}, err
}

func (s *Service) Metrics() string {
	lastError, _ := s.lastError.Load().(string)
	return fmt.Sprintf("# TYPE backup_success_total counter\nbackup_success_total %d\n# TYPE backup_failed_total counter\nbackup_failed_total %d\n# TYPE backup_last_success_timestamp_seconds gauge\nbackup_last_success_timestamp_seconds %d\n# backup_last_error %q\n", s.success.Load(), s.failed.Load(), s.lastOK.Load(), lastError)
}

func (s *Service) postgresDump(ctx context.Context, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := pgDumpCommand(ctx, s.cfg.PostgresDSN)
	cmd.Stdout, cmd.Stderr = f, os.Stderr
	runErr := cmd.Run()
	closeErr := f.Close()
	if runErr != nil {
		return runErr
	}
	return closeErr
}

func (s *Service) redisSnapshot(ctx context.Context, path string) error {
	host, port := splitHostPort(s.cfg.RedisAddr, "6379")
	args := []string{"-h", host, "-p", port}
	if s.cfg.RedisPassword != "" {
		args = append(args, "-a", s.cfg.RedisPassword, "--no-auth-warning")
	}
	args = append(args, "--rdb", path)
	cmd := redisCLICommand(ctx, host, port, args)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func pgDumpCommand(ctx context.Context, dsn string) *exec.Cmd {
	args := []string{"--format=custom", "--no-owner", "--dbname", dsn}
	if backupToolMode() != "docker" {
		return exec.CommandContext(ctx, env("PG_DUMP_BIN", "pg_dump"), args...)
	}
	if runtime.GOOS != "linux" {
		args[3] = dockerizeLocalURL(dsn)
	}
	dockerArgs := append(dockerToolArgs(), env("IOT_BACKUP_PG_IMAGE", "postgres:17-alpine3.22"), "pg_dump")
	return exec.CommandContext(ctx, "docker", append(dockerArgs, args...)...)
}

func redisCLICommand(ctx context.Context, host, port string, args []string) *exec.Cmd {
	if backupToolMode() != "docker" {
		return exec.CommandContext(ctx, env("REDIS_CLI_BIN", "redis-cli"), args...)
	}
	containerArgs := append([]string(nil), args...)
	if runtime.GOOS != "linux" {
		containerArgs[1] = dockerizeHost(host)
	}
	dockerArgs := dockerToolArgs()
	if rdbIndex := indexOf(containerArgs, "--rdb"); rdbIndex >= 0 && rdbIndex+1 < len(containerArgs) {
		hostPath, err := filepath.Abs(containerArgs[rdbIndex+1])
		if err == nil {
			dockerArgs = append(dockerArgs, "--volume", filepath.Dir(hostPath)+":/backup")
			containerArgs[rdbIndex+1] = "/backup/" + filepath.Base(hostPath)
		}
	}
	dockerArgs = append(dockerArgs, env("IOT_BACKUP_REDIS_IMAGE", "redis:7.4-alpine"), "redis-cli")
	return exec.CommandContext(ctx, "docker", append(dockerArgs, containerArgs...)...)
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func dockerToolArgs() []string {
	if runtime.GOOS == "linux" {
		return []string{"run", "--rm", "--network", "host"}
	}
	return []string{"run", "--rm", "--add-host", "host.docker.internal:host-gateway"}
}

func backupToolMode() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("IOT_BACKUP_TOOL_MODE")))
}

func backupToolAvailability() error {
	if mode := backupToolMode(); mode != "" && mode != "native" && mode != "docker" {
		return fmt.Errorf("IOT_BACKUP_TOOL_MODE must be native or docker")
	}
	if backupToolMode() == "docker" {
		if _, err := exec.LookPath("docker"); err != nil {
			return fmt.Errorf("docker is required when IOT_BACKUP_TOOL_MODE=docker")
		}
		return nil
	}
	for _, item := range []struct{ name, envName, fallback string }{
		{name: "PostgreSQL dump tool", envName: "PG_DUMP_BIN", fallback: "pg_dump"},
		{name: "Redis snapshot tool", envName: "REDIS_CLI_BIN", fallback: "redis-cli"},
	} {
		if _, err := exec.LookPath(env(item.envName, item.fallback)); err != nil {
			return fmt.Errorf("%s is unavailable: %w", item.name, err)
		}
	}
	return nil
}

func dockerizeLocalURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return raw
	}
	port := parsed.Port()
	parsed.Host = dockerizeHost(parsed.Hostname())
	if port != "" {
		parsed.Host = net.JoinHostPort(parsed.Host, port)
	} else if strings.Contains(parsed.Host, ":") {
		parsed.Host = "[" + parsed.Host + "]"
	}
	return parsed.String()
}

func dockerizeHost(host string) string {
	host = strings.TrimSpace(host)
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return "host.docker.internal"
	default:
		return host
	}
}

func (s *Service) clickHouseExport(ctx context.Context, dir string) (int, error) {
	tables, err := s.clickQuery(ctx, "SELECT name FROM system.tables WHERE database = currentDatabase() AND is_temporary = 0 AND engine NOT IN ('View','MaterializedView') FORMAT TabSeparated")
	if err != nil {
		return 0, err
	}
	names := strings.Fields(string(tables))
	schema := map[string]string{}
	for _, table := range names {
		ddl, queryErr := s.clickQuery(ctx, "SHOW CREATE TABLE `"+strings.ReplaceAll(table, "`", "``")+"`")
		if queryErr != nil {
			return 0, queryErr
		}
		schema[table] = string(ddl)
		data, queryErr := s.clickQuery(ctx, "SELECT * FROM `"+strings.ReplaceAll(table, "`", "``")+"` FORMAT Native")
		if queryErr != nil {
			return 0, queryErr
		}
		if err = os.WriteFile(filepath.Join(dir, "clickhouse-"+safeName(table)+".native"), data, 0o600); err != nil {
			return 0, err
		}
	}
	return len(names), writeJSON(filepath.Join(dir, "clickhouse-schema.json"), schema)
}

func (s *Service) clickQuery(ctx context.Context, query string) ([]byte, error) {
	u, err := url.Parse(s.cfg.ClickHouseURL)
	if err != nil {
		return nil, err
	}
	user := u.User
	u.User = nil
	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if user != nil {
		password, _ := user.Password()
		req.SetBasicAuth(user.Username(), password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("clickhouse HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (s *Service) minioInventory(ctx context.Context) (map[string]any, error) {
	buckets, err := s.store.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for _, bucket := range buckets {
		_ = s.store.SetBucketVersioning(ctx, bucket.Name, minio.BucketVersioningConfiguration{Status: "Enabled"})
		var count, size int64
		for object := range s.store.ListObjects(ctx, bucket.Name, minio.ListObjectsOptions{Recursive: true}) {
			if object.Err != nil {
				return nil, object.Err
			}
			count++
			size += object.Size
		}
		result[bucket.Name] = map[string]int64{"objects": count, "bytes": size}
	}
	return result, nil
}

func (s *Service) replicateMinIO(ctx context.Context) (map[string]int64, error) {
	buckets, err := s.store.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	var copied, skipped int64
	for _, bucket := range buckets {
		if err = s.ensureBucket(ctx, s.dr, bucket.Name); err != nil {
			return nil, err
		}
		_ = s.dr.SetBucketVersioning(ctx, bucket.Name, minio.BucketVersioningConfiguration{Status: "Enabled"})
		for item := range s.store.ListObjects(ctx, bucket.Name, minio.ListObjectsOptions{Recursive: true}) {
			if item.Err != nil {
				return nil, item.Err
			}
			if dst, statErr := s.dr.StatObject(ctx, bucket.Name, item.Key, minio.StatObjectOptions{}); statErr == nil && dst.Size == item.Size && dst.ETag == item.ETag {
				skipped++
				continue
			}
			obj, getErr := s.store.GetObject(ctx, bucket.Name, item.Key, minio.GetObjectOptions{})
			if getErr != nil {
				return nil, getErr
			}
			_, putErr := s.dr.PutObject(ctx, bucket.Name, item.Key, obj, item.Size, minio.PutObjectOptions{ContentType: item.ContentType})
			obj.Close()
			if putErr != nil {
				return nil, putErr
			}
			copied++
		}
	}
	return map[string]int64{"copied": copied, "unchanged": skipped}, nil
}

func newBackupID(kind string, now time.Time) string {
	// Weaviate permits only lowercase letters, digits, underscores and hyphens.
	return fmt.Sprintf("backup_%s_%s_%09d", strings.ToLower(kind), now.UTC().Format("20060102t150405z"), now.Nanosecond())
}

func (s *Service) weaviateBackup(ctx context.Context, id string) error {
	endpoint := strings.TrimRight(s.cfg.WeaviateURL, "/") + "/v1/backups/filesystem"
	body := strings.NewReader(fmt.Sprintf(`{"id":%q}`, id))
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	return nil
}

func (s *Service) uploadAndVerify(ctx context.Context, id, path string) (Artifact, error) {
	checksum, size, err := hashFile(path)
	if err != nil {
		return Artifact{}, err
	}
	filename := filepath.Base(path)
	key := "backup/" + id + "/" + filename
	_, err = s.store.FPutObject(ctx, s.cfg.BackupBucket, key, path, minio.PutObjectOptions{ContentType: "application/octet-stream", UserMetadata: map[string]string{"sha256": checksum}})
	if err != nil {
		return Artifact{}, err
	}
	obj, err := s.store.GetObject(ctx, s.cfg.BackupBucket, key, minio.GetObjectOptions{})
	if err != nil {
		return Artifact{}, err
	}
	h := sha256.New()
	n, err := io.Copy(h, obj)
	obj.Close()
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != checksum {
		return Artifact{}, fmt.Errorf("uploaded object verification failed: %s", filename)
	}
	return Artifact{Component: componentName(filename), Filename: filename, ObjectKey: key, SHA256: checksum, Size: size}, nil
}

func (s *Service) ensureBucket(ctx context.Context, client *minio.Client, bucket string) error {
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		return client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
	}
	return nil
}

func tarPaths(target string, paths []string) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, root := range paths {
		root = strings.TrimSpace(root)
		if root == "" || !exists(root) {
			continue
		}
		base := filepath.Base(root)
		err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			header, headerErr := tar.FileInfoHeader(info, "")
			if headerErr != nil {
				return headerErr
			}
			rel, _ := filepath.Rel(root, path)
			header.Name = filepath.ToSlash(filepath.Join(base, rel))
			if headerErr = tw.WriteHeader(header); headerErr != nil || info.IsDir() {
				return headerErr
			}
			in, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			_, copyErr := io.Copy(tw, in)
			in.Close()
			return copyErr
		})
		if err != nil {
			break
		}
	}
	closeErr := tw.Close()
	if err == nil {
		err = closeErr
	}
	closeErr = gz.Close()
	if err == nil {
		err = closeErr
	}
	closeErr = f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func downloadJSON(ctx context.Context, endpoint, path string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var parsed any
	if err = json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	return writeJSON(path, parsed)
}
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}
func exists(path string) bool { _, err := os.Stat(path); return err == nil }
func splitPaths(value string) []string {
	var out []string
	for _, v := range strings.Split(value, ",") {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}
func splitHostPort(value, defaultPort string) (string, string) {
	parts := strings.Split(value, ":")
	if len(parts) == 1 {
		return parts[0], defaultPort
	}
	return strings.Join(parts[:len(parts)-1], ":"), parts[len(parts)-1]
}
func safeName(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
}
func componentName(filename string) string {
	if i := strings.Index(filename, "-"); i > 0 {
		return filename[:i]
	}
	if i := strings.Index(filename, "."); i > 0 {
		return filename[:i]
	}
	return filename
}
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
