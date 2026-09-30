package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.DutyStore = (*Repository)(nil)
var dutyTables = map[string]string{model.DutyReceiptKind: "duty_receipt", model.DutyAttachmentKind: "duty_attachment", model.DutyStationKind: "duty_station", model.DutyTeamKind: "duty_team", model.DutyShiftTemplateKind: "duty_shift_template", model.DutyRosterKind: "duty_roster", model.DutyRunKind: "duty_run", model.DutyRecordKind: "duty_record", model.DutyItemKind: "duty_item", model.DutyItemEventKind: "duty_item_event", model.DutyHandoverKind: "duty_handover", model.DutyRevisionKind: "duty_handover_revision", model.DutyAIJobKind: "duty_ai_job", model.DutyNotificationKind: "duty_notification", model.DutyActionLinkKind: "duty_action_link"}

type dutyTx struct {
	ctx      context.Context
	tx       pgx.Tx
	tenant   string
	readonly bool
}

func (r *Repository) DutyTransaction(ctx context.Context, tenant string, fn func(ports.DutyTx) error) error {
	return r.dutyTransaction(ctx, tenant, false, fn)
}
func (r *Repository) DutyRead(ctx context.Context, tenant string, fn func(ports.DutyTx) error) error {
	return r.dutyTransaction(ctx, tenant, true, fn)
}
func (r *Repository) dutyTransaction(ctx context.Context, tenant string, readonly bool, fn func(ports.DutyTx) error) error {
	if tenant == "" {
		return errors.New("duty tenant required")
	}
	opts := pgx.TxOptions{IsoLevel: pgx.Serializable}
	if readonly {
		opts = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	for attempt := 0; attempt < 4; attempt++ {
		tx, err := r.pool.BeginTx(ctx, opts)
		if err != nil {
			return err
		}
		if !readonly {
			_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,728194603))`, tenant)
		}
		if err == nil {
			err = fn(&dutyTx{ctx: ctx, tx: tx, tenant: tenant, readonly: readonly})
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
		if !errors.As(err, &pe) || (pe.Code != "40001" && pe.Code != "40P01") {
			return err
		}
		if attempt == 3 {
			return model.ErrDutyConflict
		}
	}
	return model.ErrDutyConflict
}
func dutyTable(kind string) (string, error) {
	table, ok := dutyTables[kind]
	if !ok {
		return "", errors.New("invalid duty kind")
	}
	return table, nil
}
func scanDuty(row pgx.Row, kind string) (model.DutyDocument, error) {
	var d model.DutyDocument
	d.Kind = kind
	err := row.Scan(&d.ID, &d.TenantID, &d.Version, &d.CreatedAt, &d.UpdatedAt, &d.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		err = model.ErrNotFound
	}
	return d, err
}
func (t *dutyTx) Get(kind, id string) (model.DutyDocument, error) {
	table, err := dutyTable(kind)
	if err != nil {
		return model.DutyDocument{}, err
	}
	return scanDuty(t.tx.QueryRow(t.ctx, `SELECT id,tenant_id,version,created_at,updated_at,body FROM `+table+` WHERE tenant_id=$1 AND id=$2`, t.tenant, id), kind)
}
func (t *dutyTx) Put(d model.DutyDocument, expected int64) (model.DutyDocument, error) {
	if t.readonly {
		return d, model.ErrDutyReadOnly
	}
	table, err := dutyTable(d.Kind)
	if err != nil {
		return d, err
	}
	if d.ID == "" || !json.Valid(d.Body) {
		return d, errors.New("invalid duty document")
	}
	if d.TenantID != "" && d.TenantID != t.tenant {
		return d, errors.New("duty tenant mismatch")
	}
	now := time.Now().UnixMilli()
	var row pgx.Row
	if expected == 0 {
		row = t.tx.QueryRow(t.ctx, `INSERT INTO `+table+`(tenant_id,id,version,created_at,updated_at,body) VALUES($1,$2,1,$3,$3,$4) ON CONFLICT DO NOTHING RETURNING id,tenant_id,version,created_at,updated_at,body`, t.tenant, d.ID, now, d.Body)
	} else {
		if slices.Contains([]string{model.DutyReceiptKind, model.DutyAttachmentKind, model.DutyRevisionKind, model.DutyItemEventKind, model.DutyRecordKind}, d.Kind) {
			return d, errors.New("immutable duty document")
		}
		row = t.tx.QueryRow(t.ctx, `UPDATE `+table+` SET version=version+1,updated_at=$3,body=$4 WHERE tenant_id=$1 AND id=$2 AND version=$5 RETURNING id,tenant_id,version,created_at,updated_at,body`, t.tenant, d.ID, now, d.Body, expected)
	}
	out, err := scanDuty(row, d.Kind)
	if errors.Is(err, model.ErrNotFound) {
		err = model.ErrDutyConflict
	}
	return out, err
}
func (t *dutyTx) Delete(kind, id string, expected int64) error {
	if t.readonly {
		return model.ErrDutyReadOnly
	}
	table, err := dutyTable(kind)
	if err != nil {
		return err
	}
	if slices.Contains([]string{model.DutyReceiptKind, model.DutyAttachmentKind, model.DutyRevisionKind, model.DutyItemEventKind, model.DutyRecordKind}, kind) {
		return errors.New("immutable duty document")
	}
	tag, err := t.tx.Exec(t.ctx, `DELETE FROM `+table+` WHERE tenant_id=$1 AND id=$2 AND version=$3`, t.tenant, id, expected)
	if err == nil && tag.RowsAffected() == 0 {
		return model.ErrDutyConflict
	}
	return err
}
func dutyDocumentWhere(tenant string, f model.DutyFilter) (string, []any) {
	conds := []string{"tenant_id=$1"}
	args := []any{tenant}
	add := func(expr string, value any) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf(expr, len(args)))
	}
	for _, entry := range []struct{ key, value string }{{"stationId", f.StationID}, {"runId", f.RunID}, {"handoverId", f.HandoverID}, {"status", f.Status}} {
		if entry.value != "" {
			add("body->>'"+entry.key+"'=$%d", entry.value)
		}
	}
	if f.UserID != "" {
		args = append(args, f.UserID)
		n := len(args)
		conds = append(conds, fmt.Sprintf(`(body->>'userId'=$%d OR body->>'ownerId'=$%d OR body->>'authorId'=$%d OR body->>'requesterId'=$%d OR body->>'leaderId'=$%d OR body->>'supervisorId'=$%d OR COALESCE(body->'memberIds','[]'::jsonb) ? $%d)`, n, n, n, n, n, n, n))
	}
	at := `COALESCE(NULLIF(body->>'occurredAt','')::bigint,NULLIF(body->>'startAt','')::bigint,created_at)`
	if f.Start != 0 {
		add(at+">=$%d", f.Start)
	}
	if f.End != 0 {
		add(at+"<$%d", f.End)
	}
	if f.DeviceIDs != nil {
		scope, _ := json.Marshal(f.DeviceIDs)
		args = append(args, scope)
		n := len(args)
		conds = append(conds, fmt.Sprintf(`((COALESCE(body->>'deviceId','')='' OR $%d::jsonb ? (body->>'deviceId')) AND COALESCE(body->'deviceIds','[]'::jsonb) <@ $%d::jsonb)`, n, n))
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}
func (t *dutyTx) List(f model.DutyFilter) ([]model.DutyDocument, int, error) {
	sources := []string{}
	if f.Kind != "" {
		table, err := dutyTable(f.Kind)
		if err != nil {
			return nil, 0, err
		}
		sources = append(sources, `SELECT '`+f.Kind+`'::text AS kind,* FROM `+table)
	} else {
		keys := []string{}
		for k := range dutyTables {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			sources = append(sources, `SELECT '`+k+`'::text AS kind,* FROM `+dutyTables[k])
		}
	}
	source := "(" + strings.Join(sources, " UNION ALL ") + ") AS d"
	where, args := dutyDocumentWhere(t.tenant, f)
	var total int
	if err := t.tx.QueryRow(t.ctx, `SELECT count(*) FROM `+source+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := normalizePage(f.Limit, f.Offset)
	args = append(args, limit, offset)
	rows, err := t.tx.Query(t.ctx, fmt.Sprintf(`SELECT kind,id,tenant_id,version,created_at,updated_at,body FROM %s%s ORDER BY updated_at DESC,id DESC LIMIT $%d OFFSET $%d`, source, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.DutyDocument{}
	for rows.Next() {
		var d model.DutyDocument
		if err = rows.Scan(&d.Kind, &d.ID, &d.TenantID, &d.Version, &d.CreatedAt, &d.UpdatedAt, &d.Body); err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}
func (t *dutyTx) AppendEvent(e model.DutyBusinessEvent) error {
	if t.readonly {
		return model.ErrDutyReadOnly
	}
	if e.TenantID != "" && e.TenantID != t.tenant {
		return errors.New("duty tenant mismatch")
	}
	e.TenantID = t.tenant
	return insertDutyEvent(t.ctx, t.tx, e)
}
func insertDutyEvent(ctx context.Context, tx pgx.Tx, e model.DutyBusinessEvent) error {
	if e.ID == "" || e.Type == "" || e.TenantID == "" {
		return errors.New("invalid duty event")
	}
	if e.RecordedAt == 0 {
		e.RecordedAt = time.Now().UnixMilli()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO duty_business_event(tenant_id,id,event_type,station_id,run_id,device_id,actor_id,occurred_at,recorded_at,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(tenant_id,id) DO NOTHING`, e.TenantID, e.ID, e.Type, e.StationID, e.RunID, e.DeviceID, e.ActorID, e.OccurredAt, e.RecordedAt, b)
	return err
}
func dutyEventWhere(tenant string, f model.DutyFilter) (string, []any) {
	conds := []string{"tenant_id=$1"}
	args := []any{tenant}
	add := func(expr string, value any) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf(expr, len(args)))
	}
	for _, entry := range []struct{ key, value string }{{"event_type", f.Kind}, {"station_id", f.StationID}, {"run_id", f.RunID}, {"actor_id", f.UserID}} {
		if entry.value != "" {
			add(entry.key+"=$%d", entry.value)
		}
	}
	if f.Start != 0 {
		add("occurred_at >= $%d", f.Start)
	}
	if f.End != 0 {
		add("occurred_at < $%d", f.End)
	}
	if f.DeviceIDs != nil {
		add("device_id=ANY($%d::text[])", f.DeviceIDs)
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}
func (t *dutyTx) Events(f model.DutyFilter) ([]model.DutyBusinessEvent, int, error) {
	where, args := dutyEventWhere(t.tenant, f)
	var total int
	if err := t.tx.QueryRow(t.ctx, `SELECT count(*) FROM duty_business_event`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit, offset := normalizePage(f.Limit, f.Offset)
	args = append(args, limit, offset)
	rows, err := t.tx.Query(t.ctx, fmt.Sprintf(`SELECT seq,body FROM duty_business_event%s ORDER BY occurred_at DESC,seq DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.DutyBusinessEvent{}
	for rows.Next() {
		var e model.DutyBusinessEvent
		var b []byte
		var seq int64
		if err = rows.Scan(&seq, &b); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(b, &e); err != nil {
			return nil, 0, err
		}
		e.Seq = seq
		out = append(out, e)
	}
	return out, total, rows.Err()
}
func (t *dutyTx) Snapshot(ids []string) (model.DutySnapshot, error) {
	s := model.DutySnapshot{CutoffAt: time.Now().UnixMilli(), DeviceIDs: append([]string{}, ids...), Devices: []model.ManagedDevice{}, States: []model.DeviceState{}, Alarms: []model.Alarm{}}
	if err := t.tx.QueryRow(t.ctx, `SELECT COALESCE(max(seq),0) FROM duty_business_event WHERE tenant_id=$1`, t.tenant).Scan(&s.EventSeq); err != nil {
		return s, err
	}
	args := []any{t.tenant}
	scope := ""
	if ids != nil {
		args = append(args, ids)
		scope = " AND id=ANY($2::text[])"
	}
	rows, err := t.tx.Query(t.ctx, `SELECT body FROM device_registry WHERE tenant_id=$1`+scope+` ORDER BY id`, args...)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var d model.ManagedDevice
		var b []byte
		if err = rows.Scan(&b); err == nil {
			err = json.Unmarshal(b, &d)
		}
		if err != nil {
			rows.Close()
			return s, err
		}
		s.Devices = append(s.Devices, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	scope = strings.ReplaceAll(scope, "id=ANY", "device_id=ANY")
	rows, err = t.tx.Query(t.ctx, `SELECT body FROM device_state WHERE tenant_id=$1`+scope+` ORDER BY device_id`, args...)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var d model.DeviceState
		var b []byte
		if err = rows.Scan(&b); err == nil {
			err = json.Unmarshal(b, &d)
		}
		if err != nil {
			rows.Close()
			return s, err
		}
		s.States = append(s.States, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return s, err
	}
	rows, err = t.tx.Query(t.ctx, `SELECT body FROM alarm_record WHERE tenant_id=$1 AND status IN ('ACTIVE','ACKED')`+scope+` ORDER BY id`, args...)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var a model.Alarm
		var b []byte
		if err = rows.Scan(&b); err == nil {
			err = json.Unmarshal(b, &a)
		}
		if err != nil {
			rows.Close()
			return s, err
		}
		s.Alarms = append(s.Alarms, a)
	}
	err = rows.Err()
	rows.Close()
	return s, err
}

func (r *Repository) DutyTenants(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT tenant_id FROM duty_station UNION SELECT tenant_id FROM duty_ai_job ORDER BY tenant_id`)
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

func (t *dutyTx) Alarm(id string) (model.Alarm, error) {
	var v model.Alarm
	var body []byte
	var version int64
	err := t.tx.QueryRow(t.ctx, `SELECT body,version FROM alarm_record WHERE tenant_id=$1 AND id=$2`, t.tenant, id).Scan(&body, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(body, &v)
		v.Version = version
	}
	return v, err
}
