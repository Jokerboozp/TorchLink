package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

func (r *Repository) CreateEdgeReadJob(ctx context.Context, j model.EdgeReadJob) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize each node's queue capacity, including concurrent API instances.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 792))`, j.TenantID+"/"+j.NodeID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM edge_read_job WHERE tenant_id=$1 AND node_id=$2 AND expires_at < (extract(epoch from clock_timestamp())*1000)::bigint-3600000`, j.TenantID, j.NodeID); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM edge_read_job WHERE tenant_id=$1 AND node_id=$2 AND expires_at > (extract(epoch from clock_timestamp())*1000)::bigint AND status IN ('PENDING','RUNNING')`, j.TenantID, j.NodeID).Scan(&count); err != nil {
		return err
	}
	if count >= 8 {
		return errors.New("edge diagnostic queue full")
	}
	j.Status, j.Token, j.Raw, j.Error = "PENDING", "", nil, ""
	body, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO edge_read_job(tenant_id,id,node_id,expires_at,status,body) VALUES($1,$2,$3,$4,'PENDING',$5)`, j.TenantID, j.ID, j.NodeID, j.ExpiresAt, body)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) GetEdgeReadJob(ctx context.Context, tenant, id string) (model.EdgeReadJob, error) {
	var j model.EdgeReadJob
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM edge_read_job WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&body)
	if err == nil {
		err = json.Unmarshal(body, &j)
	}
	return j, err
}
func (r *Repository) ClaimEdgeReadJob(ctx context.Context, tenant, node, token string) (model.EdgeReadJob, error) {
	var j model.EdgeReadJob
	var body []byte
	err := r.pool.QueryRow(ctx, `WITH next AS (SELECT tenant_id,id FROM edge_read_job WHERE tenant_id=$1 AND node_id=$2 AND status='PENDING' AND expires_at > (extract(epoch from clock_timestamp())*1000)::bigint ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT 1)
UPDATE edge_read_job j SET status='RUNNING', body=j.body || jsonb_build_object('status','RUNNING','token',$3::text) FROM next WHERE j.tenant_id=next.tenant_id AND j.id=next.id RETURNING j.body`, tenant, node, token).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, nil
	}
	if err == nil {
		err = json.Unmarshal(body, &j)
	}
	return j, err
}
func (r *Repository) FinishEdgeReadJob(ctx context.Context, j model.EdgeReadJob) error {
	patch, err := json.Marshal(map[string]any{"raw": j.Raw, "error": j.Error, "status": "DONE"})
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE edge_read_job SET status='DONE',body=body || $5::jsonb WHERE tenant_id=$1 AND id=$2 AND node_id=$3 AND status='RUNNING' AND body->>'token'=$4 AND expires_at > (extract(epoch from clock_timestamp())*1000)::bigint`, j.TenantID, j.ID, j.NodeID, j.Token, patch)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("expired or unowned edge diagnostic")
	}
	return err
}
