package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/externaldata"
)

type externalDataStore struct{ repo *Repository }

var _ externaldata.Store = (*externalDataStore)(nil)

func (r *Repository) ExternalDataStore() externaldata.Store { return &externalDataStore{repo: r} }

const externalEntryColumns = `tenant_id,kind,id,source_id,endpoint_id,status,due_at,lease_until,owner,revision,created_at,updated_at,body`

func scanExternalEntry(row pgx.Row) (externaldata.Entry, error) {
	var e externaldata.Entry
	err := row.Scan(&e.TenantID, &e.Kind, &e.ID, &e.SourceID, &e.EndpointID, &e.Status, &e.DueAt, &e.LeaseUntil, &e.Owner, &e.Revision, &e.CreatedAt, &e.UpdatedAt, &e.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = externaldata.ErrNotFound
	}
	return e, err
}

func validExternalKey(tenant, kind, id string) bool {
	return strings.TrimSpace(tenant) != "" && strings.TrimSpace(kind) != "" && strings.TrimSpace(id) != ""
}

func (s *externalDataStore) Get(ctx context.Context, tenant, kind, id string) (externaldata.Entry, error) {
	if !validExternalKey(tenant, kind, id) {
		return externaldata.Entry{}, externaldata.ErrInvalid
	}
	return scanExternalEntry(s.repo.pool.QueryRow(ctx, `SELECT `+externalEntryColumns+` FROM external_data_entry WHERE tenant_id=$1 AND kind=$2 AND id=$3`, tenant, kind, id))
}

func (s *externalDataStore) List(ctx context.Context, q externaldata.Query) ([]externaldata.Entry, int, error) {
	if strings.TrimSpace(q.TenantID) == "" {
		return nil, 0, externaldata.ErrInvalid
	}
	args := []any{q.TenantID}
	where := ` WHERE tenant_id=$1`
	for _, f := range []struct{ column, value string }{{"kind", q.Kind}, {"source_id", q.SourceID}, {"endpoint_id", q.EndpointID}, {"body->>'jobId'", q.JobID}} {
		if f.value != "" {
			args = append(args, f.value)
			where += fmt.Sprintf(" AND %s=$%d", f.column, len(args))
		}
	}
	if q.Status != "" {
		args = append(args, strings.Split(q.Status, ","))
		where += fmt.Sprintf(" AND status=ANY($%d::text[])", len(args))
	}
	tx, err := s.repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	var total int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM external_data_entry`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := normalizePage(q.Limit, q.Offset)
	args = append(args, limit, offset)
	order := "created_at"
	if q.UpdatedOrder {
		order = "updated_at"
	}
	rows, err := tx.Query(ctx, `SELECT `+externalEntryColumns+` FROM external_data_entry`+where+fmt.Sprintf(` ORDER BY %s DESC,kind,id LIMIT $%d OFFSET $%d`, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	items := []externaldata.Entry{}
	for rows.Next() {
		e, eerr := scanExternalEntry(rows)
		if eerr != nil {
			rows.Close()
			return nil, 0, eerr
		}
		items = append(items, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *externalDataStore) Put(ctx context.Context, e externaldata.Entry, expectedRevision int64) (externaldata.Entry, error) {
	if !validExternalKey(e.TenantID, e.Kind, e.ID) || expectedRevision < 0 || !json.Valid(e.Body) {
		return externaldata.Entry{}, externaldata.ErrInvalid
	}
	now := time.Now().UnixMilli()
	var row pgx.Row
	if expectedRevision == 0 {
		row = s.repo.pool.QueryRow(ctx, `INSERT INTO external_data_entry (`+externalEntryColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,$10,$10,$11) ON CONFLICT(tenant_id,kind,id) DO NOTHING RETURNING `+externalEntryColumns, e.TenantID, e.Kind, e.ID, e.SourceID, e.EndpointID, e.Status, e.DueAt, e.LeaseUntil, e.Owner, now, e.Body)
	} else {
		row = s.repo.pool.QueryRow(ctx, `UPDATE external_data_entry SET source_id=$4,endpoint_id=$5,status=$6,due_at=$7,lease_until=$8,owner=$9,revision=revision+1,updated_at=$10,body=$11 WHERE tenant_id=$1 AND kind=$2 AND id=$3 AND revision=$12 AND (status<>'RUNNING' OR lease_until>$10) RETURNING `+externalEntryColumns, e.TenantID, e.Kind, e.ID, e.SourceID, e.EndpointID, e.Status, e.DueAt, e.LeaseUntil, e.Owner, now, e.Body, expectedRevision)
	}
	result, err := scanExternalEntry(row)
	if errors.Is(err, externaldata.ErrNotFound) {
		err = externaldata.ErrConflict
	}
	return result, err
}

func (s *externalDataStore) Delete(ctx context.Context, tenant, kind, id string, expectedRevision int64) error {
	if !validExternalKey(tenant, kind, id) || expectedRevision <= 0 {
		return externaldata.ErrInvalid
	}
	var matched, deleted bool
	err := s.repo.pool.QueryRow(ctx, `WITH existing AS MATERIALIZED (SELECT revision FROM external_data_entry WHERE tenant_id=$1 AND kind=$2 AND id=$3 FOR UPDATE), removed AS (DELETE FROM external_data_entry WHERE tenant_id=$1 AND kind=$2 AND id=$3 AND revision=$4 RETURNING id) SELECT EXISTS(SELECT 1 FROM existing),EXISTS(SELECT 1 FROM removed)`, tenant, kind, id, expectedRevision).Scan(&matched, &deleted)
	if err != nil {
		return err
	}
	if deleted {
		return nil
	}
	if !matched {
		return externaldata.ErrNotFound
	}
	return externaldata.ErrConflict
}

func (s *externalDataStore) Claim(ctx context.Context, kind, owner string, now, leaseMillis int64) (externaldata.Entry, error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(owner) == "" || leaseMillis <= 0 || now > (1<<63-1)-leaseMillis {
		return externaldata.Entry{}, externaldata.ErrInvalid
	}
	return scanExternalEntry(s.repo.pool.QueryRow(ctx, `WITH candidate AS (SELECT tenant_id,kind,id FROM external_data_entry WHERE kind=$1 AND due_at<=$3 AND (status IN ('PENDING','RETRY') OR (status='RUNNING' AND lease_until<=$3)) ORDER BY due_at,created_at,tenant_id,id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE external_data_entry e SET status='RUNNING',owner=$2,lease_until=$3+$4,revision=e.revision+1,updated_at=$3 FROM candidate c WHERE e.tenant_id=c.tenant_id AND e.kind=c.kind AND e.id=c.id RETURNING e.tenant_id,e.kind,e.id,e.source_id,e.endpoint_id,e.status,e.due_at,e.lease_until,e.owner,e.revision,e.created_at,e.updated_at,e.body`, kind, owner, now, leaseMillis))
}
