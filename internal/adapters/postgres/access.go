package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

func (r *Repository) LoadAccessState(ctx context.Context, tenant string) (model.AccessState, error) {
	var state model.AccessState
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body,revision FROM platform_access WHERE tenant_id=$1`, tenant).Scan(&body, &state.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	revision := state.Revision
	err = json.Unmarshal(body, &state)
	state.Revision = revision
	return state, err
}
func (r *Repository) SaveAccessState(ctx context.Context, tenant string, state model.AccessState) (bool, error) {
	body, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	result, err := r.pool.Exec(ctx, `INSERT INTO platform_access(tenant_id,revision,body) SELECT $1,1,$2::jsonb WHERE $3::bigint=0 ON CONFLICT(tenant_id) DO NOTHING`, tenant, body, state.Revision)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 1 {
		return true, nil
	}
	result, err = r.pool.Exec(ctx, `UPDATE platform_access SET revision=revision+1,body=$2::jsonb WHERE tenant_id=$1 AND revision=$3`, tenant, body, state.Revision)
	return result.RowsAffected() == 1, err
}
