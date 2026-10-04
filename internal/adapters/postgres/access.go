package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"iot-platform/internal/model"
)

// Access control is stored relationally (migration 0005): one row per user,
// role and API key, device grants in access_device_grant, and the tenant's
// revision in platform_access_revision. Every change goes through
// SaveAccessState in one transaction that checks and increments the
// revision, so callers keep the optimistic whole-state contract while reads
// can check the revision alone to reuse a cached state.

type accessQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// AccessRevision returns the tenant's access revision (0 before any save).
func (r *Repository) AccessRevision(ctx context.Context, tenant string) (int64, error) {
	var revision int64
	err := r.pool.QueryRow(ctx, `SELECT revision FROM platform_access_revision WHERE tenant_id=$1`, tenant).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return revision, err
}

func (r *Repository) LoadAccessState(ctx context.Context, tenant string) (model.AccessState, error) {
	var state model.AccessState
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Repeatable read gives one consistent snapshot across the tables.
		if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
			return err
		}
		var err error
		state, err = loadAccess(ctx, tx, tenant, false)
		return err
	})
	return state, err
}

func loadAccess(ctx context.Context, q accessQuerier, tenant string, forUpdate bool) (model.AccessState, error) {
	var state model.AccessState
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	err := q.QueryRow(ctx, `SELECT revision FROM platform_access_revision WHERE tenant_id=$1`+lock, tenant).Scan(&state.Revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return state, err
	}
	grants := map[string][]string{}
	rows, err := q.Query(ctx, `SELECT subject_kind,subject_id,device_id FROM access_device_grant WHERE tenant_id=$1 ORDER BY subject_kind,subject_id,position`, tenant)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var kind, subject, device string
		if err = rows.Scan(&kind, &subject, &device); err != nil {
			rows.Close()
			return state, err
		}
		grants[kind+"\x00"+subject] = append(grants[kind+"\x00"+subject], device)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return state, err
	}
	decode := func(sql string, each func([]byte) error) error {
		rows, err := q.Query(ctx, sql, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var body []byte
			if err = rows.Scan(&body); err != nil {
				return err
			}
			if err = each(body); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err = decode(`SELECT body FROM platform_user WHERE tenant_id=$1 ORDER BY position`, func(b []byte) error {
		var u model.PlatformUser
		if err := json.Unmarshal(b, &u); err != nil {
			return err
		}
		u.DeviceIDs = nonNil(grants["user\x00"+u.Username])
		state.Users = append(state.Users, u)
		return nil
	}); err != nil {
		return state, err
	}
	if err = decode(`SELECT body FROM platform_role WHERE tenant_id=$1 ORDER BY position`, func(b []byte) error {
		var role model.PlatformRole
		if err := json.Unmarshal(b, &role); err != nil {
			return err
		}
		role.DeviceIDs = nonNil(grants["role\x00"+role.ID])
		state.Roles = append(state.Roles, role)
		return nil
	}); err != nil {
		return state, err
	}
	err = decode(`SELECT body FROM platform_api_key WHERE tenant_id=$1 ORDER BY position`, func(b []byte) error {
		var key model.APIKey
		if err := json.Unmarshal(b, &key); err != nil {
			return err
		}
		state.APIKeys = append(state.APIKeys, key)
		return nil
	})
	return state, err
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func (r *Repository) SaveAccessState(ctx context.Context, tenant string, state model.AccessState) (bool, error) {
	saved := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		current, err := loadAccess(ctx, tx, tenant, true)
		if err != nil {
			return err
		}
		if current.Revision != state.Revision {
			return nil
		}
		if err = saveAccess(ctx, tx, tenant, current, state); err != nil {
			return err
		}
		saved = true
		return nil
	})
	if errors.Is(err, errAccessChanged) {
		return false, nil
	}
	return saved, err
}

// saveAccess writes the difference between current and next and increments
// the revision. The caller holds the revision row lock (loadAccess with
// forUpdate) or inserts the first revision.
func saveAccess(ctx context.Context, tx pgx.Tx, tenant string, current, next model.AccessState) error {
	tag, err := tx.Exec(ctx, `INSERT INTO platform_access_revision(tenant_id,revision) VALUES($1,1) ON CONFLICT(tenant_id) DO UPDATE SET revision=platform_access_revision.revision+1 WHERE platform_access_revision.revision=$2`, tenant, current.Revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errAccessChanged
	}
	type row struct {
		id      string
		body    []byte
		devices []string
	}
	sync := func(table, kind, idColumn string, old, desired []row) error {
		oldBodies := map[string][]byte{}
		oldDevices := map[string][]string{}
		for _, r := range old {
			oldBodies[r.id], oldDevices[r.id] = r.body, r.devices
		}
		keep := map[string]bool{}
		for position, r := range desired {
			keep[r.id] = true
			if previous, ok := oldBodies[r.id]; !ok || string(previous) != string(r.body) {
				if _, err := tx.Exec(ctx, `INSERT INTO `+table+`(tenant_id,`+idColumn+`,position,body) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,`+idColumn+`) DO UPDATE SET position=excluded.position,body=excluded.body`, tenant, r.id, position, r.body); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `UPDATE `+table+` SET position=$3 WHERE tenant_id=$1 AND `+idColumn+`=$2 AND position<>$3`, tenant, r.id, position); err != nil {
				return err
			}
			if kind != "" && !slices.Equal(oldDevices[r.id], r.devices) {
				if _, err := tx.Exec(ctx, `DELETE FROM access_device_grant WHERE tenant_id=$1 AND subject_kind=$2 AND subject_id=$3`, tenant, kind, r.id); err != nil {
					return err
				}
				if len(r.devices) > 0 {
					positions := make([]int32, len(r.devices))
					for i := range positions {
						positions[i] = int32(i)
					}
					if _, err := tx.Exec(ctx, `INSERT INTO access_device_grant(tenant_id,subject_kind,subject_id,device_id,position) SELECT $1,$2,$3,d,p FROM unnest($4::text[],$5::int[]) AS t(d,p) ON CONFLICT DO NOTHING`, tenant, kind, r.id, r.devices, positions); err != nil {
						return err
					}
				}
			}
		}
		for id := range oldBodies {
			if keep[id] {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2`, tenant, id); err != nil {
				return err
			}
			if kind != "" {
				if _, err := tx.Exec(ctx, `DELETE FROM access_device_grant WHERE tenant_id=$1 AND subject_kind=$2 AND subject_id=$3`, tenant, kind, id); err != nil {
					return err
				}
			}
		}
		return nil
	}
	users := func(list []model.PlatformUser) []row {
		out := make([]row, 0, len(list))
		for _, u := range list {
			devices := u.DeviceIDs
			u.DeviceIDs = nil
			body, _ := json.Marshal(u)
			out = append(out, row{u.Username, body, nonNil(devices)})
		}
		return out
	}
	roles := func(list []model.PlatformRole) []row {
		out := make([]row, 0, len(list))
		for _, role := range list {
			devices := role.DeviceIDs
			role.DeviceIDs = nil
			body, _ := json.Marshal(role)
			out = append(out, row{role.ID, body, nonNil(devices)})
		}
		return out
	}
	keys := func(list []model.APIKey) []row {
		out := make([]row, 0, len(list))
		for _, key := range list {
			body, _ := json.Marshal(key)
			out = append(out, row{key.ID, body, nil})
		}
		return out
	}
	if err = sync("platform_user", "user", "username", users(current.Users), users(next.Users)); err != nil {
		return err
	}
	if err = sync("platform_role", "role", "id", roles(current.Roles), roles(next.Roles)); err != nil {
		return err
	}
	return sync("platform_api_key", "", "id", keys(current.APIKeys), keys(next.APIKeys))
}

var errAccessChanged = errors.New("access state changed concurrently")

// pruneAccessDevices removes deleted devices from every grant inside the
// caller's transaction, ending the sessions whose effective scope shrank
// (see model.PruneCapacityAccessReferences). It returns the removed grants.
func pruneAccessDevices(ctx context.Context, tx pgx.Tx, tenant string, devices []string) (int64, error) {
	current, err := loadAccess(ctx, tx, tenant, true)
	if err != nil || current.Revision == 0 {
		return 0, err
	}
	body, err := json.Marshal(current)
	if err != nil {
		return 0, err
	}
	pruned, refs, err := model.PruneCapacityAccessReferences(body, devices)
	if err != nil || refs == 0 {
		return 0, err
	}
	var next model.AccessState
	if err = json.Unmarshal(pruned, &next); err != nil {
		return 0, err
	}
	next.Revision = current.Revision
	return refs, saveAccess(ctx, tx, tenant, current, next)
}

// migrateAccessDocuments moves every tenant's legacy JSON access document
// into the relational tables and keeps the document as platform_access_legacy
// for one release in case of rollback.
func migrateAccessDocuments(ctx context.Context, tx pgx.Tx) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('platform_access') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT tenant_id,revision,body FROM platform_access`)
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
	sort.Slice(docs, func(i, j int) bool { return docs[i].tenant < docs[j].tenant })
	for _, d := range docs {
		var state model.AccessState
		if err = json.Unmarshal(d.body, &state); err != nil {
			return err
		}
		if err = saveAccess(ctx, tx, d.tenant, model.AccessState{}, state); err != nil {
			return err
		}
		// Keep the tenant's revision so outstanding AI access versions and
		// optimistic edits stay comparable.
		if _, err = tx.Exec(ctx, `UPDATE platform_access_revision SET revision=GREATEST($2,1) WHERE tenant_id=$1`, d.tenant, d.revision); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `ALTER TABLE platform_access RENAME TO platform_access_legacy`)
	return err
}

func init() { registerMigration(6, "access_documents_to_tables", migrateAccessDocuments) }
