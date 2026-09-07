package backup

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	ClickHouseURL, BackupTimezone                                                       string
	MinIOUseTLS                                                                         bool
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
	if cfg.BackupDir == "" {
		cfg.BackupDir = "./data/backups"
	}
	if cfg.BackupBucket == "" {
		cfg.BackupBucket = "iot-backups"
	}
	if cfg.BackupTimezone == "" {
		cfg.BackupTimezone = "Asia/Shanghai"
	}
	return &Service{cfg: cfg, pool: pool, store: store}, nil
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
	return nil
}

// Run retains the old request types for existing clients, but all new
// backups contain only device data. Daily requests cover the previous day.
func (s *Service) Run(ctx context.Context, kind string) (Manifest, error) {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if kind == "" {
		kind = "FULL"
	}
	if kind == "DEVICE_DAILY" || kind == "RAW_LOGS" || kind == "INCREMENTAL" {
		return s.RunDaily(ctx, time.Now().In(s.rawBackupLocation()).AddDate(0, 0, -1))
	}
	if kind != "FULL" {
		return Manifest{}, fmt.Errorf("unsupported backup type: %s", kind)
	}
	return s.runDeviceData(ctx, "FULL", time.Time{}, time.Now())
}

func (s *Service) RunDaily(ctx context.Context, day time.Time) (Manifest, error) {
	day = day.In(s.rawBackupLocation())
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	return s.runDeviceData(ctx, "DEVICE_DAILY", start, start.AddDate(0, 0, 1))
}

func (s *Service) runDeviceData(ctx context.Context, kind string, start, end time.Time) (manifest Manifest, err error) {
	if !s.mu.TryLock() {
		return manifest, fmt.Errorf("a backup is already running")
	}
	defer s.mu.Unlock()
	id := newBackupID(kind, time.Now())
	manifest = Manifest{ID: id, Type: kind, CreatedAt: time.Now().UTC(), Components: map[string]any{
		"scope": "device raw messages and parsed data only", "timezone": s.rawBackupLocation().String(),
	}}
	if !start.IsZero() {
		manifest.Components["start"] = start
		manifest.Components["end"] = end
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,$2,'RUNNING',now())`, id, kind); err != nil {
		return manifest, err
	}
	defer func() {
		if err != nil {
			s.failed.Add(1)
			s.lastError.Store(err.Error())
			details, _ := json.Marshal(map[string]any{"error": err.Error(), "components": manifest.Components})
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = s.pool.Exec(updateCtx, `UPDATE backup_task SET status='FAILED',details=$2,completed_at=now() WHERE id=$1`, id, details)
		}
	}()
	dir := filepath.Join(s.cfg.BackupDir, id)
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return manifest, err
	}
	rawPath := filepath.Join(dir, "raw-messages.jsonl.gz")
	raw, exportErr := s.exportMessages(ctx, rawPath, start, end, false)
	if exportErr != nil {
		return manifest, fmt.Errorf("raw messages: %w", exportErr)
	}
	parsedPath := filepath.Join(dir, "parsed-messages.jsonl.gz")
	parsed, exportErr := s.exportMessages(ctx, parsedPath, start, end, true)
	if exportErr != nil {
		return manifest, fmt.Errorf("parsed messages: %w", exportErr)
	}
	manifest.Components["rawMessages"] = map[string]any{"records": raw.Total, "postgresql": raw.PostgreSQL, "clickhouse": raw.ClickHouse}
	manifest.Components["parsedMessages"] = map[string]any{"records": parsed.Total, "postgresql": parsed.PostgreSQL, "clickhouse": parsed.ClickHouse}
	if err = s.ensureBucket(ctx, s.store, s.cfg.BackupBucket); err != nil {
		return manifest, err
	}
	for _, path := range []string{rawPath, parsedPath} {
		artifact, uploadErr := s.uploadAndVerify(ctx, id, path)
		if uploadErr != nil {
			return manifest, uploadErr
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err = writeJSON(manifestPath, manifest); err != nil {
		return manifest, err
	}
	artifact, uploadErr := s.uploadAndVerify(ctx, id, manifestPath)
	if uploadErr != nil {
		return manifest, uploadErr
	}
	manifest.Artifacts = append(manifest.Artifacts, artifact)
	details, _ := json.Marshal(manifest)
	_, err = s.pool.Exec(ctx, `UPDATE backup_task SET status='COMPLETED',object_key=$2,checksum=$3,details=$4,completed_at=now() WHERE id=$1`, id, artifact.ObjectKey, artifact.SHA256, details)
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

func (s *Service) exportMessages(ctx context.Context, path string, start, end time.Time, parsed bool) (stats rawLogStats, err error) {
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

	pgSQL, args, chSQL := messageQueries(start, end, parsed)
	rows, err := s.pool.Query(ctx, pgSQL, args...)
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
		stats.ClickHouse, err = s.exportClickHouseRows(ctx, chSQL, !parsed, encoder)
		if err != nil {
			return stats, err
		}
	}
	stats.Total = stats.PostgreSQL + stats.ClickHouse
	return stats, nil
}

// Query only the four device-data tables. Full exports have no timestamp
// predicate, so historical and device-clock-skewed messages are not omitted.
func messageQueries(start, end time.Time, parsed bool) (string, []any, string) {
	pgTable, pgTime, chTable, chTime, chColumns := "raw_message_log", "received_at", "iot_raw_message", "received_at", "body"
	if parsed {
		pgTable, pgTime = "standard_message", "(CASE WHEN processed_at > 0 THEN processed_at ELSE ts END)"
		chTable, chTime, chColumns = "iot_telemetry", "ts", "*"
	}
	pgSQL, chSQL := "SELECT body FROM "+pgTable, "SELECT "+chColumns+" FROM "+chTable
	var args []any
	if !start.IsZero() {
		pgSQL += " WHERE " + pgTime + " >= $1 AND " + pgTime + " < $2"
		args = []any{start.UnixMilli(), end.UnixMilli()}
		if parsed {
			chSQL += fmt.Sprintf(" WHERE ts >= fromUnixTimestamp64Milli(%d) AND ts < fromUnixTimestamp64Milli(%d)", start.UnixMilli(), end.UnixMilli())
		} else {
			chSQL += fmt.Sprintf(" WHERE received_at >= %d AND received_at < %d", start.UnixMilli(), end.UnixMilli())
		}
	}
	return pgSQL, args, chSQL + " ORDER BY " + chTime + ",message_id FORMAT JSONEachRow"
}

func (s *Service) exportClickHouseRows(ctx context.Context, sql string, unwrap bool, encoder *json.Encoder) (int64, error) {
	u, err := url.Parse(s.cfg.ClickHouseURL)
	if err != nil {
		return 0, err
	}
	user := u.User
	u.User = nil
	query := u.Query()
	query.Set("query", sql)
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
		var row json.RawMessage
		if err = decoder.Decode(&row); err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			return count, err
		}
		body := row
		if unwrap {
			var wrapped struct {
				Body string `json:"body"`
			}
			if err = json.Unmarshal(row, &wrapped); err != nil {
				return count, err
			}
			body = json.RawMessage(wrapped.Body)
		}
		if !json.Valid(body) {
			return count, fmt.Errorf("invalid message JSON")
		}
		if err = encoder.Encode(rawLogRecord{Storage: "clickhouse", Message: body}); err != nil {
			return count, err
		}
		count++
	}
}

func (s *Service) Verify(ctx context.Context, backupID string) (map[string]any, error) {
	if backupID == "" || backupID == "latest" {
		if err := s.pool.QueryRow(ctx, `SELECT id FROM backup_task WHERE status='COMPLETED' AND backup_type IN ('FULL','INCREMENTAL','RAW_LOGS','DEVICE_DAILY') ORDER BY completed_at DESC LIMIT 1`).Scan(&backupID); err != nil {
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

func newBackupID(kind string, now time.Time) string {
	return fmt.Sprintf("backup_%s_%s_%09d", strings.ToLower(kind), now.UTC().Format("20060102t150405z"), now.Nanosecond())
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
