package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
)

func (r *Repository) LoadFireSafetyState(ctx context.Context, tenant string) (model.FireSafetyState, error) {
	var state model.FireSafetyState
	if err := ctx.Err(); err != nil {
		return state, err
	}
	var body []byte
	err := r.pool.QueryRow(ctx, `SELECT body,revision FROM platform_fire_safety WHERE tenant_id=$1`, tenant).Scan(&body, &state.Revision)
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

func (r *Repository) SaveFireSafetyState(ctx context.Context, tenant string, state model.FireSafetyState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	revision := state.Revision
	state.Revision++
	body, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	result, err := r.pool.Exec(ctx, `INSERT INTO platform_fire_safety(tenant_id,revision,body) SELECT $1,1,$2::jsonb WHERE $3::bigint=0 ON CONFLICT(tenant_id) DO NOTHING`, tenant, body, revision)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 1 {
		return true, nil
	}
	result, err = r.pool.Exec(ctx, `UPDATE platform_fire_safety SET revision=revision+1,body=$2::jsonb WHERE tenant_id=$1 AND revision=$3`, tenant, body, revision)
	return result.RowsAffected() == 1, err
}
