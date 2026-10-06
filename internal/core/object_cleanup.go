package core

import (
	"context"

	"iot-platform/internal/ports"
)

// objectCleanupBatch is how many queued files one cleanup pass deletes.
const objectCleanupBatch = 200

// DeleteObjectLater deletes a file whose record is gone; when the delete
// fails the file is queued for CleanupObjectsOnce.
func (e *Engine) DeleteObjectLater(ctx context.Context, bucket, key string) {
	if deleter, ok := e.Archive.(ports.ObjectDeleter); ok && deleter.DeleteObject(ctx, bucket, key) == nil {
		return
	}
	if err := e.Repo.EnqueueObjectCleanup(context.WithoutCancel(ctx), bucket, key); err != nil && e.Log != nil {
		e.Log.Warn("queue object cleanup failed", "bucket", bucket, "key", key, "error", err)
	}
}

// CleanupObjectsOnce deletes queued files; it runs as a Jobs singleton.
func (e *Engine) CleanupObjectsOnce(ctx context.Context) error {
	deleter, ok := e.Archive.(ports.ObjectDeleter)
	if !ok {
		return nil
	}
	refs, err := e.Repo.PendingObjectCleanups(ctx, objectCleanupBatch)
	if err != nil {
		return err
	}
	var failed error
	for _, ref := range refs {
		if err = deleter.DeleteObject(ctx, ref.Bucket, ref.Key); err != nil {
			failed = err
			continue
		}
		if err = e.Repo.FinishObjectCleanup(ctx, ref.Bucket, ref.Key); err != nil {
			return err
		}
	}
	return failed
}
