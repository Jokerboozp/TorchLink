package backup /* 声明 backup 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"compress/gzip" /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"sync/atomic"   /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"github.com/jackc/pgx/v5/pgxpool"              /* 执行当前语句并推进处理流程。 */
	"github.com/minio/minio-go/v7"                 /* 执行当前语句并推进处理流程。 */
	"github.com/minio/minio-go/v7/pkg/credentials" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Config struct { /* 定义 Config 类型。 */
	PostgresDSN, BackupDir, BackupBucket, MinIOEndpoint, MinIOAccessKey, MinIOSecretKey string /* 执行当前语句并推进处理流程。 */
	ClickHouseURL, BackupTimezone                                                       string /* 执行当前语句并推进处理流程。 */
	MinIOUseTLS                                                                         bool   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Artifact struct { /* 定义 Artifact 类型。 */
	Component string `json:"component"` /* 执行当前语句并推进处理流程。 */
	Filename  string `json:"filename"`  /* 执行当前语句并推进处理流程。 */
	ObjectKey string `json:"objectKey"` /* 执行当前语句并推进处理流程。 */
	SHA256    string `json:"sha256"`    /* 执行当前语句并推进处理流程。 */
	Size      int64  `json:"size"`      /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Manifest struct { /* 定义 Manifest 类型。 */
	ID         string         `json:"id"`         /* 执行当前语句并推进处理流程。 */
	Type       string         `json:"type"`       /* 执行当前语句并推进处理流程。 */
	CreatedAt  time.Time      `json:"createdAt"`  /* 执行当前语句并推进处理流程。 */
	Artifacts  []Artifact     `json:"artifacts"`  /* 执行当前语句并推进处理流程。 */
	Components map[string]any `json:"components"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type rawLogRecord struct { /* 定义 rawLogRecord 类型。 */
	Storage string          `json:"storage"` /* 执行当前语句并推进处理流程。 */
	Message json.RawMessage `json:"message"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type rawLogStats struct { /* 定义 rawLogStats 类型。 */
	Date       string    /* 执行当前语句并推进处理流程。 */
	Start      time.Time /* 执行当前语句并推进处理流程。 */
	End        time.Time /* 执行当前语句并推进处理流程。 */
	PostgreSQL int64     /* 执行当前语句并推进处理流程。 */
	ClickHouse int64     /* 执行当前语句并推进处理流程。 */
	Total      int64     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Service struct { /* 定义 Service 类型。 */
	cfg       Config        /* 执行当前语句并推进处理流程。 */
	pool      *pgxpool.Pool /* 执行当前语句并推进处理流程。 */
	store     *minio.Client /* 执行当前语句并推进处理流程。 */
	mu        sync.Mutex    /* 执行当前语句并推进处理流程。 */
	success   atomic.Uint64 /* 执行当前语句并推进处理流程。 */
	failed    atomic.Uint64 /* 执行当前语句并推进处理流程。 */
	lastOK    atomic.Int64  /* 执行当前语句并推进处理流程。 */
	lastError atomic.Value  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(ctx context.Context, cfg Config) (*Service, error) { /* 定义 New 函数。 */
	if cfg.PostgresDSN == "" || cfg.MinIOEndpoint == "" { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("postgres and minio configuration are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = pool.Ping(ctx); err != nil { /* 判断条件并选择处理分支。 */
		pool.Close()                                /* 执行当前语句并推进处理流程。 */
		return nil, fmt.Errorf("postgres: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	store, err := minio.New(cfg.MinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.MinIOAccessKey, cfg.MinIOSecretKey, ""), Secure: cfg.MinIOUseTLS}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                 /* 判断条件并选择处理分支。 */
		pool.Close()    /* 执行当前语句并推进处理流程。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.BackupDir == "" { /* 判断条件并选择处理分支。 */
		cfg.BackupDir = "./data/backups" /* 更新 cfg.BackupDir 的值。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.BackupBucket == "" { /* 判断条件并选择处理分支。 */
		cfg.BackupBucket = "iot-backups" /* 更新 cfg.BackupBucket 的值。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.BackupTimezone == "" { /* 判断条件并选择处理分支。 */
		cfg.BackupTimezone = "Asia/Shanghai" /* 更新 cfg.BackupTimezone 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &Service{cfg: cfg, pool: pool, store: store}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) Close() { s.pool.Close() } /* 定义 Close 函数。 */

// Ready checks the catalog created by the platform API and the backup stores.
func (s *Service) Ready(ctx context.Context) error { /* 定义 Ready 函数。 */
	if _, err := s.pool.Exec(ctx, "SELECT 1 FROM backup_task LIMIT 0"); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("postgres: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := s.store.ListBuckets(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("minio: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Run retains the old request types for existing clients, but all new
// backups contain only device data. Daily requests cover the previous day.
func (s *Service) Run(ctx context.Context, kind string) (Manifest, error) { /* 定义 Run 函数。 */
	kind = strings.ToUpper(strings.TrimSpace(kind)) /* 更新 kind 的值。 */
	if kind == "" {                                 /* 判断条件并选择处理分支。 */
		kind = "FULL" /* 更新 kind 的值。 */
	} /* 结束当前表达式或代码块。 */
	if kind == "DEVICE_DAILY" || kind == "RAW_LOGS" || kind == "INCREMENTAL" { /* 判断条件并选择处理分支。 */
		return s.RunDaily(ctx, time.Now().In(s.rawBackupLocation()).AddDate(0, 0, -1)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if kind != "FULL" { /* 判断条件并选择处理分支。 */
		return Manifest{}, fmt.Errorf("unsupported backup type: %s", kind) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return s.runDeviceData(ctx, "FULL", time.Time{}, time.Now()) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) RunDaily(ctx context.Context, day time.Time) (Manifest, error) { /* 定义 RunDaily 函数。 */
	day = day.In(s.rawBackupLocation())                                                /* 更新 day 的值。 */
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()) /* 更新 start 的值。 */
	return s.runDeviceData(ctx, "DEVICE_DAILY", start, start.AddDate(0, 0, 1))         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) runDeviceData(ctx context.Context, kind string, start, end time.Time) (manifest Manifest, err error) { /* 定义 runDeviceData 函数。 */
	if !s.mu.TryLock() { /* 判断条件并选择处理分支。 */
		return manifest, fmt.Errorf("a backup is already running") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer s.mu.Unlock()                                                                              /* 安排函数结束时执行清理。 */
	id := newBackupID(kind, time.Now())                                                              /* 更新 id 的值。 */
	manifest = Manifest{ID: id, Type: kind, CreatedAt: time.Now().UTC(), Components: map[string]any{ /* 更新 manifest 的值。 */
		"scope": "device raw messages and parsed data only", "timezone": s.rawBackupLocation().String(), /* 执行当前语句并推进处理流程。 */
	}} /* 结束当前表达式或代码块。 */
	if !start.IsZero() { /* 判断条件并选择处理分支。 */
		manifest.Components["start"] = start /* 执行当前语句并推进处理流程。 */
		manifest.Components["end"] = end     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,$2,'RUNNING',now())`, id, kind); err != nil { /* 判断条件并选择处理分支。 */
		return manifest, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { /* 安排函数结束时执行清理。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			s.failed.Add(1)                                                                                                                /* 执行当前语句并推进处理流程。 */
			s.lastError.Store(err.Error())                                                                                                 /* 执行当前语句并推进处理流程。 */
			details, _ := json.Marshal(map[string]any{"error": err.Error(), "components": manifest.Components})                            /* 更新 _ 的值。 */
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)                                                  /* 更新 cancel 的值。 */
			defer cancel()                                                                                                                 /* 安排函数结束时执行清理。 */
			_, _ = s.pool.Exec(updateCtx, `UPDATE backup_task SET status='FAILED',details=$2,completed_at=now() WHERE id=$1`, id, details) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	dir := filepath.Join(s.cfg.BackupDir, id)      /* 更新 dir 的值。 */
	if err = os.MkdirAll(dir, 0o750); err != nil { /* 判断条件并选择处理分支。 */
		return manifest, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rawPath := filepath.Join(dir, "raw-messages.jsonl.gz")              /* 更新 rawPath 的值。 */
	raw, exportErr := s.exportMessages(ctx, rawPath, start, end, false) /* 更新 exportErr 的值。 */
	if exportErr != nil {                                               /* 判断条件并选择处理分支。 */
		return manifest, fmt.Errorf("raw messages: %w", exportErr) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parsedPath := filepath.Join(dir, "parsed-messages.jsonl.gz")             /* 更新 parsedPath 的值。 */
	parsed, exportErr := s.exportMessages(ctx, parsedPath, start, end, true) /* 更新 exportErr 的值。 */
	if exportErr != nil {                                                    /* 判断条件并选择处理分支。 */
		return manifest, fmt.Errorf("parsed messages: %w", exportErr) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	manifest.Components["rawMessages"] = map[string]any{"records": raw.Total, "postgresql": raw.PostgreSQL, "clickhouse": raw.ClickHouse}             /* 执行当前语句并推进处理流程。 */
	manifest.Components["parsedMessages"] = map[string]any{"records": parsed.Total, "postgresql": parsed.PostgreSQL, "clickhouse": parsed.ClickHouse} /* 执行当前语句并推进处理流程。 */
	if err = s.ensureBucket(ctx, s.store, s.cfg.BackupBucket); err != nil {                                                                           /* 判断条件并选择处理分支。 */
		return manifest, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, path := range []string{rawPath, parsedPath} { /* 循环处理当前数据。 */
		artifact, uploadErr := s.uploadAndVerify(ctx, id, path) /* 更新 uploadErr 的值。 */
		if uploadErr != nil {                                   /* 判断条件并选择处理分支。 */
			return manifest, uploadErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		manifest.Artifacts = append(manifest.Artifacts, artifact) /* 更新 manifest.Artifacts 的值。 */
	} /* 结束当前表达式或代码块。 */
	manifestPath := filepath.Join(dir, "manifest.json")      /* 更新 manifestPath 的值。 */
	if err = writeJSON(manifestPath, manifest); err != nil { /* 判断条件并选择处理分支。 */
		return manifest, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	artifact, uploadErr := s.uploadAndVerify(ctx, id, manifestPath) /* 更新 uploadErr 的值。 */
	if uploadErr != nil {                                           /* 判断条件并选择处理分支。 */
		return manifest, uploadErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	manifest.Artifacts = append(manifest.Artifacts, artifact)                                                                                                                                    /* 更新 manifest.Artifacts 的值。 */
	details, _ := json.Marshal(manifest)                                                                                                                                                         /* 更新 _ 的值。 */
	_, err = s.pool.Exec(ctx, `UPDATE backup_task SET status='COMPLETED',object_key=$2,checksum=$3,details=$4,completed_at=now() WHERE id=$1`, id, artifact.ObjectKey, artifact.SHA256, details) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                              /* 判断条件并选择处理分支。 */
		return manifest, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.success.Add(1)                  /* 执行当前语句并推进处理流程。 */
	s.lastOK.Store(time.Now().Unix()) /* 执行当前语句并推进处理流程。 */
	return manifest, nil              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) rawBackupLocation() *time.Location { /* 定义 rawBackupLocation 函数。 */
	location, err := time.LoadLocation(strings.TrimSpace(s.cfg.BackupTimezone)) /* 更新 err 的值。 */
	if err != nil {                                                             /* 判断条件并选择处理分支。 */
		return time.UTC /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return location /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) exportMessages(ctx context.Context, path string, start, end time.Time, parsed bool) (stats rawLogStats, err error) { /* 定义 exportMessages 函数。 */
	stats = rawLogStats{Date: start.Format("2006-01-02"), Start: start, End: end} /* 更新 stats 的值。 */
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)         /* 更新 err 的值。 */
	if err != nil {                                                               /* 判断条件并选择处理分支。 */
		return stats, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	gz := gzip.NewWriter(f)        /* 更新 gz 的值。 */
	encoder := json.NewEncoder(gz) /* 更新 encoder 的值。 */
	defer func() {                 /* 安排函数结束时执行清理。 */
		if closeErr := gz.Close(); err == nil && closeErr != nil { /* 判断条件并选择处理分支。 */
			err = closeErr /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		if closeErr := f.Close(); err == nil && closeErr != nil { /* 判断条件并选择处理分支。 */
			err = closeErr /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */

	pgSQL, args, chSQL := messageQueries(start, end, parsed) /* 更新 chSQL 的值。 */
	rows, err := s.pool.Query(ctx, pgSQL, args...)           /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		return stats, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for rows.Next() { /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			rows.Close()      /* 执行当前语句并推进处理流程。 */
			return stats, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = encoder.Encode(rawLogRecord{Storage: "postgres", Message: json.RawMessage(body)}); err != nil { /* 判断条件并选择处理分支。 */
			rows.Close()      /* 执行当前语句并推进处理流程。 */
			return stats, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		stats.PostgreSQL++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err = rows.Err(); err != nil { /* 判断条件并选择处理分支。 */
		rows.Close()      /* 执行当前语句并推进处理流程。 */
		return stats, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows.Close() /* 执行当前语句并推进处理流程。 */

	if s.cfg.ClickHouseURL != "" { /* 判断条件并选择处理分支。 */
		stats.ClickHouse, err = s.exportClickHouseRows(ctx, chSQL, !parsed, encoder) /* 更新 err 的值。 */
		if err != nil {                                                              /* 判断条件并选择处理分支。 */
			return stats, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	stats.Total = stats.PostgreSQL + stats.ClickHouse /* 更新 stats.Total 的值。 */
	return stats, nil                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Query only the four device-data tables. Full exports have no timestamp
// predicate, so historical and device-clock-skewed messages are not omitted.
func messageQueries(start, end time.Time, parsed bool) (string, []any, string) { /* 定义 messageQueries 函数。 */
	pgTable, pgTime, chTable, chTime, chColumns := "raw_message_log", "received_at", "iot_raw_message", "received_at", "body" /* 更新 chColumns 的值。 */
	if parsed {                                                                                                               /* 判断条件并选择处理分支。 */
		pgTable, pgTime = "standard_message", "(CASE WHEN processed_at > 0 THEN processed_at ELSE ts END)" /* 更新 pgTime 的值。 */
		chTable, chTime, chColumns = "iot_telemetry", "ts", "*"                                            /* 更新 chColumns 的值。 */
	} /* 结束当前表达式或代码块。 */
	pgSQL, chSQL := "SELECT body FROM "+pgTable, "SELECT "+chColumns+" FROM "+chTable /* 更新 chSQL 的值。 */
	var args []any                                                                    /* 声明 args。 */
	if !start.IsZero() {                                                              /* 判断条件并选择处理分支。 */
		pgSQL += " WHERE " + pgTime + " >= $1 AND " + pgTime + " < $2" /* 更新 pgSQL 的值。 */
		args = []any{start.UnixMilli(), end.UnixMilli()}               /* 更新 args 的值。 */
		if parsed {                                                    /* 判断条件并选择处理分支。 */
			chSQL += fmt.Sprintf(" WHERE ts >= fromUnixTimestamp64Milli(%d) AND ts < fromUnixTimestamp64Milli(%d)", start.UnixMilli(), end.UnixMilli()) /* 更新 chSQL 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			chSQL += fmt.Sprintf(" WHERE received_at >= %d AND received_at < %d", start.UnixMilli(), end.UnixMilli()) /* 更新 chSQL 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return pgSQL, args, chSQL + " ORDER BY " + chTime + ",message_id FORMAT JSONEachRow" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) exportClickHouseRows(ctx context.Context, sql string, unwrap bool, encoder *json.Encoder) (int64, error) { /* 定义 exportClickHouseRows 函数。 */
	u, err := url.Parse(s.cfg.ClickHouseURL) /* 更新 err 的值。 */
	if err != nil {                          /* 判断条件并选择处理分支。 */
		return 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	user := u.User                                                                /* 更新 user 的值。 */
	u.User = nil                                                                  /* 更新 u.User 的值。 */
	query := u.Query()                                                            /* 更新 query 的值。 */
	query.Set("query", sql)                                                       /* 执行当前语句并推进处理流程。 */
	u.RawQuery = query.Encode()                                                   /* 更新 u.RawQuery 的值。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                               /* 判断条件并选择处理分支。 */
		return 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if user != nil { /* 判断条件并选择处理分支。 */
		password, _ := user.Password()              /* 更新 _ 的值。 */
		req.SetBasicAuth(user.Username(), password) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := http.DefaultClient.Do(req) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		return 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))                                          /* 更新 _ 的值。 */
		return 0, fmt.Errorf("clickhouse HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data))) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	decoder := json.NewDecoder(resp.Body) /* 更新 decoder 的值。 */
	var count int64                       /* 声明 count。 */
	for {                                 /* 循环处理当前数据。 */
		var row json.RawMessage                     /* 声明 row。 */
		if err = decoder.Decode(&row); err != nil { /* 判断条件并选择处理分支。 */
			if errors.Is(err, io.EOF) { /* 判断条件并选择处理分支。 */
				return count, nil /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return count, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		body := row /* 更新 body 的值。 */
		if unwrap { /* 判断条件并选择处理分支。 */
			var wrapped struct { /* 声明 wrapped。 */
				Body string `json:"body"` /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if err = json.Unmarshal(row, &wrapped); err != nil { /* 判断条件并选择处理分支。 */
				return count, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			body = json.RawMessage(wrapped.Body) /* 更新 body 的值。 */
		} /* 结束当前表达式或代码块。 */
		if !json.Valid(body) { /* 判断条件并选择处理分支。 */
			return count, fmt.Errorf("invalid message JSON") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = encoder.Encode(rawLogRecord{Storage: "clickhouse", Message: body}); err != nil { /* 判断条件并选择处理分支。 */
			return count, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		count++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) Verify(ctx context.Context, backupID string) (map[string]any, error) { /* 定义 Verify 函数。 */
	if backupID == "" || backupID == "latest" { /* 判断条件并选择处理分支。 */
		if err := s.pool.QueryRow(ctx, `SELECT id FROM backup_task WHERE status='COMPLETED' AND backup_type IN ('FULL','INCREMENTAL','RAW_LOGS','DEVICE_DAILY') ORDER BY completed_at DESC LIMIT 1`).Scan(&backupID); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	drillID := "drill_" + time.Now().UTC().Format("20060102T150405.000Z")                                                                    /* 更新 drillID 的值。 */
	_, _ = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,'RESTORE_DRILL','RUNNING',now())`, drillID) /* 更新 _ 的值。 */
	key := "backup/" + backupID + "/manifest.json"                                                                                           /* 更新 key 的值。 */
	object, err := s.store.GetObject(ctx, s.cfg.BackupBucket, key, minio.GetObjectOptions{})                                                 /* 更新 err 的值。 */
	if err != nil {                                                                                                                          /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer object.Close()                                             /* 安排函数结束时执行清理。 */
	var manifest Manifest                                            /* 声明 manifest。 */
	if err = json.NewDecoder(object).Decode(&manifest); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	checked := 0                                  /* 更新 checked 的值。 */
	for _, artifact := range manifest.Artifacts { /* 循环处理当前数据。 */
		if artifact.Filename == "manifest.json" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		obj, getErr := s.store.GetObject(ctx, s.cfg.BackupBucket, artifact.ObjectKey, minio.GetObjectOptions{}) /* 更新 getErr 的值。 */
		if getErr != nil {                                                                                      /* 判断条件并选择处理分支。 */
			err = getErr /* 更新 err 的值。 */
			break        /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		h := sha256.New()                                                        /* 更新 h 的值。 */
		_, copyErr := io.Copy(h, obj)                                            /* 更新 copyErr 的值。 */
		obj.Close()                                                              /* 执行当前语句并推进处理流程。 */
		if copyErr != nil || hex.EncodeToString(h.Sum(nil)) != artifact.SHA256 { /* 判断条件并选择处理分支。 */
			err = fmt.Errorf("checksum mismatch: %s", artifact.Filename) /* 验证实际结果符合预期。 */
			break                                                        /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		checked++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	status := "COMPLETED" /* 更新 status 的值。 */
	if err != nil {       /* 判断条件并选择处理分支。 */
		status = "FAILED" /* 更新 status 的值。 */
	} /* 结束当前表达式或代码块。 */
	details, _ := json.Marshal(map[string]any{"backupId": backupID, "artifactsChecked": checked, "error": errorString(err)})        /* 更新 _ 的值。 */
	_, _ = s.pool.Exec(ctx, `UPDATE backup_task SET status=$2,details=$3,completed_at=now() WHERE id=$1`, drillID, status, details) /* 更新 _ 的值。 */
	return map[string]any{"drillId": drillID, "backupId": backupID, "status": status, "artifactsChecked": checked}, err             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) Metrics() string { /* 定义 Metrics 函数。 */
	lastError, _ := s.lastError.Load().(string)                                                                                                                                                                                                                                                                                           /* 更新 _ 的值。 */
	return fmt.Sprintf("# TYPE backup_success_total counter\nbackup_success_total %d\n# TYPE backup_failed_total counter\nbackup_failed_total %d\n# TYPE backup_last_success_timestamp_seconds gauge\nbackup_last_success_timestamp_seconds %d\n# backup_last_error %q\n", s.success.Load(), s.failed.Load(), s.lastOK.Load(), lastError) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func newBackupID(kind string, now time.Time) string { /* 定义 newBackupID 函数。 */
	return fmt.Sprintf("backup_%s_%s_%09d", strings.ToLower(kind), now.UTC().Format("20060102t150405z"), now.Nanosecond()) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) uploadAndVerify(ctx context.Context, id, path string) (Artifact, error) { /* 定义 uploadAndVerify 函数。 */
	checksum, size, err := hashFile(path) /* 更新 err 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		return Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	filename := filepath.Base(path)                                                                                                                                                       /* 更新 filename 的值。 */
	key := "backup/" + id + "/" + filename                                                                                                                                                /* 更新 key 的值。 */
	_, err = s.store.FPutObject(ctx, s.cfg.BackupBucket, key, path, minio.PutObjectOptions{ContentType: "application/octet-stream", UserMetadata: map[string]string{"sha256": checksum}}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		return Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	obj, err := s.store.GetObject(ctx, s.cfg.BackupBucket, key, minio.GetObjectOptions{}) /* 更新 err 的值。 */
	if err != nil {                                                                       /* 判断条件并选择处理分支。 */
		return Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	h := sha256.New()                                                          /* 更新 h 的值。 */
	n, err := io.Copy(h, obj)                                                  /* 更新 err 的值。 */
	obj.Close()                                                                /* 执行当前语句并推进处理流程。 */
	if err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != checksum { /* 判断条件并选择处理分支。 */
		return Artifact{}, fmt.Errorf("uploaded object verification failed: %s", filename) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return Artifact{Component: componentName(filename), Filename: filename, ObjectKey: key, SHA256: checksum, Size: size}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) ensureBucket(ctx context.Context, client *minio.Client, bucket string) error { /* 定义 ensureBucket 函数。 */
	exists, err := client.BucketExists(ctx, bucket) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !exists { /* 判断条件并选择处理分支。 */
		return client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func writeJSON(path string, value any) error { /* 定义 writeJSON 函数。 */
	data, err := json.MarshalIndent(value, "", "  ") /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return os.WriteFile(path, data, 0o600) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func hashFile(path string) (string, int64, error) { /* 定义 hashFile 函数。 */
	f, err := os.Open(path) /* 更新 err 的值。 */
	if err != nil {         /* 判断条件并选择处理分支。 */
		return "", 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer f.Close()                               /* 安排函数结束时执行清理。 */
	h := sha256.New()                             /* 更新 h 的值。 */
	n, err := io.Copy(h, f)                       /* 更新 err 的值。 */
	return hex.EncodeToString(h.Sum(nil)), n, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func componentName(filename string) string { /* 定义 componentName 函数。 */
	if i := strings.Index(filename, "-"); i > 0 { /* 判断条件并选择处理分支。 */
		return filename[:i] /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if i := strings.Index(filename, "."); i > 0 { /* 判断条件并选择处理分支。 */
		return filename[:i] /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return filename /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func errorString(err error) string { /* 定义 errorString 函数。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return err.Error() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
