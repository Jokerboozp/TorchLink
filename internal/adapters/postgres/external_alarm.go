package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
)

func (r *Repository) UpsertExternalAlarm(ctx context.Context, report model.Alarm) (model.Alarm, bool, bool, error) {
	if report.TenantID == "" || report.ID == "" || report.DeviceID == "" || report.TriggerID == "" || !strings.Contains(report.RuleID, ":external:") {
		return report, false, false, fmt.Errorf("%w: external alarm identity is incomplete", model.ErrInvalidIngress)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return report, false, false, err
	}
	defer tx.Rollback(ctx)
	saved := report
	saved.TriggerCount, saved.Version = 1, 1
	body, err := json.Marshal(saved)
	if err != nil {
		return report, false, false, err
	}
	// The unique identity serializes first reports too, when no row exists to
	// lock yet. A contender waits for the insertion transaction before reading.
	// No conflict target: the active (tenant, device, rule) index must be an
	// arbiter too, or a contender racing the first insert fails on it instead
	// of waiting.
	tag, err := tx.Exec(ctx, `INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, saved.TenantID, saved.ID, saved.RuleID, saved.DeviceID, saved.Status, saved.AlarmLevel, saved.Source, saved.LastTriggeredAt, body, saved.Version)
	if err != nil {
		return report, false, false, err
	}
	created := tag.RowsAffected() == 1
	if !created {
		var version int64
		err = tx.QueryRow(ctx, `SELECT body,version FROM alarm_record WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, report.TenantID, report.ID).Scan(&body, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return report, false, false, fmt.Errorf("%w: another active alarm already holds this device and rule", model.ErrInvalidIngress)
		}
		if err != nil {
			return report, false, false, err
		}
		saved = model.Alarm{}
		if err = json.Unmarshal(body, &saved); err != nil {
			return report, false, false, err
		}
		saved.Version = version
		if saved.DeviceID != report.DeviceID || saved.RuleID != report.RuleID {
			return report, false, false, fmt.Errorf("%w: external alarm identity belongs to a different device or rule", model.ErrInvalidIngress)
		}
		if saved.TriggerID == report.TriggerID || saved.Status == "CLOSED" || saved.Status == "RECOVERED" {
			return saved, false, false, tx.Commit(ctx)
		}
		saved.Details = report.Details
		saved.Content = report.Content
		saved.LastTriggeredAt = report.LastTriggeredAt
		saved.AlarmLevel = report.AlarmLevel
		saved.Confidence = report.Confidence
		saved.TriggerID = report.TriggerID
		saved.TriggerCount++
		saved.Version++
		body, err = json.Marshal(saved)
		if err != nil {
			return report, false, false, err
		}
		_, err = tx.Exec(ctx, `UPDATE alarm_record SET level=$3,last_triggered_at=$4,body=$5,version=$6 WHERE tenant_id=$1 AND id=$2`, saved.TenantID, saved.ID, saved.AlarmLevel, saved.LastTriggeredAt, body, saved.Version)
		if err != nil {
			return report, false, false, err
		}
	}
	if err = insertOutbox(ctx, tx, model.AlarmReportEvent(saved, report)); err != nil {
		return report, false, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return report, false, false, err
	}
	return saved, created, true, nil
}
