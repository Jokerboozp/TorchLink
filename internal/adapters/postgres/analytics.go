package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

//go:embed analytics_schema.sql
var analyticsSchema string

var _ ports.AnalysisStore = (*Repository)(nil)

// MigrateAnalytics is also usable by migration tools. Application startup
// includes analyticsSchema in the repository's normal migration transaction.
func (r *Repository) MigrateAnalytics(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, analyticsSchema)
	return err
}

type analyticsBackend struct{ r *Repository }
type analyticsTx struct {
	ctx                context.Context
	tx                 pgx.Tx
	tenant             string
	readOnly           bool
	now, leaseDeadline int64
}

func (r *Repository) analysisStore() *analytics.Store { return analytics.NewStore(analyticsBackend{r}) }
func (b analyticsBackend) Transaction(ctx context.Context, tenant string, fn func(analytics.StorageTx) error) error {
	return b.transaction(ctx, tenant, false, fn)
}
func (b analyticsBackend) Read(ctx context.Context, tenant string, fn func(analytics.StorageTx) error) error {
	return b.transaction(ctx, tenant, true, fn)
}
func (b analyticsBackend) transaction(ctx context.Context, tenant string, readOnly bool, fn func(analytics.StorageTx) error) error {
	if tenant == "" {
		return model.ErrAnalysisInvalid
	}
	opts := pgx.TxOptions{}
	if readOnly {
		opts = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	tx, err := b.r.pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if !readOnly {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "analytics:"+tenant); err != nil {
			return err
		}
	}
	var now int64
	if err = tx.QueryRow(ctx, `SELECT floor(extract(epoch from clock_timestamp())*1000)::bigint`).Scan(&now); err != nil {
		return err
	}
	wrapped := &analyticsTx{ctx: ctx, tx: tx, tenant: tenant, readOnly: readOnly, now: now}
	if err = fn(wrapped); err != nil {
		return err
	}
	if wrapped.leaseDeadline > 0 {
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp() < to_timestamp($1::double precision/1000)`, wrapped.leaseDeadline).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return model.ErrAnalysisLeaseLost
		}
	}
	return tx.Commit(ctx)
}
func (b analyticsBackend) WorkTenants(ctx context.Context, kinds []string) ([]string, error) {
	rows, err := b.r.pool.Query(ctx, `SELECT DISTINCT tenant_id FROM analysis_document WHERE kind='run' AND application_kind=ANY($1::text[]) AND status IN ('QUEUED','PREPARING','RUNNING') ORDER BY tenant_id`, kinds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var tenant string
		if err = rows.Scan(&tenant); err != nil {
			return nil, err
		}
		out = append(out, tenant)
	}
	return out, rows.Err()
}

func (tx *analyticsTx) TransactionTime() int64          { return tx.now }
func (tx *analyticsTx) SetLeaseDeadline(deadline int64) { tx.leaseDeadline = deadline }

const analyticsDocumentColumns = `tenant_id,kind,id,run_id,device_id,application_kind,resource_id,status,device_ids,version,created_at,body`

func scanAnalytics(row pgx.Row) (d analytics.StorageDocument, err error) {
	err = row.Scan(&d.TenantID, &d.Kind, &d.ID, &d.RunID, &d.DeviceID, &d.ApplicationKind, &d.ResourceID, &d.Status, &d.DeviceIDs, &d.Version, &d.CreatedAt, &d.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = model.ErrNotFound
	}
	return
}
func (tx *analyticsTx) Get(kind, id string) (analytics.StorageDocument, error) {
	sql := `SELECT ` + analyticsDocumentColumns + ` FROM analysis_document WHERE tenant_id=$1 AND kind=$2 AND id=$3`
	if !tx.readOnly {
		sql += ` FOR UPDATE`
	}
	return scanAnalytics(tx.tx.QueryRow(tx.ctx, sql, tx.tenant, kind, id))
}
func (tx *analyticsTx) Put(d analytics.StorageDocument, expected int64) error {
	if tx.readOnly {
		return model.ErrAnalysisConflict
	}
	if d.TenantID != tx.tenant || d.Version != expected+1 {
		return model.ErrAnalysisInvalid
	}
	if d.DeviceIDs == nil {
		d.DeviceIDs = []string{}
	}
	args := []any{d.TenantID, d.Kind, d.ID, d.RunID, d.DeviceID, d.ApplicationKind, d.ResourceID, d.Status, d.DeviceIDs, d.Version, d.CreatedAt, d.Body}
	var tag pgconn.CommandTag
	var err error
	if expected == 0 {
		tag, err = tx.tx.Exec(tx.ctx, `INSERT INTO analysis_document (`+analyticsDocumentColumns+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT DO NOTHING`, args...)
	} else {
		args = append(args, expected)
		tag, err = tx.tx.Exec(tx.ctx, `UPDATE analysis_document SET run_id=$4,device_id=$5,application_kind=$6,resource_id=$7,status=$8,device_ids=$9,version=$10,created_at=$11,body=$12 WHERE tenant_id=$1 AND kind=$2 AND id=$3 AND version=$13`, args...)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return model.ErrAnalysisConflict
	}
	return nil
}
func (tx *analyticsTx) List(kind string, f model.AnalysisFilter) ([]analytics.StorageDocument, int, error) {
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	args := []any{tx.tenant, kind}
	where := `tenant_id=$1 AND kind=$2`
	add := func(clause string, v any) { args = append(args, v); where += fmt.Sprintf(" AND "+clause, len(args)) }
	if f.Kind != "" {
		add(`application_kind=$%d`, f.Kind)
	}
	if f.RunID != "" {
		add(`run_id=$%d`, f.RunID)
	}
	if f.ResourceID != "" {
		add(`resource_id=$%d`, f.ResourceID)
	}
	if f.DeviceID != "" {
		add(`device_id=$%d`, f.DeviceID)
	}
	if len(f.Statuses) > 0 {
		add(`status=ANY($%d::text[])`, f.Statuses)
	}
	if f.DeviceScopeSet {
		ids := f.DeviceIDs
		if ids == nil {
			ids = []string{}
		}
		add(`cardinality(device_ids)>0 AND device_ids <@ $%d::text[]`, ids)
		add(`(device_id='' OR device_id=ANY($%d::text[]))`, ids)
	}
	var total int
	if err := tx.tx.QueryRow(tx.ctx, `SELECT count(*) FROM analysis_document WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := tx.tx.Query(tx.ctx, `SELECT `+analyticsDocumentColumns+` FROM analysis_document WHERE `+where+fmt.Sprintf(` ORDER BY created_at,id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []analytics.StorageDocument{}
	for rows.Next() {
		d, err := scanAnalytics(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func (r *Repository) CreateAnalysisRun(ctx context.Context, v model.AnalysisRun, limit int) (model.AnalysisRun, error) {
	return r.analysisStore().CreateAnalysisRun(ctx, v, limit)
}
func (r *Repository) GetAnalysisRun(ctx context.Context, tenant, id string) (model.AnalysisRun, error) {
	return r.analysisStore().GetAnalysisRun(ctx, tenant, id)
}
func (r *Repository) ListAnalysisRuns(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisRun, int, error) {
	return r.analysisStore().ListAnalysisRuns(ctx, tenant, f)
}
func (r *Repository) ClaimAnalysisRun(ctx context.Context, worker string, lease time.Duration, kinds []string) (model.AnalysisRun, error) {
	return r.analysisStore().ClaimAnalysisRun(ctx, worker, lease, kinds)
}
func (r *Repository) RenewAnalysisLease(ctx context.Context, tenant, id string, token int64, lease time.Duration) (model.AnalysisRun, error) {
	return r.analysisStore().RenewAnalysisLease(ctx, tenant, id, token, lease)
}
func (r *Repository) CommitAnalysisBatch(ctx context.Context, tenant, id string, token int64, b model.AnalysisBatch) (model.AnalysisRun, error) {
	return r.analysisStore().CommitAnalysisBatch(ctx, tenant, id, token, b)
}
func (r *Repository) StopAnalysisRun(ctx context.Context, tenant, id string, expected int64) (model.AnalysisRun, error) {
	return r.analysisStore().StopAnalysisRun(ctx, tenant, id, expected)
}
func (r *Repository) GetAnalysisSnapshot(ctx context.Context, tenant, id string) (model.AnalysisSnapshot, error) {
	return r.analysisStore().GetAnalysisSnapshot(ctx, tenant, id)
}
func (r *Repository) ListAnalysisEvidence(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisEvidence, int, error) {
	return r.analysisStore().ListAnalysisEvidence(ctx, tenant, f)
}
func (r *Repository) ListAnalysisOutputs(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisOutput, int, error) {
	return r.analysisStore().ListAnalysisOutputs(ctx, tenant, f)
}
func (r *Repository) PutAnalysisConfig(ctx context.Context, v model.AnalysisConfigRevision, expected int64) (model.AnalysisConfigRevision, error) {
	return r.analysisStore().PutAnalysisConfig(ctx, v, expected)
}
func (r *Repository) GetAnalysisConfig(ctx context.Context, tenant, id string) (model.AnalysisConfigRevision, error) {
	return r.analysisStore().GetAnalysisConfig(ctx, tenant, id)
}
func (r *Repository) ListAnalysisConfigs(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error) {
	return r.analysisStore().ListAnalysisConfigs(ctx, tenant, f)
}
func (r *Repository) AppendAnalysisReview(ctx context.Context, v model.AnalysisReview, expected int64) (model.AnalysisReview, error) {
	return r.analysisStore().AppendAnalysisReview(ctx, v, expected)
}
func (r *Repository) ListAnalysisReviews(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisReview, int, error) {
	return r.analysisStore().ListAnalysisReviews(ctx, tenant, f)
}
func (r *Repository) PutAnalysisAIRevision(ctx context.Context, v model.AnalysisAIRevision, expected int64) (model.AnalysisAIRevision, error) {
	return r.analysisStore().PutAnalysisAIRevision(ctx, v, expected)
}
func (r *Repository) GetAnalysisAIRevision(ctx context.Context, tenant, id string) (model.AnalysisAIRevision, error) {
	return r.analysisStore().GetAnalysisAIRevision(ctx, tenant, id)
}
func (r *Repository) ListAnalysisAIRevisions(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error) {
	return r.analysisStore().ListAnalysisAIRevisions(ctx, tenant, f)
}
