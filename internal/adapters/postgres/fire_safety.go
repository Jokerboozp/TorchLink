package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"iot-platform/internal/model"
)

// Fire safety records are stored one row each in fire_safety_record
// (migration 0007) with the tenant's revision in fire_safety_revision. The
// service keeps validating against the whole tenant state, but a save writes
// only the records that changed instead of rewriting one tenant document,
// and readers can check the revision alone to reuse a cached state.

const (
	fireStation      = "station"
	firePersonnel    = "personnel"
	fireEquipment    = "equipment"
	fireDispatch     = "dispatch"
	fireShift        = "shift"
	fireAssignment   = "assignment"
	fireSwap         = "swap"
	fireExtinguisher = "extinguisher"
	fireInspection   = "inspection"
)

type fireRow struct {
	kind, id string
	body     []byte
}

// fireRows flattens a state in its slice order.
func fireRows(s model.FireSafetyState) ([]fireRow, error) {
	out := []fireRow{}
	add := func(kind, id string, v any) error {
		body, err := json.Marshal(v)
		if err != nil {
			return err
		}
		out = append(out, fireRow{kind, id, body})
		return nil
	}
	for _, v := range s.Stations {
		if err := add(fireStation, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Personnel {
		if err := add(firePersonnel, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Equipment {
		if err := add(fireEquipment, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Dispatches {
		if err := add(fireDispatch, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Shifts {
		if err := add(fireShift, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Assignments {
		if err := add(fireAssignment, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Swaps {
		if err := add(fireSwap, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Extinguishers {
		if err := add(fireExtinguisher, v.ID, v); err != nil {
			return nil, err
		}
	}
	for _, v := range s.Inspections {
		if err := add(fireInspection, v.ID, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func appendFireRow(s *model.FireSafetyState, kind string, body []byte) error {
	decode := func(target any) error { return json.Unmarshal(body, target) }
	switch kind {
	case fireStation:
		var v model.FireStation
		if err := decode(&v); err != nil {
			return err
		}
		s.Stations = append(s.Stations, v)
	case firePersonnel:
		var v model.FirePersonnel
		if err := decode(&v); err != nil {
			return err
		}
		s.Personnel = append(s.Personnel, v)
	case fireEquipment:
		var v model.FireEquipment
		if err := decode(&v); err != nil {
			return err
		}
		s.Equipment = append(s.Equipment, v)
	case fireDispatch:
		var v model.FireDispatch
		if err := decode(&v); err != nil {
			return err
		}
		s.Dispatches = append(s.Dispatches, v)
	case fireShift:
		var v model.DutyShift
		if err := decode(&v); err != nil {
			return err
		}
		s.Shifts = append(s.Shifts, v)
	case fireAssignment:
		var v model.DutyAssignment
		if err := decode(&v); err != nil {
			return err
		}
		s.Assignments = append(s.Assignments, v)
	case fireSwap:
		var v model.DutySwap
		if err := decode(&v); err != nil {
			return err
		}
		s.Swaps = append(s.Swaps, v)
	case fireExtinguisher:
		var v model.Extinguisher
		if err := decode(&v); err != nil {
			return err
		}
		s.Extinguishers = append(s.Extinguishers, v)
	case fireInspection:
		var v model.FireInspection
		if err := decode(&v); err != nil {
			return err
		}
		s.Inspections = append(s.Inspections, v)
	default:
		return fmt.Errorf("unknown fire safety record kind %q", kind)
	}
	return nil
}

// FireSafetyRevision returns the tenant's revision (0 before any save).
func (r *Repository) FireSafetyRevision(ctx context.Context, tenant string) (int64, error) {
	var revision int64
	err := r.pool.QueryRow(ctx, `SELECT revision FROM fire_safety_revision WHERE tenant_id=$1`, tenant).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

func (r *Repository) LoadFireSafetyState(ctx context.Context, tenant string) (model.FireSafetyState, error) {
	var state model.FireSafetyState
	if err := ctx.Err(); err != nil {
		return state, err
	}
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
			return err
		}
		var err error
		state, err = loadFireSafety(ctx, tx, tenant, false)
		return err
	})
	return state, err
}

func loadFireSafety(ctx context.Context, tx pgx.Tx, tenant string, forUpdate bool) (model.FireSafetyState, error) {
	var state model.FireSafetyState
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	err := tx.QueryRow(ctx, `SELECT revision FROM fire_safety_revision WHERE tenant_id=$1`+lock, tenant).Scan(&state.Revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	rows, err := tx.Query(ctx, `SELECT kind,body FROM fire_safety_record WHERE tenant_id=$1 ORDER BY seq`, tenant)
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
		if err = appendFireRow(&state, kind, body); err != nil {
			return state, err
		}
	}
	return state, rows.Err()
}

func (r *Repository) SaveFireSafetyState(ctx context.Context, tenant string, state model.FireSafetyState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	saved := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		current, err := loadFireSafety(ctx, tx, tenant, true)
		if err != nil {
			return err
		}
		if current.Revision != state.Revision {
			return nil
		}
		if err = saveFireSafety(ctx, tx, tenant, current, state); err != nil {
			return err
		}
		saved = true
		return nil
	})
	if errors.Is(err, errFireSafetyChanged) {
		return false, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23P01" && pgErr.ConstraintName == "duty_personnel_no_overlap" {
		return false, model.ErrDutyOverlap
	}
	return saved, err
}

// errFireSafetyChanged reports that another writer created the first
// revision concurrently; Save reports it as an ordinary conflict.
var errFireSafetyChanged = errors.New("fire safety state changed concurrently")

// saveFireSafety writes only records that were added, changed or removed.
func saveFireSafety(ctx context.Context, tx pgx.Tx, tenant string, current, next model.FireSafetyState) error {
	tag, err := tx.Exec(ctx, `INSERT INTO fire_safety_revision(tenant_id,revision) VALUES($1,1) ON CONFLICT(tenant_id) DO UPDATE SET revision=fire_safety_revision.revision+1 WHERE fire_safety_revision.revision=$2`, tenant, current.Revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errFireSafetyChanged
	}
	before, err := fireRows(current)
	if err != nil {
		return err
	}
	after, err := fireRows(next)
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
		if exists && string(previous) == string(row.body) {
			continue
		}
		batch.Queue(`INSERT INTO fire_safety_record(tenant_id,kind,id,body) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,kind,id) DO UPDATE SET body=excluded.body`, tenant, row.kind, row.id, row.body)
	}
	for key := range old {
		kind, id, _ := cutKey(key)
		batch.Queue(`DELETE FROM fire_safety_record WHERE tenant_id=$1 AND kind=$2 AND id=$3`, tenant, kind, id)
	}
	if batch.Len() == 0 {
		return nil
	}
	return tx.SendBatch(ctx, batch).Close()
}

func cutKey(key string) (string, string, bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return key[:i], key[i+1:], true
		}
	}
	return key, "", false
}

// migrateFireSafetyDocuments moves every tenant's legacy document into
// fire_safety_record and keeps it as platform_fire_safety_legacy.
func migrateFireSafetyDocuments(ctx context.Context, tx pgx.Tx) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('platform_fire_safety') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT tenant_id,revision,body FROM platform_fire_safety ORDER BY tenant_id`)
	if err != nil {
		return err
	}
	type doc struct {
		tenant   string
		revision int64
		body     []byte
	}
	docs := []doc{}
	for rows.Next() {
		var d doc
		if err = rows.Scan(&d.tenant, &d.revision, &d.body); err != nil {
			rows.Close()
			return err
		}
		docs = append(docs, d)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, d := range docs {
		var state model.FireSafetyState
		if err = json.Unmarshal(d.body, &state); err != nil {
			return fmt.Errorf("tenant %s: %w", d.tenant, err)
		}
		if err = saveFireSafety(ctx, tx, d.tenant, model.FireSafetyState{}, state); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE fire_safety_revision SET revision=GREATEST($2,1) WHERE tenant_id=$1`, d.tenant, d.revision); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `ALTER TABLE platform_fire_safety RENAME TO platform_fire_safety_legacy`)
	return err
}

func init() { registerMigration(8, "fire_safety_documents_to_records", migrateFireSafetyDocuments) }
