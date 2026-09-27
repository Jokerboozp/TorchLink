package postgres

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolOptions tune the primary pool and an optional read replica.
type PoolOptions struct {
	MaxConns          int32
	MaxConnLifetime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
	// ReadDSN is a hot standby used only by history and report queries that
	// tolerate lag. Queries return to the primary while the standby is
	// unreachable or more than MaxReplicaLag behind.
	ReadDSN       string
	MaxReplicaLag time.Duration
}

func applyPoolOptions(config *pgxpool.Config, dsn string, o PoolOptions) {
	if o.MaxConns > 0 && !strings.Contains(dsn, "pool_max_conns") {
		config.MaxConns = o.MaxConns
	}
	// Recycling connections lets a DSN listing several hosts with
	// target_session_attrs=read-write reach a newly promoted primary.
	if o.MaxConnLifetime > 0 {
		config.MaxConnLifetime = o.MaxConnLifetime
	}
	if o.HealthCheckPeriod > 0 {
		config.HealthCheckPeriod = o.HealthCheckPeriod
	}
	if o.ConnectTimeout > 0 {
		config.ConnConfig.ConnectTimeout = o.ConnectTimeout
	}
}

// replica tracks whether the standby may serve reads.
type replica struct {
	pool   *pgxpool.Pool
	maxLag time.Duration
	usable atomic.Bool
	lagMS  atomic.Int64
}

// watch samples replay lag every few seconds. A standby that has replayed
// everything it received counts as zero lag even when the primary is idle.
func (p *replica) watch(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		p.sample(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (p *replica) sample(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var lag float64
	err := p.pool.QueryRow(c, `SELECT CASE WHEN NOT pg_is_in_recovery() THEN 0 WHEN pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0 ELSE COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - pg_last_xact_replay_timestamp()), 1e9) END`).Scan(&lag)
	if err != nil {
		p.usable.Store(false)
		p.lagMS.Store(-1)
		return
	}
	ms := int64(lag * 1000)
	p.lagMS.Store(ms)
	p.usable.Store(time.Duration(ms)*time.Millisecond <= p.maxLag)
}

// reader returns the replica pool when it is healthy and within the lag
// budget, otherwise the primary.
func (r *Repository) reader() *pgxpool.Pool {
	if r.replica != nil && r.replica.usable.Load() {
		return r.replica.pool
	}
	return r.pool
}

// ReplicaLag reports the sampled standby lag in milliseconds (-1 unknown)
// and whether reads currently use it.
func (r *Repository) ReplicaLag() (int64, bool) {
	if r.replica == nil {
		return -1, false
	}
	return r.replica.lagMS.Load(), r.replica.usable.Load()
}
