package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
)

// Metadata and ordered details commit together. Finished jobs cannot be rewritten.
func (r *Repository) saveInspection(ctx context.Context, v model.HealthInspectionJob, create bool) (bool, error) {
	items := v.Report.Items
	v.Report.ReportID, v.Report.TotalItems, v.Report.Items = v.ID, len(items), nil
	b, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	query := `UPDATE health_inspection_job SET status=$3,started_at=$4,updated_at=$5,body=$6 WHERE tenant_id=$1 AND id=$2 AND status='running'`
	if create {
		query = `INSERT INTO health_inspection_job(tenant_id,id,status,started_at,updated_at,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`
	}
	tag, err := tx.Exec(ctx, query, v.TenantID, v.ID, v.Status, v.StartedAt, v.UpdatedAt, b)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if len(items) > 0 {
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"health_inspection_item"}, []string{"tenant_id", "job_id", "position", "body"}, pgx.CopyFromSlice(len(items), func(i int) ([]any, error) {
			body, e := json.Marshal(items[i])
			return []any{v.TenantID, v.ID, i, body}, e
		}))
		if err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}
func (r *Repository) CreateHealthInspectionJob(ctx context.Context, v model.HealthInspectionJob) (bool, error) {
	return r.saveInspection(ctx, v, true)
}
func (r *Repository) UpdateRunningHealthInspectionJob(ctx context.Context, v model.HealthInspectionJob) (bool, error) {
	return r.saveInspection(ctx, v, false)
}

func (r *Repository) LatestHealthInspectionSummary(ctx context.Context, tenant, status string) (model.HealthInspectionJob, error) {
	var v model.HealthInspectionJob
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM health_inspection_job WHERE tenant_id=$1 AND ($2='' OR status=$2) ORDER BY started_at DESC,updated_at DESC,id DESC LIMIT 1`, tenant, status).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	return v, err
}
func (r *Repository) HealthInspectionPage(ctx context.Context, tenant, id string, limit, offset int) (model.HealthInspectionJob, error) {
	var v model.HealthInspectionJob
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM health_inspection_job WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	limit = max(1, min(limit, 2000))
	offset = max(0, offset)
	rows, err := r.pool.Query(ctx, `SELECT body FROM health_inspection_item WHERE tenant_id=$1 AND job_id=$2 AND position >= $3 AND position < $3+$4 ORDER BY position`, tenant, id, offset, limit)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	v.Report.Items = []model.DeviceHealthItem{}
	for rows.Next() {
		var item model.DeviceHealthItem
		if err = rows.Scan(&b); err != nil {
			return v, err
		}
		if err = json.Unmarshal(b, &item); err != nil {
			return v, err
		}
		v.Report.Items = append(v.Report.Items, item)
	}
	return v, rows.Err()
}

// Full reads remain available to internal callers; HTTP uses summary/pages.
func (r *Repository) LatestHealthInspectionJob(ctx context.Context, tenant, status string) (model.HealthInspectionJob, error) {
	v, err := r.LatestHealthInspectionSummary(ctx, tenant, status)
	if err != nil {
		return v, err
	}
	for offset := 0; offset < v.Report.TotalItems; offset += 2000 {
		page, e := r.HealthInspectionPage(ctx, tenant, v.ID, 2000, offset)
		if e != nil {
			return v, e
		}
		v.Report.Items = append(v.Report.Items, page.Report.Items...)
	}
	return v, nil
}
