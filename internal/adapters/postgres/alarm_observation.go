package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

//go:embed alarm_observation_schema.sql
var alarmObservationSchema string

func recordAlarmObservation(ctx context.Context, tx pgx.Tx, o model.AlarmObservation) (model.AlarmObservation, bool, error) {
	if err := o.Normalize(); err != nil {
		return o, false, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO alarm_observation_input(tenant_id,input_key,content_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, o.TenantID, o.InputKey(), o.SourceInputHash)
	if err != nil {
		return o, false, err
	}
	var inputHash string
	if err = tx.QueryRow(ctx, `SELECT content_hash FROM alarm_observation_input WHERE tenant_id=$1 AND input_key=$2 FOR UPDATE`, o.TenantID, o.InputKey()).Scan(&inputHash); err != nil {
		return o, false, err
	}
	var existing model.AlarmObservation
	var b []byte
	err = tx.QueryRow(ctx, `SELECT body FROM alarm_observation WHERE tenant_id=$1 AND slot_key=$2`, o.TenantID, o.SlotKey()).Scan(&b)
	if err == nil {
		if err = json.Unmarshal(b, &existing); err != nil {
			return o, false, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return o, false, err
	}
	conflictReason := ""
	if inputHash != o.SourceInputHash {
		conflictReason = "SOURCE_INPUT_CONFLICT"
	} else if existing.ID != "" && existing.SourceContentHash != o.SourceContentHash {
		conflictReason = "SOURCE_SLOT_CONFLICT"
	}
	if conflictReason != "" {
		c := model.AlarmObservationConflict{ID: "aoc_" + model.ObservationHash([]string{o.ID, o.SourceInputHash, o.SourceContentHash})[:32], TenantID: o.TenantID, ObservationID: existing.ID, Incoming: o, Reason: conflictReason, RecordedAt: o.RecordedAt}
		body, _ := json.Marshal(c)
		tag, e := tx.Exec(ctx, `INSERT INTO alarm_observation_conflict(tenant_id,id,observation_id,recorded_at,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, o.TenantID, c.ID, c.ObservationID, c.RecordedAt, body)
		if e == nil && tag.RowsAffected() > 0 {
			originals := []model.AlarmObservation{o}
			rows, queryErr := tx.Query(ctx, `SELECT body FROM alarm_observation WHERE tenant_id=$1 AND body->>'sourceSystem'=$2 AND body->>'sourceEventId'=$3`, o.TenantID, o.SourceSystem, o.SourceEventID)
			if queryErr != nil {
				return o, false, queryErr
			}
			for rows.Next() {
				var original model.AlarmObservation
				var body []byte
				if queryErr = rows.Scan(&body); queryErr != nil {
					break
				}
				if queryErr = json.Unmarshal(body, &original); queryErr != nil {
					break
				}
				originals = append(originals, original)
			}
			if queryErr == nil {
				queryErr = rows.Err()
			}
			rows.Close()
			if queryErr != nil {
				return o, false, queryErr
			}
			e = bumpAlarmObservationSources(ctx, tx, originals, true)
		}
		o.Acceptance = "CONFLICT"
		o.Reason = conflictReason
		return o, false, e
	}
	if existing.ID != "" {
		attempt := model.AlarmObservationAttempt{ObservationID: existing.ID, Acceptance: o.Acceptance, Reason: o.Reason, AlarmID: o.AlarmID, EvaluationAt: o.EvaluationAt, RecordedAt: o.RecordedAt}
		body, _ := json.Marshal(attempt)
		_, e := tx.Exec(ctx, `INSERT INTO alarm_observation_attempt(tenant_id,observation_id,recorded_at,body) VALUES($1,$2,$3,$4)`, o.TenantID, existing.ID, o.RecordedAt, body)
		return existing, false, e
	}
	// No committed-time receipt exists at this point. Keep AvailableAt unknown;
	// an assigned transaction timestamp is not successful persistence evidence.
	b, err = json.Marshal(o)
	if err != nil {
		return o, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alarm_observation(tenant_id,id,slot_key,device_id,component_id,alarm_type,origin_kind,signal_key,event_at,recorded_at,acceptance,fact_kind,alarm_id,content_hash,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, o.TenantID, o.ID, o.SlotKey(), o.DeviceID, o.ComponentID, o.AlarmType, o.OriginKind, o.SignalKey, o.EventAt, o.RecordedAt, o.Acceptance, o.FactKind, o.AlarmID, o.SourceContentHash, b)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO alarm_observation_collection(tenant_id,device_id,source_system,collection_started_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, o.TenantID, o.DeviceID, o.SourceSystem, o.RecordedAt)
	}
	if err == nil {
		err = bumpAlarmObservationSource(ctx, tx, o, false)
	}
	return o, err == nil, err
}

func bumpAlarmObservationSource(ctx context.Context, tx pgx.Tx, o model.AlarmObservation, conservative bool) error {
	return bumpAlarmObservationSources(ctx, tx, []model.AlarmObservation{o}, conservative)
}
func bumpAlarmObservationSources(ctx context.Context, tx pgx.Tx, observations []model.AlarmObservation, conservative bool) error {
	if len(observations) == 0 {
		return nil
	}
	o := observations[0]
	keys := []model.GovernanceSourceVersion{}
	for _, v := range observations {
		keys = append(keys, model.ObservationSourceVersions(v, conservative)...)
	}
	versions := governanceVersionOrder(keys)
	for _, v := range versions {
		lockKey := o.TenantID + "\x1f" + v.DependencyKey + "\x1f" + strconv.FormatInt(v.BucketStart, 10)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO alarm_governance_source_version(tenant_id,dependency_key,bucket_start,generation) VALUES($1,$2,$3,1) ON CONFLICT(tenant_id,dependency_key,bucket_start) DO UPDATE SET generation=alarm_governance_source_version.generation+1`, o.TenantID, v.DependencyKey, v.BucketStart); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) SaveAlarmObservation(ctx context.Context, o model.AlarmObservation) (model.AlarmObservation, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return o, false, err
	}
	defer tx.Rollback(ctx)
	saved, created, err := recordAlarmObservation(ctx, tx, o)
	if err != nil {
		return o, false, err
	}
	return saved, created, tx.Commit(ctx)
}
func (r *Repository) GetAlarmObservation(ctx context.Context, tenant, id string) (model.AlarmObservation, error) {
	var o model.AlarmObservation
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM alarm_observation WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &o)
	}
	return o, err
}
func observationTimeSQL(basis string) string {
	switch basis {
	case "RECEIVED_AT":
		return "COALESCE((body->>'receivedAt')::bigint,0)"
	case "EVALUATION_AT":
		return "COALESCE((body->>'evaluationAt')::bigint,0)"
	case "RECORDED_AT":
		return "recorded_at"
	default:
		return "event_at"
	}
}
func observationFilterSQL(tenant string, f ports.AlarmObservationFilter) (string, []any) {
	where := " WHERE tenant_id=$1"
	timeColumn := observationTimeSQL(f.TimeBasis)
	if f.TimeBasis != "" && f.TimeBasis != "EVENT_AT" && f.TimeBasis != "RECEIVED_AT" && f.TimeBasis != "EVALUATION_AT" && f.TimeBasis != "RECORDED_AT" {
		where += " AND FALSE"
	}
	if f.TimeBasis == "RECEIVED_AT" || f.TimeBasis == "EVALUATION_AT" || f.TimeBasis == "RECORDED_AT" {
		where += " AND " + timeColumn + ">0"
	}
	args := []any{tenant}
	add := func(col string, v any) { args = append(args, v); where += fmt.Sprintf(" AND %s=$%d", col, len(args)) }
	if f.DeviceIDs != nil {
		args = append(args, f.DeviceIDs)
		where += fmt.Sprintf(" AND device_id=ANY($%d::text[])", len(args))
	} else if f.DeviceID == "" {
		where += " AND FALSE"
	}
	for _, x := range []struct{ col, v string }{{"device_id", f.DeviceID}, {"component_id", f.ComponentID}, {"alarm_type", f.AlarmType}, {"origin_kind", f.OriginKind}, {"signal_key", f.SignalKey}, {"alarm_id", f.AlarmID}} {
		if x.v != "" {
			add(x.col, x.v)
		}
	}
	if f.Start > 0 {
		args = append(args, f.Start)
		where += fmt.Sprintf(" AND %s >= $%d", timeColumn, len(args))
	}
	if f.End > 0 {
		args = append(args, f.End)
		where += fmt.Sprintf(" AND %s < $%d", timeColumn, len(args))
	}
	if f.Cursor != "" {
		p := strings.SplitN(f.Cursor, ":", 2)
		if len(p) != 2 {
			where += " AND FALSE"
		} else {
			at, err := strconv.ParseInt(p[0], 10, 64)
			if err != nil {
				where += " AND FALSE"
			} else {
				args = append(args, at, p[1])
				where += fmt.Sprintf(" AND (%s,id)>($%d,$%d)", timeColumn, len(args)-1, len(args))
			}
		}
	}
	return where, args
}
func (r *Repository) ListAlarmObservations(ctx context.Context, tenant string, f ports.AlarmObservationFilter) ([]model.AlarmObservation, error) {
	where, args := observationFilterSQL(tenant, f)
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT body FROM alarm_observation%s ORDER BY %s,id LIMIT $%d`, where, observationTimeSQL(f.TimeBasis), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AlarmObservation{}
	for rows.Next() {
		var b []byte
		var o model.AlarmObservation
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (r *Repository) GetAlarmSignalSeed(ctx context.Context, tenant string, f ports.AlarmObservationFilter, before int64) (model.AlarmObservation, error) {
	f.Start = 0
	f.End = before
	f.Cursor = ""
	where, args := observationFilterSQL(tenant, f)
	var o model.AlarmObservation
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM alarm_observation`+where+` AND acceptance='ACCEPTED' AND fact_kind IN ('ASSERT','CLEAR') ORDER BY `+observationTimeSQL(f.TimeBasis)+` DESC,id DESC LIMIT 1`, args...).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &o)
	}
	return o, err
}

// RecoverAlarmSignal records CLEAR even with no active alarm. Source time
// watermarks fence late CLEAR; equal-time CLEAR cannot cancel an ASSERT.
func (r *Repository) RecoverAlarmSignal(ctx context.Context, o model.AlarmObservation, ruleID string) ([]model.Alarm, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = checkRoutingTrace(ctx, tx, o.TenantID, o.DeviceID); err != nil {
		return nil, err
	}
	if err = o.Normalize(); err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, o.TenantID+"\x1f"+o.DeviceID+"\x1f"+o.SignalKey)
	if err != nil {
		return nil, err
	}
	var at int64
	var active bool
	err = tx.QueryRow(ctx, `SELECT event_at,active FROM alarm_signal_state WHERE tenant_id=$1 AND device_id=$2 AND signal_key=$3`, o.TenantID, o.DeviceID, o.SignalKey).Scan(&at, &active)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	o.WatermarkAt = at
	if o.SignalWatermarkAt() < at || o.SignalWatermarkAt() == at && active {
		o.Acceptance = "REJECTED"
		o.Reason = "STALE_OR_EQUAL_CLEAR"
	}
	rows, err := tx.Query(ctx, `SELECT body,version FROM alarm_record WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3 AND status IN ('ACTIVE','ACKED') FOR UPDATE`, o.TenantID, o.DeviceID, ruleID)
	if err != nil {
		return nil, err
	}
	alarms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Alarm, error) {
		var a model.Alarm
		var b []byte
		var version int64
		err := row.Scan(&b, &version)
		if err == nil {
			err = json.Unmarshal(b, &a)
			a.Version = version
		}
		return a, err
	})
	if err != nil {
		return nil, err
	}
	if len(alarms) > 0 {
		o.AlarmID = alarms[0].ID
	}
	saved, created, err := recordAlarmObservation(ctx, tx, o)
	if err != nil {
		return nil, err
	}
	if !created || saved.Acceptance != "ACCEPTED" {
		return nil, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `INSERT INTO alarm_signal_state(tenant_id,device_id,signal_key,event_at,active,observation_id) VALUES($1,$2,$3,$4,false,$5) ON CONFLICT(tenant_id,device_id,signal_key) DO UPDATE SET event_at=excluded.event_at,active=false,observation_id=excluded.observation_id`, o.TenantID, o.DeviceID, o.SignalKey, o.SignalWatermarkAt(), saved.ID)
	if err != nil {
		return nil, err
	}
	out := []model.Alarm{}
	for _, old := range alarms {
		a := old
		a.Status = "RECOVERED"
		a.RecoveredAt = o.EvaluationAt
		if a.RecoveredAt == 0 {
			a.RecoveredAt = o.RecordedAt
		}
		a.Version++
		b, _ := json.Marshal(a)
		_, err = tx.Exec(ctx, `UPDATE alarm_record SET status=$3,body=$4,version=$5 WHERE tenant_id=$1 AND id=$2`, a.TenantID, a.ID, a.Status, b, a.Version)
		if err == nil {
			err = appendRoutingTrace(ctx, tx, routingRecoverStep(old, a, true))
		}
		if err == nil {
			for _, event := range model.DutyAlarmEvents(ctx, old, a) {
				if err = insertDutyEvent(ctx, tx, event); err != nil {
					break
				}
			}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, tx.Commit(ctx)
}

func acceptAlarmSignal(ctx context.Context, tx pgx.Tx, o model.AlarmObservation) error {
	if o.Acceptance != "ACCEPTED" || o.FactKind != "ASSERT" && o.FactKind != "CLEAR" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO alarm_signal_state(tenant_id,device_id,signal_key,event_at,active,observation_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,device_id,signal_key) DO UPDATE SET event_at=excluded.event_at,active=excluded.active,observation_id=excluded.observation_id WHERE excluded.event_at>alarm_signal_state.event_at OR excluded.event_at=alarm_signal_state.event_at AND excluded.active`, o.TenantID, o.DeviceID, o.SignalKey, o.SignalWatermarkAt(), o.FactKind == "ASSERT", o.ID)
	return err
}

func (t *governanceTx) GetAlarmObservation(id string) (model.AlarmObservation, error) {
	ctx, tenant := t.ctx, t.tenant
	var o model.AlarmObservation
	var b []byte
	err := t.tx.QueryRow(ctx, `SELECT body FROM alarm_observation WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &o)
	}
	return o, err
}
func (t *governanceTx) ListAlarmObservations(f ports.AlarmObservationFilter) ([]model.AlarmObservation, error) {
	ctx, tenant := t.ctx, t.tenant
	where, args := observationFilterSQL(tenant, f)
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	args = append(args, limit)
	rows, err := t.tx.Query(ctx, fmt.Sprintf(`SELECT body FROM alarm_observation%s ORDER BY %s,id LIMIT $%d`, where, observationTimeSQL(f.TimeBasis), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AlarmObservation{}
	for rows.Next() {
		var b []byte
		var o model.AlarmObservation
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (t *governanceTx) GetAlarmSignalSeed(f ports.AlarmObservationFilter, before int64) (model.AlarmObservation, error) {
	ctx, tenant := t.ctx, t.tenant
	f.Start = 0
	f.End = before
	f.Cursor = ""
	where, args := observationFilterSQL(tenant, f)
	var o model.AlarmObservation
	var b []byte
	err := t.tx.QueryRow(ctx, `SELECT body FROM alarm_observation`+where+` AND acceptance='ACCEPTED' AND fact_kind IN ('ASSERT','CLEAR') ORDER BY `+observationTimeSQL(f.TimeBasis)+` DESC,id DESC LIMIT 1`, args...).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(b, &o)
	}
	return o, err
}

// RecoverAlarmSignal records CLEAR even with no active alarm. Source time
// watermarks fence late CLEAR; equal-time CLEAR cannot cancel an ASSERT.
