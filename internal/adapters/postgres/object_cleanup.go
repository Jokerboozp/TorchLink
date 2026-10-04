package postgres

import (
	"context"

	"iot-platform/internal/model"
)

func (r *Repository) EnqueueObjectCleanup(ctx context.Context, bucket, key string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO object_cleanup(bucket,object_key) VALUES($1,$2) ON CONFLICT DO NOTHING`, bucket, key)
	return err
}

func (r *Repository) PendingObjectCleanups(ctx context.Context, limit int) ([]model.ObjectRef, error) {
	rows, err := r.pool.Query(ctx, `SELECT bucket, object_key FROM object_cleanup ORDER BY created_at, bucket, object_key LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ObjectRef{}
	for rows.Next() {
		var ref model.ObjectRef
		if err = rows.Scan(&ref.Bucket, &ref.Key); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (r *Repository) FinishObjectCleanup(ctx context.Context, bucket, key string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM object_cleanup WHERE bucket=$1 AND object_key=$2`, bucket, key)
	return err
}
