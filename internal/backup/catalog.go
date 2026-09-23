package backup /* 声明 backup 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"github.com/jackc/pgx/v5/pgtype" /* 执行当前语句并推进处理流程。 */
	"github.com/minio/minio-go/v7"   /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Task is the durable history entry written by the backup worker.
// Details is returned by GetTask, but intentionally omitted from list rows so
// the history page stays small even when a manifest contains many artifacts.
type Task struct { /* 定义 Task 类型。 */
	ID          string         `json:"id"`                    /* 执行当前语句并推进处理流程。 */
	Type        string         `json:"type"`                  /* 执行当前语句并推进处理流程。 */
	Status      string         `json:"status"`                /* 执行当前语句并推进处理流程。 */
	ObjectKey   string         `json:"objectKey,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Checksum    string         `json:"checksum,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Details     map[string]any `json:"details,omitempty"`     /* 执行当前语句并推进处理流程。 */
	StartedAt   *time.Time     `json:"startedAt,omitempty"`   /* 执行当前语句并推进处理流程。 */
	CompletedAt *time.Time     `json:"completedAt,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type TaskPage struct { /* 定义 TaskPage 类型。 */
	Items    []Task `json:"items"`    /* 执行当前语句并推进处理流程。 */
	Total    int    `json:"total"`    /* 执行当前语句并推进处理流程。 */
	Limit    int    `json:"limit"`    /* 执行当前语句并推进处理流程。 */
	Offset   int    `json:"offset"`   /* 执行当前语句并推进处理流程。 */
	Page     int    `json:"page"`     /* 执行当前语句并推进处理流程。 */
	PageSize int    `json:"pageSize"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ArtifactPage struct { /* 定义 ArtifactPage 类型。 */
	Manifest     /* 执行当前语句并推进处理流程。 */
	Total    int `json:"total"`    /* 执行当前语句并推进处理流程。 */
	Limit    int `json:"limit"`    /* 执行当前语句并推进处理流程。 */
	Offset   int `json:"offset"`   /* 执行当前语句并推进处理流程。 */
	Page     int `json:"page"`     /* 执行当前语句并推进处理流程。 */
	PageSize int `json:"pageSize"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ListTasks returns system-level backup and restore-drill history. Backup
// records are not tenant scoped because the worker protects the whole
// platform's persistence layer.
func (s *Service) ListTasks(ctx context.Context, backupType, status string, limit, offset int) (TaskPage, error) { /* 定义 ListTasks 函数。 */
	backupType = strings.ToUpper(strings.TrimSpace(backupType)) /* 更新 backupType 的值。 */
	status = strings.ToUpper(strings.TrimSpace(status))         /* 更新 status 的值。 */
	if backupType != "" && !validBackupType(backupType) {       /* 判断条件并选择处理分支。 */
		return TaskPage{}, fmt.Errorf("unsupported backup type: %s", backupType) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if status != "" && !validBackupStatus(status) { /* 判断条件并选择处理分支。 */
		return TaskPage{}, fmt.Errorf("unsupported backup status: %s", status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 20 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if limit > 100 { /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset < 0 { /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */

	where := `WHERE ($1 = '' OR backup_type = $1) AND ($2 = '' OR status = $2)`                                              /* 更新 where 的值。 */
	var total int                                                                                                            /* 声明 total。 */
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backup_task `+where, backupType, status).Scan(&total); err != nil { /* 判断条件并选择处理分支。 */
		return TaskPage{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := s.pool.Query(ctx, `
		SELECT id, backup_type, status, COALESCE(object_key, ''), COALESCE(checksum, ''), started_at, completed_at
		FROM backup_task `+where+`
		ORDER BY COALESCE(completed_at, started_at) DESC NULLS LAST, id DESC
		LIMIT $3 OFFSET $4`, backupType, status, limit, offset)
	if err != nil { /* 判断条件并选择处理分支。 */
		return TaskPage{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()              /* 安排函数结束时执行清理。 */
	items := make([]Task, 0, limit) /* 更新 items 的值。 */
	for rows.Next() {               /* 循环处理当前数据。 */
		item, scanErr := scanTask(rows, false) /* 更新 scanErr 的值。 */
		if scanErr != nil {                    /* 判断条件并选择处理分支。 */
			return TaskPage{}, scanErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = append(items, item) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err = rows.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return TaskPage{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return TaskPage{Items: items, Total: total, Limit: limit, Offset: offset, Page: offset/limit + 1, PageSize: limit}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) GetTask(ctx context.Context, id string) (Task, error) { /* 定义 GetTask 函数。 */
	if err := validateSegment(id, "backup id"); err != nil { /* 判断条件并选择处理分支。 */
		return Task{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return scanTask(s.pool.QueryRow(ctx, `
		SELECT id, backup_type, status, COALESCE(object_key, ''), COALESCE(checksum, ''), details, started_at, completed_at
		FROM backup_task WHERE id = $1`, id), true)
} /* 结束当前表达式或代码块。 */

// ReadManifest reads the manifest object that the worker uploaded to MinIO.
func (s *Service) ReadManifest(ctx context.Context, id string) (Manifest, error) { /* 定义 ReadManifest 函数。 */
	if err := validateSegment(id, "backup id"); err != nil { /* 判断条件并选择处理分支。 */
		return Manifest{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	object, err := s.store.GetObject(ctx, s.cfg.BackupBucket, manifestKey(id), minio.GetObjectOptions{}) /* 更新 err 的值。 */
	if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
		return Manifest{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer object.Close()                    /* 安排函数结束时执行清理。 */
	if _, err = object.Stat(); err != nil { /* 判断条件并选择处理分支。 */
		return Manifest{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var manifest Manifest                                            /* 声明 manifest。 */
	if err = json.NewDecoder(object).Decode(&manifest); err != nil { /* 判断条件并选择处理分支。 */
		return Manifest{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return manifest, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// ListArtifacts returns the manifest files plus the manifest itself. The
// worker writes the manifest before adding its own artifact record, so the
// latter is added here for the UI and download API.
func (s *Service) ListArtifacts(ctx context.Context, id string) (Manifest, error) { /* 定义 ListArtifacts 函数。 */
	manifest, err := s.ReadManifest(ctx, id) /* 更新 err 的值。 */
	if err != nil {                          /* 判断条件并选择处理分支。 */
		return Manifest{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	task, err := s.GetTask(ctx, id) /* 更新 err 的值。 */
	if err != nil {                 /* 判断条件并选择处理分支。 */
		return Manifest{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, artifact := range manifest.Artifacts { /* 循环处理当前数据。 */
		if artifact.Filename == "manifest.json" { /* 判断条件并选择处理分支。 */
			return manifest, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	manifestInfo, err := s.store.StatObject(ctx, s.cfg.BackupBucket, manifestKey(id), minio.StatObjectOptions{}) /* 更新 err 的值。 */
	if err != nil {                                                                                              /* 判断条件并选择处理分支。 */
		return Manifest{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	manifest.Artifacts = append(manifest.Artifacts, Artifact{ /* 更新 manifest.Artifacts 的值。 */
		Component: "backup-manifest", /* 执行当前语句并推进处理流程。 */
		Filename:  "manifest.json",   /* 执行当前语句并推进处理流程。 */
		ObjectKey: manifestKey(id),   /* 执行当前语句并推进处理流程。 */
		SHA256:    task.Checksum,     /* 执行当前语句并推进处理流程。 */
		Size:      manifestInfo.Size, /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	return manifest, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) ListArtifactsPage(ctx context.Context, id string, limit, offset int) (ArtifactPage, error) { /* 定义 ListArtifactsPage 函数。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 20 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if limit > 100 { /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset < 0 { /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	manifest, err := s.ListArtifacts(ctx, id) /* 更新 err 的值。 */
	if err != nil {                           /* 判断条件并选择处理分支。 */
		return ArtifactPage{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	total := len(manifest.Artifacts) /* 更新 total 的值。 */
	if offset >= total {             /* 判断条件并选择处理分支。 */
		manifest.Artifacts = []Artifact{} /* 更新 manifest.Artifacts 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		end := offset + limit /* 更新 end 的值。 */
		if end > total {      /* 判断条件并选择处理分支。 */
			end = total /* 更新 end 的值。 */
		} /* 结束当前表达式或代码块。 */
		manifest.Artifacts = manifest.Artifacts[offset:end] /* 更新 manifest.Artifacts 的值。 */
	} /* 结束当前表达式或代码块。 */
	return ArtifactPage{Manifest: manifest, Total: total, Limit: limit, Offset: offset, Page: offset/limit + 1, PageSize: limit}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// OpenArtifact opens a file after checking that it belongs to the selected
// backup's manifest. Callers must close the returned reader.
func (s *Service) OpenArtifact(ctx context.Context, id, filename string) (io.ReadCloser, Artifact, error) { /* 定义 OpenArtifact 函数。 */
	if err := validateSegment(id, "backup id"); err != nil { /* 判断条件并选择处理分支。 */
		return nil, Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateSegment(filename, "artifact filename"); err != nil { /* 判断条件并选择处理分支。 */
		return nil, Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	manifest, err := s.ListArtifacts(ctx, id) /* 更新 err 的值。 */
	if err != nil {                           /* 判断条件并选择处理分支。 */
		return nil, Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var selected Artifact                         /* 声明 selected。 */
	for _, artifact := range manifest.Artifacts { /* 循环处理当前数据。 */
		if artifact.Filename == filename { /* 判断条件并选择处理分支。 */
			selected = artifact /* 更新 selected 的值。 */
			break               /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if selected.Filename == "" { /* 判断条件并选择处理分支。 */
		return nil, Artifact{}, fmt.Errorf("artifact not found: %s", filename) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	prefix := "backup/" + id + "/"                      /* 更新 prefix 的值。 */
	if !strings.HasPrefix(selected.ObjectKey, prefix) { /* 判断条件并选择处理分支。 */
		return nil, Artifact{}, fmt.Errorf("artifact object key is invalid") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	objectName := selected.ObjectKey[len(prefix):]                                                                            /* 更新 objectName 的值。 */
	if objectName != selected.Filename || filepath.Base(objectName) != objectName || strings.ContainsAny(objectName, `/\\`) { /* 判断条件并选择处理分支。 */
		return nil, Artifact{}, fmt.Errorf("artifact object key is invalid") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	object, err := s.store.GetObject(ctx, s.cfg.BackupBucket, selected.ObjectKey, minio.GetObjectOptions{}) /* 更新 err 的值。 */
	if err != nil {                                                                                         /* 判断条件并选择处理分支。 */
		return nil, Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	info, err := object.Stat() /* 更新 err 的值。 */
	if err != nil {            /* 判断条件并选择处理分支。 */
		object.Close()              /* 执行当前语句并推进处理流程。 */
		return nil, Artifact{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if selected.Size == 0 { /* 判断条件并选择处理分支。 */
		selected.Size = info.Size /* 更新 selected.Size 的值。 */
	} /* 结束当前表达式或代码块。 */
	return object, selected, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func scanTask(row interface{ Scan(...any) error }, withDetails bool) (Task, error) { /* 定义 scanTask 函数。 */
	var item Task                                                                      /* 声明 item。 */
	var details []byte                                                                 /* 声明 details。 */
	var started, completed pgtype.Timestamptz                                          /* 声明 started。 */
	args := []any{&item.ID, &item.Type, &item.Status, &item.ObjectKey, &item.Checksum} /* 更新 args 的值。 */
	if withDetails {                                                                   /* 判断条件并选择处理分支。 */
		args = append(args, &details) /* 更新 args 的值。 */
	} /* 结束当前表达式或代码块。 */
	args = append(args, &started, &completed) /* 更新 args 的值。 */
	if err := row.Scan(args...); err != nil { /* 判断条件并选择处理分支。 */
		return Task{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if withDetails && len(details) > 0 { /* 判断条件并选择处理分支。 */
		if err := json.Unmarshal(details, &item.Details); err != nil { /* 判断条件并选择处理分支。 */
			return Task{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if started.Valid { /* 判断条件并选择处理分支。 */
		value := started.Time   /* 更新 value 的值。 */
		item.StartedAt = &value /* 更新 item.StartedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if completed.Valid { /* 判断条件并选择处理分支。 */
		value := completed.Time   /* 更新 value 的值。 */
		item.CompletedAt = &value /* 更新 item.CompletedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	return item, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func manifestKey(id string) string { return "backup/" + id + "/manifest.json" } /* 定义 manifestKey 函数。 */

func validateSegment(value, label string) error { /* 定义 validateSegment 函数。 */
	value = strings.TrimSpace(value)                                                                                        /* 更新 value 的值。 */
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("invalid %s", label) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validBackupType(value string) bool { /* 定义 validBackupType 函数。 */
	return value == "DEVICE_DAILY" || value == "FULL" || value == "INCREMENTAL" || value == "RAW_LOGS" || value == "RESTORE_DRILL" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validBackupStatus(value string) bool { /* 定义 validBackupStatus 函数。 */
	return value == "RUNNING" || value == "COMPLETED" || value == "FAILED" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
