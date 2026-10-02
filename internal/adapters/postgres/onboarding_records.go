package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

const onboardingColumns = `tenant_id,id,owner_id,kind,status,revision,created_at,updated_at,body`

func scanOnboardingRecord(row pgx.Row) (model.OnboardingRecord, error) {
	var v model.OnboardingRecord
	err := row.Scan(&v.TenantID, &v.ID, &v.OwnerID, &v.Kind, &v.Status, &v.Revision, &v.CreatedAt, &v.UpdatedAt, &v.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = model.ErrNotFound
	}
	return v, err
}

func (r *Repository) GetOnboardingRecord(ctx context.Context, tenant, id string) (model.OnboardingRecord, error) {
	return scanOnboardingRecord(r.pool.QueryRow(ctx, `SELECT `+onboardingColumns+` FROM onboarding_record WHERE tenant_id=$1 AND id=$2`, tenant, id))
}

func (r *Repository) SaveOnboardingRecord(ctx context.Context, v model.OnboardingRecord, expected int64) (model.OnboardingRecord, error) {
	if v.TenantID == "" || v.ID == "" || v.OwnerID == "" || v.Kind == "" || expected < 0 || !json.Valid(v.Body) {
		return v, errors.New("invalid onboarding record")
	}
	now := time.Now().UnixMilli()
	var row pgx.Row
	if expected == 0 {
		row = r.pool.QueryRow(ctx, `INSERT INTO onboarding_record (`+onboardingColumns+`) VALUES($1,$2,$3,$4,$5,1,$6,$6,$7) ON CONFLICT DO NOTHING RETURNING `+onboardingColumns, v.TenantID, v.ID, v.OwnerID, v.Kind, v.Status, now, v.Body)
	} else {
		row = r.pool.QueryRow(ctx, `UPDATE onboarding_record SET status=$5,revision=revision+1,updated_at=$6,body=$7 WHERE tenant_id=$1 AND id=$2 AND owner_id=$3 AND kind=$4 AND revision=$8 RETURNING `+onboardingColumns, v.TenantID, v.ID, v.OwnerID, v.Kind, v.Status, now, v.Body, expected)
	}
	saved, err := scanOnboardingRecord(row)
	if errors.Is(err, model.ErrNotFound) {
		err = model.ErrOnboardingChanged
	}
	return saved, err
}

func (r *Repository) ListOnboardingRecords(ctx context.Context, tenant, owner, kind string, limit, offset int) ([]model.OnboardingRecord, int, error) {
	if tenant == "" {
		return nil, 0, errors.New("onboarding tenant is required")
	}
	limit, offset = normalizePage(limit, offset)
	const where = ` FROM onboarding_record WHERE tenant_id=$1 AND ($2='' OR owner_id=$2) AND ($3='' OR kind=$3)`
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*)`+where, tenant, owner, kind).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "updated_at DESC,id"
	if strings.HasPrefix(kind, "device-batch-row:") {
		order = "id"
	}
	rows, err := r.pool.Query(ctx, `SELECT `+onboardingColumns+where+` ORDER BY `+order+` LIMIT $4 OFFSET $5`, tenant, owner, kind, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.OnboardingRecord{}
	for rows.Next() {
		v, err := scanOnboardingRecord(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func (r *Repository) ListPendingOnboardingRecords(ctx context.Context, kind string, limit int) ([]model.OnboardingRecord, error) {
	limit, _ = normalizePage(limit, 0)
	rows, err := r.pool.Query(ctx, `SELECT `+onboardingColumns+` FROM onboarding_record WHERE kind=$1 AND status IN ('INITIALIZING','QUEUED','RUNNING') ORDER BY updated_at,id LIMIT $2`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.OnboardingRecord{}
	for rows.Next() {
		v, err := scanOnboardingRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
