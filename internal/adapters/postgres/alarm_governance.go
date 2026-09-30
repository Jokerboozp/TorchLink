package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"sort"
	"strings"
	"time"
)

//go:embed alarm_governance_schema.sql
var alarmGovernanceSchema string
var _ ports.AlarmGovernanceStore = (*Repository)(nil)
var governanceTables = map[string]string{}

func init() {
	for _, k := range []string{model.GovernanceTemplateKind, model.GovernanceSceneKind, model.GovernanceProfileKind, model.GovernanceCaseKind, model.GovernanceRoundKind, model.GovernanceAlarmLinkKind, model.GovernanceVerificationKind, model.GovernanceVerificationLinkKind, model.GovernanceActivityKind, model.GovernanceCoverageKind, model.GovernanceCauseKind, model.GovernanceMeasureKind, model.GovernancePlanKind, model.GovernanceReviewKind, model.GovernanceReportKind, model.GovernanceEventKind, model.GovernanceReceiptKind, model.GovernanceAttachmentKind, model.GovernanceBusinessLinkKind, model.GovernanceReminderKind} {
		governanceTables[k] = "alarm_governance_" + strings.ReplaceAll(k, "-", "_")
	}
}

type governanceTx struct {
	ctx      context.Context
	tx       pgx.Tx
	tenant   string
	readonly bool
}

func (r *Repository) GovernanceTransaction(ctx context.Context, tenant string, fn func(ports.AlarmGovernanceTx) error) error {
	return r.governanceTransaction(ctx, tenant, false, fn)
}
func (r *Repository) GovernanceRead(ctx context.Context, tenant string, fn func(ports.AlarmGovernanceTx) error) error {
	return r.governanceTransaction(ctx, tenant, true, fn)
}
func (r *Repository) governanceTransaction(ctx context.Context, tenant string, read bool, fn func(ports.AlarmGovernanceTx) error) error {
	if tenant == "" {
		return model.ErrGovernanceInvalid
	}
	opts := pgx.TxOptions{IsoLevel: pgx.Serializable}
	if read {
		opts = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	for attempt := 0; attempt < 4; attempt++ {
		tx, err := r.pool.BeginTx(ctx, opts)
		if err != nil {
			return err
		}
		if !read {
			_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,728194609))`, tenant)
		}
		if err == nil {
			err = fn(&governanceTx{ctx, tx, tenant, read})
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err == nil {
			return nil
		}
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "40001" && pe.Code != "40P01" {
			return err
		}
	}
	return model.ErrGovernanceConflict
}

const governanceColumns = `id,tenant_id,version,created_by,created_at,updated_at,coalesce(case_id,''),coalesce(round_id,''),coalesce(parent_id,''),coalesce(resource_id,''),revision_number,status,owner_user_id,device_ids,point_key,occurred_at,coalesce(corrects_id,''),body`

func scanGovernance(row pgx.Row, kind string) (model.GovernanceDocument, error) {
	var d model.GovernanceDocument
	d.Kind = kind
	err := row.Scan(&d.ID, &d.TenantID, &d.Version, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt, &d.CaseID, &d.RoundID, &d.ParentID, &d.ResourceID, &d.RevisionNumber, &d.Status, &d.OwnerUserID, &d.DeviceIDs, &d.PointKey, &d.OccurredAt, &d.CorrectsID, &d.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = model.ErrNotFound
	}
	return d, err
}
func (t *governanceTx) Get(kind, id string) (model.GovernanceDocument, error) {
	table, ok := governanceTables[kind]
	if !ok {
		return model.GovernanceDocument{}, model.ErrGovernanceInvalid
	}
	return scanGovernance(t.tx.QueryRow(t.ctx, `SELECT `+governanceColumns+` FROM `+table+` WHERE tenant_id=$1 AND id=$2`, t.tenant, id), kind)
}
func (t *governanceTx) List(f model.GovernanceFilter) ([]model.GovernanceDocument, int, error) {
	table, ok := governanceTables[f.Kind]
	if !ok {
		return nil, 0, model.ErrGovernanceInvalid
	}
	conds := []string{"tenant_id=$1"}
	args := []any{t.tenant}
	add := func(expr string, v any) { args = append(args, v); conds = append(conds, fmt.Sprintf(expr, len(args))) }
	for _, v := range []struct{ column, value string }{{"case_id", f.CaseID}, {"round_id", f.RoundID}, {"parent_id", f.ParentID}, {"resource_id", f.ResourceID}, {"status", f.Status}, {"owner_user_id", f.OwnerUserID}} {
		if v.value != "" {
			add(v.column+"=$%d", v.value)
		}
	}
	if !f.AllDevices {
		ids := f.DeviceIDs
		if ids == nil {
			ids = []string{}
		}
		add("device_ids <@ $%d::text[]", ids)
		if !slices.Contains([]string{model.GovernanceTemplateKind, model.GovernanceSceneKind, model.GovernanceProfileKind}, f.Kind) {
			conds = append(conds, "cardinality(device_ids)>0")
		}
	}
	if f.Start > 0 {
		add("occurred_at >= $%d", f.Start)
	}
	if f.End > 0 {
		add("occurred_at < $%d", f.End)
	}
	where := strings.Join(conds, " AND ")
	var total int
	if err := t.tx.QueryRow(t.ctx, `SELECT count(*) FROM `+table+` WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := normalizePage(f.Limit, f.Offset)
	args = append(args, limit, offset)
	rows, err := t.tx.Query(t.ctx, `SELECT `+governanceColumns+` FROM `+table+` WHERE `+where+fmt.Sprintf(` ORDER BY updated_at DESC,id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.GovernanceDocument{}
	for rows.Next() {
		d, err := scanGovernance(rows, f.Kind)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}
func (t *governanceTx) Put(d model.GovernanceDocument, expected int64) (model.GovernanceDocument, error) {
	table, ok := governanceTables[d.Kind]
	if !ok || t.readonly || d.ID == "" || !json.Valid(d.Body) || d.TenantID != "" && d.TenantID != t.tenant {
		return d, model.ErrGovernanceInvalid
	}
	if expected > 0 && slices.Contains([]string{model.GovernanceEventKind, model.GovernanceReceiptKind, model.GovernanceReportKind, model.GovernanceAlarmLinkKind, model.GovernanceVerificationLinkKind, model.GovernanceCoverageKind}, d.Kind) {
		return d, model.ErrGovernanceConflict
	}
	if d.DeviceIDs == nil {
		d.DeviceIDs = []string{}
	}
	now := time.Now().UnixMilli()
	args := []any{t.tenant, d.ID, d.CreatedBy, now, d.CaseID, d.RoundID, d.ParentID, d.ResourceID, d.RevisionNumber, d.Status, d.OwnerUserID, d.DeviceIDs, d.PointKey, d.OccurredAt, d.CorrectsID, d.Body}
	var row pgx.Row
	if expected == 0 {
		row = t.tx.QueryRow(t.ctx, `INSERT INTO `+table+`(tenant_id,id,version,created_by,created_at,updated_at,case_id,round_id,parent_id,resource_id,revision_number,status,owner_user_id,device_ids,point_key,occurred_at,corrects_id,body) VALUES($1,$2,1,$3,$4,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),$9,$10,$11,$12,$13,$14,NULLIF($15,''),$16) ON CONFLICT DO NOTHING RETURNING `+governanceColumns, args...)
	} else {
		args = append(args, expected)
		row = t.tx.QueryRow(t.ctx, `UPDATE `+table+` SET version=version+1,updated_at=$4,case_id=NULLIF($5,''),round_id=NULLIF($6,''),parent_id=NULLIF($7,''),resource_id=NULLIF($8,''),revision_number=$9,status=$10,owner_user_id=$11,device_ids=$12,point_key=$13,occurred_at=$14,corrects_id=NULLIF($15,''),body=$16 WHERE tenant_id=$1 AND id=$2 AND version=$17 RETURNING `+governanceColumns, args...)
	}
	out, err := scanGovernance(row, d.Kind)
	if errors.Is(err, model.ErrNotFound) {
		err = model.ErrGovernanceConflict
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && (pe.Code == "23505" || pe.Code == "23503") {
		err = model.ErrGovernanceConflict
	}
	if err == nil && slices.Contains([]string{model.GovernanceTemplateKind, model.GovernanceSceneKind, model.GovernanceProfileKind}, d.Kind) && d.Status == "PUBLISHED" {
		_, err = t.tx.Exec(t.ctx, `INSERT INTO alarm_governance_published_config(tenant_id,kind,resource_id,revision_id) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,kind,resource_id) DO UPDATE SET revision_id=excluded.revision_id,version=alarm_governance_published_config.version+1`, t.tenant, d.Kind, d.ResourceID, d.ID)
	}
	return out, err
}
func governanceVersionOrder(keys []model.GovernanceSourceVersion) []model.GovernanceSourceVersion {
	out := append([]model.GovernanceSourceVersion{}, keys...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].DependencyKey == out[j].DependencyKey {
			return out[i].BucketStart < out[j].BucketStart
		}
		return out[i].DependencyKey < out[j].DependencyKey
	})
	return slices.CompactFunc(out, func(a, b model.GovernanceSourceVersion) bool {
		return a.DependencyKey == b.DependencyKey && a.BucketStart == b.BucketStart
	})
}
func (t *governanceTx) SourceVersions(keys []model.GovernanceSourceVersion) ([]model.GovernanceSourceVersion, error) {
	out := governanceVersionOrder(keys)
	for i, v := range out {
		if !t.readonly {
			lock := fmt.Sprintf("%s\x1f%s\x1f%d", t.tenant, v.DependencyKey, v.BucketStart)
			if _, err := t.tx.Exec(t.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lock); err != nil {
				return nil, err
			}
			if _, err := t.tx.Exec(t.ctx, `INSERT INTO alarm_governance_source_version(tenant_id,dependency_key,bucket_start) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, t.tenant, v.DependencyKey, v.BucketStart); err != nil {
				return nil, err
			}
		}
		err := t.tx.QueryRow(t.ctx, `SELECT generation FROM alarm_governance_source_version WHERE tenant_id=$1 AND dependency_key=$2 AND bucket_start=$3`, t.tenant, v.DependencyKey, v.BucketStart).Scan(&out[i].Generation)
		if errors.Is(err, pgx.ErrNoRows) {
			out[i].Generation = 0
		} else if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (t *governanceTx) BumpSourceVersions(keys []model.GovernanceSourceVersion) error {
	if t.readonly {
		return model.ErrGovernanceInvalid
	}
	versions, err := t.SourceVersions(keys)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if _, err = t.tx.Exec(t.ctx, `UPDATE alarm_governance_source_version SET generation=generation+1 WHERE tenant_id=$1 AND dependency_key=$2 AND bucket_start=$3`, t.tenant, v.DependencyKey, v.BucketStart); err != nil {
			return err
		}
	}
	return nil
}
