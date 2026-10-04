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

func siteRows(s model.SiteState) ([]fireRow, error) {
	out := []fireRow{}
	add := func(kind, id string, v any) error {
		body, err := json.Marshal(v)
		if err == nil {
			out = append(out, fireRow{kind, id, body})
		}
		return err
	}
	for _, v := range s.Units {
		if err := add(siteUnit, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Buildings {
		if err := add(siteBuilding, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Floors {
		if err := add(siteFloor, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Points {
		if err := add(sitePoint, v.ID, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

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

var errSitesChanged = errors.New("site state changed concurrently")

func (r *Repository) SaveSiteState(ctx context.Context, tenant string, state model.SiteState) (bool, error) {
	saved := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		current, err := loadSites(ctx, tx, tenant, true)
		if err != nil || current.Revision != state.Revision {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO site_revision(tenant_id,revision) VALUES($1,1) ON CONFLICT(tenant_id) DO UPDATE SET revision=site_revision.revision+1 WHERE site_revision.revision=$2`, tenant, current.Revision)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errSitesChanged
		}
		before, err := siteRows(current)
		if err != nil {
			return err
		}
		after, err := siteRows(state)
		if err != nil {
			return err
		}
		old := make(map[string][]byte, len(before))
		for _, row := range before {
			old[row.kind+"\x00"+row.id] = row.body
		}
		batch := &pgx.Batch{}
		for _, row := range after {
			key := row.kind + "\x00" + row.id
			previous, exists := old[key]
			delete(old, key)
			if !exists || string(previous) != string(row.body) {
				batch.Queue(`INSERT INTO site_record(tenant_id,kind,id,body) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,kind,id) DO UPDATE SET body=excluded.body`, tenant, row.kind, row.id, row.body)
			}
		}
		for key := range old {
			kind, id, _ := cutKey(key)
			batch.Queue(`DELETE FROM site_record WHERE tenant_id=$1 AND kind=$2 AND id=$3`, tenant, kind, id)
		}
		if batch.Len() > 0 {
			if err = tx.SendBatch(ctx, batch).Close(); err != nil {
				return err
			}
		}
		saved = true
		return nil
	})
	if errors.Is(err, errSitesChanged) {
		return false, nil
	}
	return saved, err
}
