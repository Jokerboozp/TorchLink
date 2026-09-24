package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/minio/minio-go/v7"
)

var ErrTaskRunning = errors.New("backup task is running")
var ErrTaskReferenced = errors.New("backup is referenced by a restore drill")

// DeleteTask clears all object-store artifacts and the worker's local copy
// before dropping the catalog entry. A failed cleanup leaves the entry for retry.
func (s *Service) DeleteTask(ctx context.Context, id string) error {
	if err := validateSegment(id, "backup id"); err != nil {
		return err
	}
	if !s.mu.TryLock() {
		return ErrTaskRunning
	}
	defer s.mu.Unlock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, "SELECT status FROM backup_task WHERE id=$1 FOR UPDATE", id).Scan(&status); err != nil {
		return err
	}
	if status == "RUNNING" {
		return ErrTaskRunning
	}
	var referenced bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backup_task WHERE backup_type='RESTORE_DRILL' AND details->>'backupId'=$1)", id).Scan(&referenced); err != nil {
		return err
	}
	if referenced {
		return ErrTaskReferenced
	}
	prefix := "backup/" + id + "/"
	for object := range s.store.ListObjects(ctx, s.cfg.BackupBucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return object.Err
		}
		if err = s.store.RemoveObject(ctx, s.cfg.BackupBucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	base, err := filepath.Abs(s.cfg.BackupDir)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Join(base, id))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil || rel != id {
		return fmt.Errorf("invalid backup directory")
	}
	if err = os.RemoveAll(target); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM backup_task WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
