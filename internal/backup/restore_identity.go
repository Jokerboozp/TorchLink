package backup

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Transaction advisory locks are scoped to the actual PostgreSQL database.
// This catches alias hostnames, forwarding ports and proxies before business
// DDL, and rollback releases both probes even with transaction pooling.
func restoreConnectedTargetSafe(ctx context.Context, source *pgxpool.Pool, target *pgx.Conn) error {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return errors.New("restore isolation verification unavailable")
	}
	key := int64(binary.BigEndian.Uint64(random[:]))
	sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("restore source isolation verification unavailable")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sourceTx.Rollback(cleanup)
	}()
	if _, err = sourceTx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		return errors.New("restore source isolation verification failed")
	}
	targetTx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("restore target isolation verification unavailable")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := targetTx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			target.Close(cleanup)
		}
	}()
	var independent bool
	if err = targetTx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, key).Scan(&independent); err != nil {
		return errors.New("restore target isolation verification failed")
	}
	if !independent {
		return ErrRestoreTargetUnsafe
	}
	return nil
}
