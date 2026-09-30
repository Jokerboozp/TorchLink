package backup

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type backupSnapshotKey struct{}
type borrowedBackupTx struct{ pgx.Tx }

func (borrowedBackupTx) Commit(context.Context) error   { return nil }
func (borrowedBackupTx) Rollback(context.Context) error { return nil }

func (s *Service) beginBackupRead(ctx context.Context) (pgx.Tx, error) {
	if tx, ok := ctx.Value(backupSnapshotKey{}).(pgx.Tx); ok {
		return borrowedBackupTx{tx}, nil
	}
	return s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
}
