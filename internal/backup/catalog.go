package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/minio/minio-go/v7"
)

// Task is the durable history entry written by the backup worker.
// Details is returned by GetTask, but intentionally omitted from list rows so
// the history page stays small even when a manifest contains many artifacts.
type Task struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	ObjectKey   string         `json:"objectKey,omitempty"`
	Checksum    string         `json:"checksum,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	StartedAt   *time.Time     `json:"startedAt,omitempty"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
}

type TaskPage struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type ArtifactPage struct {
	Manifest
	Total    int `json:"total"`
	Limit    int `json:"limit"`
	Offset   int `json:"offset"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

// ListTasks returns system-level backup and restore-drill history. Backup
// records are not tenant scoped because the worker protects the whole
// platform's persistence layer.
func (s *Service) ListTasks(ctx context.Context, backupType, status string, limit, offset int) (TaskPage, error) {
	backupType = strings.ToUpper(strings.TrimSpace(backupType))
	status = strings.ToUpper(strings.TrimSpace(status))
	if backupType != "" && !validBackupType(backupType) {
		return TaskPage{}, fmt.Errorf("unsupported backup type: %s", backupType)
	}
	if status != "" && !validBackupStatus(status) {
		return TaskPage{}, fmt.Errorf("unsupported backup status: %s", status)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	where := `WHERE ($1 = '' OR backup_type = $1) AND ($2 = '' OR status = $2)`
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM backup_task `+where, backupType, status).Scan(&total); err != nil {
		return TaskPage{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, backup_type, status, COALESCE(object_key, ''), COALESCE(checksum, ''), started_at, completed_at
		FROM backup_task `+where+`
		ORDER BY COALESCE(completed_at, started_at) DESC NULLS LAST, id DESC
		LIMIT $3 OFFSET $4`, backupType, status, limit, offset)
	if err != nil {
		return TaskPage{}, err
	}
	defer rows.Close()
	items := make([]Task, 0, limit)
	for rows.Next() {
		item, scanErr := scanTask(rows, false)
		if scanErr != nil {
			return TaskPage{}, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return TaskPage{}, err
	}
	return TaskPage{Items: items, Total: total, Limit: limit, Offset: offset, Page: offset/limit + 1, PageSize: limit}, nil
}

func (s *Service) GetTask(ctx context.Context, id string) (Task, error) {
	if err := validateSegment(id, "backup id"); err != nil {
		return Task{}, err
	}
	return scanTask(s.pool.QueryRow(ctx, `
		SELECT id, backup_type, status, COALESCE(object_key, ''), COALESCE(checksum, ''), details, started_at, completed_at
		FROM backup_task WHERE id = $1`, id), true)
}

// ReadManifest reads the manifest object that the worker uploaded to MinIO.
func (s *Service) ReadManifest(ctx context.Context, id string) (Manifest, error) {
	if err := validateSegment(id, "backup id"); err != nil {
		return Manifest{}, err
	}
	object, err := s.store.GetObject(ctx, s.cfg.BackupBucket, manifestKey(id), minio.GetObjectOptions{})
	if err != nil {
		return Manifest{}, err
	}
	defer object.Close()
	if _, err = object.Stat(); err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err = json.NewDecoder(object).Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ListArtifacts returns the manifest files plus the manifest itself. The
// worker writes the manifest before adding its own artifact record, so the
// latter is added here for the UI and download API.
func (s *Service) ListArtifacts(ctx context.Context, id string) (Manifest, error) {
	manifest, err := s.ReadManifest(ctx, id)
	if err != nil {
		return Manifest{}, err
	}
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return Manifest{}, err
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Filename == "manifest.json" {
			return manifest, nil
		}
	}
	manifestInfo, err := s.store.StatObject(ctx, s.cfg.BackupBucket, manifestKey(id), minio.StatObjectOptions{})
	if err != nil {
		return Manifest{}, err
	}
	manifest.Artifacts = append(manifest.Artifacts, Artifact{
		Component: "backup-manifest",
		Filename:  "manifest.json",
		ObjectKey: manifestKey(id),
		SHA256:    task.Checksum,
		Size:      manifestInfo.Size,
	})
	return manifest, nil
}

func (s *Service) ListArtifactsPage(ctx context.Context, id string, limit, offset int) (ArtifactPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	manifest, err := s.ListArtifacts(ctx, id)
	if err != nil {
		return ArtifactPage{}, err
	}
	total := len(manifest.Artifacts)
	if offset >= total {
		manifest.Artifacts = []Artifact{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		manifest.Artifacts = manifest.Artifacts[offset:end]
	}
	return ArtifactPage{Manifest: manifest, Total: total, Limit: limit, Offset: offset, Page: offset/limit + 1, PageSize: limit}, nil
}

// OpenArtifact opens a file after checking that it belongs to the selected
// backup's manifest. Callers must close the returned reader.
func (s *Service) OpenArtifact(ctx context.Context, id, filename string) (io.ReadCloser, Artifact, error) {
	if err := validateSegment(id, "backup id"); err != nil {
		return nil, Artifact{}, err
	}
	if err := validateSegment(filename, "artifact filename"); err != nil {
		return nil, Artifact{}, err
	}
	manifest, err := s.ListArtifacts(ctx, id)
	if err != nil {
		return nil, Artifact{}, err
	}
	var selected Artifact
	for _, artifact := range manifest.Artifacts {
		if artifact.Filename == filename {
			selected = artifact
			break
		}
	}
	if selected.Filename == "" {
		return nil, Artifact{}, fmt.Errorf("artifact not found: %s", filename)
	}
	prefix := "backup/" + id + "/"
	if !strings.HasPrefix(selected.ObjectKey, prefix) {
		return nil, Artifact{}, fmt.Errorf("artifact object key is invalid")
	}
	objectName := selected.ObjectKey[len(prefix):]
	if objectName != selected.Filename || filepath.Base(objectName) != objectName || strings.ContainsAny(objectName, `/\\`) {
		return nil, Artifact{}, fmt.Errorf("artifact object key is invalid")
	}
	object, err := s.store.GetObject(ctx, s.cfg.BackupBucket, selected.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, Artifact{}, err
	}
	info, err := object.Stat()
	if err != nil {
		object.Close()
		return nil, Artifact{}, err
	}
	if selected.Size == 0 {
		selected.Size = info.Size
	}
	return object, selected, nil
}

func scanTask(row interface{ Scan(...any) error }, withDetails bool) (Task, error) {
	var item Task
	var details []byte
	var started, completed pgtype.Timestamptz
	args := []any{&item.ID, &item.Type, &item.Status, &item.ObjectKey, &item.Checksum}
	if withDetails {
		args = append(args, &details)
	}
	args = append(args, &started, &completed)
	if err := row.Scan(args...); err != nil {
		return Task{}, err
	}
	if withDetails && len(details) > 0 {
		if err := json.Unmarshal(details, &item.Details); err != nil {
			return Task{}, err
		}
	}
	if started.Valid {
		value := started.Time
		item.StartedAt = &value
	}
	if completed.Valid {
		value := completed.Time
		item.CompletedAt = &value
	}
	return item, nil
}

func manifestKey(id string) string { return "backup/" + id + "/manifest.json" }

func validateSegment(value, label string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("invalid %s", label)
	}
	return nil
}

func validBackupType(value string) bool {
	return value == "DEVICE_DAILY" || value == "FULL" || value == "INCREMENTAL" || value == "RAW_LOGS" || value == "RESTORE_DRILL"
}

func validBackupStatus(value string) bool {
	return value == "RUNNING" || value == "COMPLETED" || value == "FAILED"
}
