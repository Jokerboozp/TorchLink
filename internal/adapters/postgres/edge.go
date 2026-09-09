package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
)

func (r *Repository) SetEdgeCredential(ctx context.Context, tenant, id, hash string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE edge_node SET secret_hash=$3 WHERE tenant_id=$1 AND id=$2`, tenant, id, hash)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}
func (r *Repository) GetEdgeCredential(ctx context.Context, tenant, id string) (string, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `SELECT secret_hash FROM edge_node WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&hash)
	return hash, err
}
func (r *Repository) SaveEdgeHeartbeat(ctx context.Context, tenant, id string, v model.EdgeHeartbeat) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE edge_node SET heartbeat=$3 WHERE tenant_id=$1 AND id=$2`, tenant, id, body)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}
func (r *Repository) GetEdgeHeartbeat(ctx context.Context, tenant, id string) (model.EdgeHeartbeat, error) {
	var v model.EdgeHeartbeat
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT heartbeat FROM edge_node WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&body)
	if err == nil {
		err = json.Unmarshal(body, &v)
	}
	return v, err
}
