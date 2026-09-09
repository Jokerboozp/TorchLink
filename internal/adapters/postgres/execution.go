package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) AcquireExecutionLease(ctx context.Context, tenant, resource, owner, endpoint string, ttl time.Duration) (model.ExecutionLease, bool, error) {
	var v model.ExecutionLease
	if tenant == "" || resource == "" || owner == "" || ttl < time.Second || ttl > time.Minute {
		return v, false, errors.New("invalid execution lease")
	}
	err := r.pool.QueryRow(ctx, `INSERT INTO execution_lease(tenant_id,resource,owner,endpoint,expires_at) VALUES($1,$2,$3,$4,clock_timestamp()+$5*interval '1 millisecond') ON CONFLICT(tenant_id,resource) DO UPDATE SET owner=excluded.owner,endpoint=excluded.endpoint,expires_at=excluded.expires_at,token=CASE WHEN execution_lease.expires_at<=clock_timestamp() THEN execution_lease.token+1 ELSE execution_lease.token END WHERE execution_lease.owner=excluded.owner OR execution_lease.expires_at<=clock_timestamp() RETURNING tenant_id,resource,owner,endpoint,token,(extract(epoch FROM expires_at)*1000)::bigint`, tenant, resource, owner, endpoint, ttl.Milliseconds()).Scan(&v.TenantID, &v.Resource, &v.Owner, &v.Endpoint, &v.Token, &v.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, false, nil
	}
	return v, err == nil, err
}
func (r *Repository) GetExecutionLease(ctx context.Context, tenant, resource string) (model.ExecutionLease, error) {
	var v model.ExecutionLease
	err := r.pool.QueryRow(ctx, `SELECT tenant_id,resource,owner,endpoint,token,(extract(epoch FROM expires_at)*1000)::bigint FROM execution_lease WHERE tenant_id=$1 AND resource=$2 AND expires_at>clock_timestamp()`, tenant, resource).Scan(&v.TenantID, &v.Resource, &v.Owner, &v.Endpoint, &v.Token, &v.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
func (r *Repository) ReleaseExecutionLease(ctx context.Context, v model.ExecutionLease) error {
	_, err := r.pool.Exec(ctx, `UPDATE execution_lease SET expires_at=to_timestamp(0) WHERE tenant_id=$1 AND resource=$2 AND owner=$3 AND token=$4`, v.TenantID, v.Resource, v.Owner, v.Token)
	return err
}
