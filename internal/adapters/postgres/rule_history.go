package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
	"iot-platform/internal/rulelab/history"
)

//go:embed rule_history_schema.sql
var ruleHistorySchema string

func ruleHistoryClock(ctx context.Context, tx pgx.Tx) (int64, error) {
	var now int64
	err := tx.QueryRow(ctx, `SELECT `+nowMS).Scan(&now)
	return now, err
}
func ruleHistoryLock(ctx context.Context, tx pgx.Tx, tenant string, shared bool) error {
	query := `SELECT pg_advisory_xact_lock(hashtextextended('rule-history:'||$1,0))`
	if shared {
		query = `SELECT pg_advisory_xact_lock_shared(hashtextextended('rule-history:'||$1,0))`
	}
	_, err := tx.Exec(ctx, query, tenant)
	return err
}
func insertRuleHistory(ctx context.Context, tx pgx.Tx, revision model.AlarmRuleRevision, activation model.AlarmRuleActivation) error {
	body, err := json.Marshal(revision)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alarm_rule_revision(tenant_id,id,rule_id,version,hash,registered_at,body) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, revision.TenantID, revision.ID, revision.RuleID, revision.Version, revision.Hash, revision.RegisteredAt, body); err != nil {
		return err
	}
	body, err = json.Marshal(activation)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alarm_rule_activation(tenant_id,rule_id,revision_id,version,since_at,deleted,body) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, activation.TenantID, activation.RuleID, activation.RevisionID, activation.Version, activation.Since, activation.Deleted, body); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO alarm_rule_current_revision(tenant_id,rule_id,revision_id,version,deleted) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,rule_id) DO UPDATE SET revision_id=excluded.revision_id,version=excluded.version,deleted=excluded.deleted`, activation.TenantID, activation.RuleID, activation.RevisionID, activation.Version, activation.Deleted)
	return err
}
func currentRuleHistory(ctx context.Context, tx pgx.Tx, tenant, rule string) (*model.AlarmRuleRevision, error) {
	var body []byte
	err := tx.QueryRow(ctx, `SELECT r.body FROM alarm_rule_current_revision c JOIN alarm_rule_revision r ON r.tenant_id=c.tenant_id AND r.id=c.revision_id WHERE c.tenant_id=$1 AND c.rule_id=$2`, tenant, rule).Scan(&body)
	if err == nil {
		var revision model.AlarmRuleRevision
		err = json.Unmarshal(body, &revision)
		return &revision, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = tx.QueryRow(ctx, `SELECT body FROM alarm_rule WHERE tenant_id=$1 AND id=$2`, tenant, rule).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var current model.AlarmRule
	if err = json.Unmarshal(body, &current); err != nil {
		return nil, err
	}
	now, err := ruleHistoryClock(ctx, tx)
	if err != nil {
		return nil, err
	}
	revision, activation := history.Register(current, now)
	if err = insertRuleHistory(ctx, tx, revision, activation); err != nil {
		return nil, err
	}
	return &revision, nil
}
func publishRuleHistory(ctx context.Context, tx pgx.Tx, request model.RulePublishRequest, legacy bool) (model.AlarmRuleRevision, error) {
	if err := ruleHistoryLock(ctx, tx, request.Rule.TenantID, false); err != nil {
		return model.AlarmRuleRevision{}, err
	}
	current, err := currentRuleHistory(ctx, tx, request.Rule.TenantID, request.Rule.ID)
	if err != nil {
		return model.AlarmRuleRevision{}, err
	}
	var deleted bool
	if current != nil {
		if err = tx.QueryRow(ctx, `SELECT deleted FROM alarm_rule_current_revision WHERE tenant_id=$1 AND rule_id=$2`, request.Rule.TenantID, request.Rule.ID).Scan(&deleted); err != nil {
			return model.AlarmRuleRevision{}, err
		}
	}
	if request.Delete && (current == nil || deleted) {
		return model.AlarmRuleRevision{}, model.ErrNotFound
	}
	if legacy {
		request.ExpectedBaselineVersion = 0
		if current != nil {
			request.ExpectedBaselineVersion = current.Version
			if model.RuleBodyHash(current.Rule) == model.RuleBodyHash(request.Rule) && !request.Delete && !deleted {
				return *current, nil
			}
		}
	}
	if request.RollbackFrom != "" {
		var body []byte
		var source model.AlarmRuleRevision
		if err = tx.QueryRow(ctx, `SELECT body FROM alarm_rule_revision WHERE tenant_id=$1 AND id=$2`, request.Rule.TenantID, request.RollbackFrom).Scan(&body); err != nil || json.Unmarshal(body, &source) != nil || source.RuleID != request.Rule.ID || !history.SameRulePolicy(source.Rule, request.Rule) {
			return model.AlarmRuleRevision{}, model.ErrRuleHistoryInvalid
		}
	}
	now, err := ruleHistoryClock(ctx, tx)
	if err != nil {
		return model.AlarmRuleRevision{}, err
	}
	revision, activation, err := history.Revision(request, current, now)
	if err != nil {
		return revision, err
	}
	if err = insertRuleHistory(ctx, tx, revision, activation); err != nil {
		return revision, err
	}
	if request.Delete {
		_, err = tx.Exec(ctx, `DELETE FROM alarm_rule WHERE tenant_id=$1 AND id=$2`, revision.TenantID, revision.RuleID)
	} else {
		body, e := json.Marshal(revision.Rule)
		if e != nil {
			return revision, e
		}
		_, err = tx.Exec(ctx, `INSERT INTO alarm_rule(tenant_id,id,product_id,enabled,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,id) DO UPDATE SET product_id=excluded.product_id,enabled=excluded.enabled,body=excluded.body,updated_at=now()`, revision.TenantID, revision.RuleID, revision.Rule.ProductID, revision.Rule.Enabled, body)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `DELETE FROM alarm_rule_revision_pending WHERE tenant_id=$1 AND rule_id=$2`, revision.TenantID, revision.RuleID)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `DELETE FROM alarm_rule_pending WHERE tenant_id=$1 AND rule_id=$2`, revision.TenantID, revision.RuleID)
	}
	return revision, err
}
func (r *Repository) PublishRule(ctx context.Context, request model.RulePublishRequest) (model.AlarmRuleRevision, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.AlarmRuleRevision{}, err
	}
	defer tx.Rollback(ctx)
	v, err := publishRuleHistory(ctx, tx, request, false)
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) saveRuleHistory(ctx context.Context, rule model.AlarmRule, deleted bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	reason := "rule saved"
	if deleted {
		reason = "rule deleted; retain unresolved alarms"
	}
	_, err = publishRuleHistory(ctx, tx, model.RulePublishRequest{Rule: rule, Reason: reason, Actor: "system", SemanticsVersion: eval.RevisionV2, Delete: deleted}, true)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) GetRuleRevision(ctx context.Context, tenant, id string) (model.AlarmRuleRevision, error) {
	var body []byte
	var value model.AlarmRuleRevision
	err := r.pool.QueryRow(ctx, `SELECT body FROM alarm_rule_revision WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(body, &value)
	}
	return value, err
}
func (r *Repository) ListRuleRevisions(ctx context.Context, tenant, rule string, limit, offset int) ([]model.AlarmRuleRevision, int, error) {
	if _, err := r.RuleEvaluationRules(ctx, tenant, ""); err != nil {
		return nil, 0, err
	}
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_rule_revision WHERE tenant_id=$1 AND rule_id=$2`, tenant, rule).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM alarm_rule_revision WHERE tenant_id=$1 AND rule_id=$2 ORDER BY version DESC LIMIT $3 OFFSET $4`, tenant, rule, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.AlarmRuleRevision{}
	for rows.Next() {
		var body []byte
		var v model.AlarmRuleRevision
		if err = rows.Scan(&body); err == nil {
			err = json.Unmarshal(body, &v)
		}
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
func (r *Repository) ListRuleActivations(ctx context.Context, tenant, rule string, limit, offset int) ([]model.AlarmRuleActivation, int, error) {
	if _, err := r.RuleEvaluationRules(ctx, tenant, ""); err != nil {
		return nil, 0, err
	}
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_rule_activation WHERE tenant_id=$1 AND rule_id=$2`, tenant, rule).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body,lead(since_at) OVER (ORDER BY version) FROM alarm_rule_activation WHERE tenant_id=$1 AND rule_id=$2 ORDER BY version DESC LIMIT $3 OFFSET $4`, tenant, rule, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.AlarmRuleActivation{}
	for rows.Next() {
		var body []byte
		var v model.AlarmRuleActivation
		var until *int64
		if err = rows.Scan(&body, &until); err == nil {
			err = json.Unmarshal(body, &v)
		}
		if err != nil {
			return nil, 0, err
		}
		v.Until = until
		out = append(out, v)
	}
	return out, total, rows.Err()
}
func (r *Repository) RuleEvaluationRules(ctx context.Context, tenant, device string) ([]model.AlarmRuleRevision, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = ruleHistoryLock(ctx, tx, tenant, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM alarm_rule WHERE tenant_id=$1 ORDER BY updated_at DESC,id`, tenant)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err = currentRuleHistory(ctx, tx, tenant, id); err != nil {
			return nil, err
		}
	}
	rows, err = tx.Query(ctx, `SELECT r.body FROM alarm_rule_current_revision c JOIN alarm_rule_revision r ON r.tenant_id=c.tenant_id AND r.id=c.revision_id LEFT JOIN alarm_rule a ON a.tenant_id=c.tenant_id AND a.id=c.rule_id WHERE c.tenant_id=$1 AND (NOT c.deleted OR EXISTS(SELECT 1 FROM alarm_record alarm WHERE alarm.tenant_id=c.tenant_id AND alarm.rule_id=c.rule_id AND alarm.device_id=$2 AND alarm.status='ACTIVE')) ORDER BY COALESCE(a.updated_at,to_timestamp(r.registered_at/1000.0)) DESC,c.rule_id`, tenant, device)
	if err != nil {
		return nil, err
	}
	out := []model.AlarmRuleRevision{}
	for rows.Next() {
		var body []byte
		var v model.AlarmRuleRevision
		if err = rows.Scan(&body); err == nil {
			err = json.Unmarshal(body, &v)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func checkRuleTraceClaim(ctx context.Context, tx pgx.Tx, binding model.RuleTraceBinding) error {
	var token, processed int64
	err := tx.QueryRow(ctx, `SELECT claim_token,processed_at FROM standard_message WHERE tenant_id=$1 AND message_id=$2 FOR UPDATE`, binding.TenantID, binding.MessageID).Scan(&token, &processed)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return err
	}
	if token != binding.ClaimToken || processed != 0 {
		return model.ErrStaleClaim
	}
	return nil
}
func (r *Repository) BeginRuleEvaluationTrace(ctx context.Context, trace model.RuleEvaluationTrace) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = checkRuleTraceClaim(ctx, tx, trace.RuleTraceBinding); err != nil {
		return err
	}
	now, err := ruleHistoryClock(ctx, tx)
	if err != nil {
		return err
	}
	trace, err = history.Begin(trace, now)
	if err != nil {
		return err
	}
	trace.Routing.Initial, err = routingSnapshot(ctx, tx, trace.TenantID, trace.DeviceID)
	if err != nil {
		return err
	}
	for _, revision := range trace.Rules {
		var hash string
		if err = tx.QueryRow(ctx, `SELECT hash FROM alarm_rule_revision WHERE tenant_id=$1 AND id=$2`, trace.TenantID, revision.ID).Scan(&hash); err != nil || hash != revision.Hash {
			return model.ErrRuleHistoryInvalid
		}
	}
	body, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO rule_evaluation_trace(tenant_id,id,message_id,claim_token,device_id,message_timestamp,started_at,status,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, trace.TenantID, trace.ID, trace.MessageID, trace.ClaimToken, trace.DeviceID, trace.MessageTimestamp, trace.StartedAt, trace.Status, body); err != nil {
		return err
	}
	var existing []byte
	if err = tx.QueryRow(ctx, `SELECT body FROM rule_evaluation_trace WHERE tenant_id=$1 AND id=$2`, trace.TenantID, trace.ID).Scan(&existing); err != nil {
		return err
	}
	var previous model.RuleEvaluationTrace
	if err = json.Unmarshal(existing, &previous); err != nil {
		return err
	}
	if previous.MessageHash != trace.MessageHash || previous.RuleSetHash != trace.RuleSetHash {
		return model.ErrRuleHistoryInvalid
	}
	return tx.Commit(ctx)
}
func readRuleTrace(ctx context.Context, tx pgx.Tx, binding model.RuleTraceBinding) (model.RuleEvaluationTrace, error) {
	var body []byte
	var trace model.RuleEvaluationTrace
	err := tx.QueryRow(ctx, `SELECT body FROM rule_evaluation_trace WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, binding.TenantID, model.RuleTraceID(binding)).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return trace, model.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(body, &trace)
	}
	return trace, err
}
func writeRuleTrace(ctx context.Context, tx pgx.Tx, trace model.RuleEvaluationTrace) error {
	body, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE rule_evaluation_trace SET status=$3,body=$4 WHERE tenant_id=$1 AND id=$2`, trace.TenantID, trace.ID, trace.Status, body)
	return err
}
func (r *Repository) CommitRuleEvaluationStep(ctx context.Context, binding model.RuleTraceBinding, sequence int, calculate func(model.RuleEvaluationState) (model.RuleEvaluationStep, error)) (model.RuleEvaluationStep, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.RuleEvaluationStep{}, err
	}
	defer tx.Rollback(ctx)
	if err = checkRuleTraceClaim(ctx, tx, binding); err != nil {
		return model.RuleEvaluationStep{}, err
	}
	trace, err := readRuleTrace(ctx, tx, binding)
	if err != nil {
		return model.RuleEvaluationStep{}, err
	}
	if sequence < 0 || sequence >= len(trace.Rules) {
		return model.RuleEvaluationStep{}, model.ErrRuleHistoryInvalid
	}
	revision := trace.Rules[sequence]
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('rule-lifecycle:'||$1||':'||$2||':'||$3,0))`, binding.TenantID, revision.RuleID, trace.DeviceID); err != nil {
		return model.RuleEvaluationStep{}, err
	}
	state := model.RuleEvaluationState{InitialStateQuality: "KNOWN"}
	err = tx.QueryRow(ctx, `SELECT since_at FROM alarm_rule_revision_pending WHERE tenant_id=$1 AND revision_id=$2 AND device_id=$3 FOR UPDATE`, binding.TenantID, revision.ID, trace.DeviceID).Scan(&state.Pending.Since)
	if err == nil {
		state.Pending.Exists = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return model.RuleEvaluationStep{}, err
	}
	var body []byte
	err = tx.QueryRow(ctx, `SELECT body,version FROM alarm_record WHERE tenant_id=$1 AND rule_id=$2 AND device_id=$3 AND status IN ('ACTIVE','ACKED') FOR UPDATE`, binding.TenantID, revision.RuleID, trace.DeviceID).Scan(&body, &state.AlarmVersion)
	if err == nil {
		if err = json.Unmarshal(body, &state.Alarm); err != nil {
			return model.RuleEvaluationStep{}, err
		}
		state.Alarm.Version = state.AlarmVersion
		if state.Alarm.CreatedRuleRevision != "" {
			var revisionBody []byte
			if err = tx.QueryRow(ctx, `SELECT body FROM alarm_rule_revision WHERE tenant_id=$1 AND id=$2`, binding.TenantID, state.Alarm.CreatedRuleRevision).Scan(&revisionBody); err == nil {
				var recovery model.AlarmRuleRevision
				if err = json.Unmarshal(revisionBody, &recovery); err != nil {
					return model.RuleEvaluationStep{}, err
				}
				state.RecoveryRevision = &recovery
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return model.RuleEvaluationStep{}, err
			}
		}
		if state.RecoveryRevision == nil {
			state.InitialStateQuality = "UNKNOWN"
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return model.RuleEvaluationStep{}, err
	}
	step, err := calculate(state)
	if err != nil {
		return step, err
	}
	now, err := ruleHistoryClock(ctx, tx)
	if err != nil {
		return step, err
	}
	step, err = history.Step(trace, sequence, state, step, now)
	if err != nil {
		return step, err
	}
	if capture, ok := model.AlarmObservationCaptureFromContext(ctx); ok && capture.Observation != nil {
		o := *capture.Observation
		o.AlarmID = step.Alarm.ID
		if o.AlarmID == "" {
			o.AlarmID = state.Alarm.ID
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, o.TenantID+"\x1f"+o.DeviceID+"\x1f"+o.SignalKey); err != nil {
			return step, err
		}
		err = tx.QueryRow(ctx, `SELECT event_at FROM alarm_signal_state WHERE tenant_id=$1 AND device_id=$2 AND signal_key=$3`, o.TenantID, o.DeviceID, o.SignalKey).Scan(&o.WatermarkAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return step, err
		}
		saved, created, err := recordAlarmObservation(ctx, tx, o)
		if err != nil {
			return step, err
		}
		if created {
			if err = acceptAlarmSignal(ctx, tx, saved); err != nil {
				return step, err
			}
		}
	}
	if step.WriteAlarm {
		step.Alarm.Version = state.Alarm.Version + 1
		step.AlarmVersion = step.Alarm.Version
		body, err = json.Marshal(step.Alarm)
		if err != nil {
			return step, err
		}
		if step.Created {
			_, err = tx.Exec(ctx, `INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, step.Alarm.TenantID, step.Alarm.ID, step.Alarm.RuleID, step.Alarm.DeviceID, step.Alarm.Status, step.Alarm.AlarmLevel, step.Alarm.Source, step.Alarm.LastTriggeredAt, body, step.Alarm.Version)
		} else {
			_, err = tx.Exec(ctx, `UPDATE alarm_record SET status=$3,level=$4,last_triggered_at=$5,body=$6,version=$7 WHERE tenant_id=$1 AND id=$2`, step.Alarm.TenantID, step.Alarm.ID, step.Alarm.Status, step.Alarm.AlarmLevel, step.Alarm.LastTriggeredAt, body, step.Alarm.Version)
		}
		if err != nil {
			return step, err
		}
		if step.RuleAlarmHandled {
			report := step.Alarm
			if step.ReportAlarm != nil {
				report = *step.ReportAlarm
			}
			report.CreatedRuleRevision = step.Alarm.CreatedRuleRevision
			report.TriggerRuleRevision = step.Alarm.TriggerRuleRevision
			if err = insertOutbox(ctx, tx, model.AlarmReportEvent(step.Alarm, report)); err != nil {
				return step, err
			}
		}
		for _, event := range model.DutyAlarmEvents(ctx, state.Alarm, step.Alarm) {
			if err = insertDutyEvent(ctx, tx, event); err != nil {
				return step, err
			}
		}
	}
	switch step.PendingMutation {
	case eval.SetPending:
		_, err = tx.Exec(ctx, `INSERT INTO alarm_rule_revision_pending(tenant_id,rule_id,revision_id,device_id,since_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,revision_id,device_id) DO UPDATE SET since_at=LEAST(alarm_rule_revision_pending.since_at,excluded.since_at)`, binding.TenantID, revision.RuleID, revision.ID, trace.DeviceID, step.Pending.Since)
	case eval.ClearPending:
		_, err = tx.Exec(ctx, `DELETE FROM alarm_rule_revision_pending WHERE tenant_id=$1 AND revision_id=$2 AND device_id=$3`, binding.TenantID, revision.ID, trace.DeviceID)
	}
	if err != nil {
		return step, err
	}
	trace.Steps = append(trace.Steps, history.Trim(step))
	if err = writeRuleTrace(ctx, tx, trace); err != nil {
		return step, err
	}
	return step, tx.Commit(ctx)
}
func (r *Repository) RecordRuleRoutingTrace(ctx context.Context, binding model.RuleTraceBinding, routing model.RuleRoutingTrace) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = checkRuleTraceClaim(ctx, tx, binding); err != nil {
		return err
	}
	trace, err := readRuleTrace(ctx, tx, binding)
	if err != nil {
		return err
	}
	routing.Committed = true
	routing.Initial = trace.Routing.Initial
	routing.Final, err = routingSnapshot(ctx, tx, binding.TenantID, trace.DeviceID)
	if err != nil {
		return err
	}
	trace.Routing = routing
	if err = writeRuleTrace(ctx, tx, trace); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) completeStandardWithRuleTrace(ctx context.Context, tenant, message string, token int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE standard_message SET processed_at=GREATEST(`+nowMS+`,1),claim_expires_at=0 WHERE tenant_id=$1 AND message_id=$2 AND claim_token=$3 AND processed_at=0`, tenant, message, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		var current int64
		if err = tx.QueryRow(ctx, `SELECT claim_token FROM standard_message WHERE tenant_id=$1 AND message_id=$2`, tenant, message).Scan(&current); errors.Is(err, pgx.ErrNoRows) {
			return model.ErrNotFound
		} else if err != nil {
			return err
		}
		return model.ErrStaleClaim
	}
	trace, err := readRuleTrace(ctx, tx, model.RuleTraceBinding{TenantID: tenant, MessageID: message, ClaimToken: token})
	if err == nil {
		now, e := ruleHistoryClock(ctx, tx)
		if e != nil {
			return e
		}
		if err = writeRuleTrace(ctx, tx, history.Finish(trace, now)); err != nil {
			return err
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) ListRuleEvaluationTraces(ctx context.Context, tenant string, filter model.RuleTraceFilter) ([]model.RuleEvaluationTrace, int, error) {
	if err := history.ValidateFilter(filter); err != nil {
		return nil, 0, err
	}
	limit, offset := normalizePage(filter.Limit, filter.Offset)
	var total int
	where := `tenant_id=$1 AND device_id=ANY($2::text[]) AND ($3::text[] IS NULL OR message_id=ANY($3::text[])) AND ($4::bigint=0 OR message_timestamp>=$4) AND ($5::bigint=0 OR message_timestamp<$5)`
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM rule_evaluation_trace WHERE `+where, tenant, filter.DeviceIDs, filter.MessageIDs, filter.Start, filter.End).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT body FROM rule_evaluation_trace WHERE `+where+` ORDER BY started_at,id LIMIT $6 OFFSET $7`, tenant, filter.DeviceIDs, filter.MessageIDs, filter.Start, filter.End, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.RuleEvaluationTrace{}
	for rows.Next() {
		var body []byte
		var v model.RuleEvaluationTrace
		if err = rows.Scan(&body); err == nil {
			err = json.Unmarshal(body, &v)
		}
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
