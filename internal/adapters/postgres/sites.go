package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
)

// Sites are stored one row per record in site_record (migration 0009) with
// the tenant's revision in site_revision; saves write only changed rows.

const (
	siteUnit     = "unit"
	siteBuilding = "building"
	siteFloor    = "floor"
	sitePoint    = "point"
)

func appendSiteRow(s *model.SiteState, kind string, body []byte) error {
	var err error
	switch kind {
	case siteUnit:
		var v model.SiteUnit
		if err = json.Unmarshal(body, &v); err == nil {
			s.Units = append(s.Units, v)
		}
	case siteBuilding:
		var v model.SiteBuilding
		if err = json.Unmarshal(body, &v); err == nil {
			s.Buildings = append(s.Buildings, v)
		}
	case siteFloor:
		var v model.SiteFloor
		if err = json.Unmarshal(body, &v); err == nil {
			s.Floors = append(s.Floors, v)
		}
	case sitePoint:
		var v model.SitePoint
		if err = json.Unmarshal(body, &v); err == nil {
			s.Points = append(s.Points, v)
		}
	default:
		err = fmt.Errorf("unknown site record kind %q", kind)
	}
	return err
}

func (r *Repository) SiteRevision(ctx context.Context, tenant string) (int64, error) {
	var revision int64
	err := r.pool.QueryRow(ctx, `SELECT revision FROM site_revision WHERE tenant_id=$1`, tenant).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

func (r *Repository) LoadSiteState(ctx context.Context, tenant string) (model.SiteState, error) {
	var state model.SiteState
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
			return err
		}
		var err error
		state, err = loadSites(ctx, tx, tenant, false)
		return err
	})
	return state, err
}

func loadSites(ctx context.Context, tx pgx.Tx, tenant string, forUpdate bool) (model.SiteState, error) {
	var state model.SiteState
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	err := tx.QueryRow(ctx, `SELECT revision FROM site_revision WHERE tenant_id=$1`+lock, tenant).Scan(&state.Revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	rows, err := tx.Query(ctx, `SELECT kind,body FROM site_record WHERE tenant_id=$1 ORDER BY seq`, tenant)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var body []byte
		if err = rows.Scan(&kind, &body); err != nil {
			return state, err
		}
		if err = appendSiteRow(&state, kind, body); err != nil {
			return state, err
		}
	}
	return state, rows.Err()
}

// SaveSiteState writes only the records that differ from base. The
// conditional revision update locks the tenant's sites and rejects a base
// that is no longer current, so base needs no reload.
func (r *Repository) SaveSiteState(ctx context.Context, tenant string, base, next model.SiteState) (bool, error) {
	saved := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO site_revision(tenant_id,revision) VALUES($1,1) ON CONFLICT(tenant_id) DO UPDATE SET revision=site_revision.revision+1 WHERE site_revision.revision=$2`, tenant, base.Revision)
		if err != nil || tag.RowsAffected() != 1 {
			return err
		}
		batch := &pgx.Batch{}
		for _, change := range model.SiteChanges(base, next) {
			if change.Value == nil {
				batch.Queue(`DELETE FROM site_record WHERE tenant_id=$1 AND kind=$2 AND id=$3`, tenant, change.Kind, change.ID)
				continue
			}
			body, err := json.Marshal(change.Value)
			if err != nil {
				return err
			}
			batch.Queue(`INSERT INTO site_record(tenant_id,kind,id,body) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,kind,id) DO UPDATE SET body=excluded.body`, tenant, change.Kind, change.ID, body)
		}
		if batch.Len() > 0 {
			if err = tx.SendBatch(ctx, batch).Close(); err != nil {
				return err
			}
		}
		saved = true
		return nil
	})
	return saved, err
}
