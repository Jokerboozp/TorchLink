package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) GetEdgeProgram(ctx context.Context, tenant, node string) (model.EdgeProgram, error) {
	var v model.EdgeProgram
	v.TenantID = tenant
	v.NodeID = node
	var status []byte
	err := r.pool.QueryRow(ctx, `SELECT generation,target_version,updated_at,status FROM edge_program WHERE tenant_id=$1 AND node_id=$2`, tenant, node).Scan(&v.Generation, &v.TargetVersion, &v.UpdatedAt, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, nil
	}
	if err == nil {
		err = json.Unmarshal(status, &v.Status)
	}
	return v, err
}
func (r *Repository) SetEdgeProgram(ctx context.Context, tenant, node string, expected int64, version string) (model.EdgeProgram, error) {
	var v model.EdgeProgram
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO edge_program(tenant_id,node_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, tenant, node); err != nil {
		return v, err
	}
	var status []byte
	err = tx.QueryRow(ctx, `UPDATE edge_program SET generation=generation+1,target_version=$4,updated_at=$5 WHERE tenant_id=$1 AND node_id=$2 AND generation=$3 RETURNING generation,target_version,updated_at,status`, tenant, node, expected, version, time.Now().UnixMilli()).Scan(&v.Generation, &v.TargetVersion, &v.UpdatedAt, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, model.ErrEdgeProgramConflict
	}
	if err != nil {
		return v, err
	}
	v.TenantID = tenant
	v.NodeID = node
	if err = json.Unmarshal(status, &v.Status); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) ReportEdgeProgram(ctx context.Context, tenant, node string, status model.EdgeProgramStatus) error {
	status.LastSeenAt = time.Now().UnixMilli()
	b, err := json.Marshal(status)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO edge_program(tenant_id,node_id,status) VALUES($1,$2,$3) ON CONFLICT(tenant_id,node_id) DO UPDATE SET status=excluded.status`, tenant, node, b)
	return err
}
