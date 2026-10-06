package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"

	"iot-platform/internal/model"
)

// retentionTables maps each purgeable table to its time column, whether the
// column holds epoch milliseconds, and the rows that are never purged
// regardless of age (unprocessed messages, unpublished raw messages and
// alarms that are still active).
var retentionTables = map[string]struct {
	column string
	millis bool
	guard  string
}{
	model.RetentionStandardMessages: {"processed_at", true, "processed_at > 0"},
	model.RetentionRawIndex:         {"received_at", true, "published_at > 0"},
	model.RetentionRawLog:           {"received_at", true, ""},
	model.RetentionReservations:     {"created_at", false, ""},
	model.RetentionStateEvents:      {"created_at", false, ""},
	model.RetentionAlarms:           {"last_triggered_at", true, "status IN ('CLOSED','RECOVERED','SUPPRESSED')"},
	model.RetentionAudit:            {"created_at", true, ""},
	model.RetentionAIToolCalls:      {"created_at", false, ""},
	model.RetentionAIRuns:           {"started_at", true, ""},
	model.RetentionAIConversations:  {"updated_at", true, ""},
	model.RetentionVideoEvents:      {"event_time", true, ""},
	model.RetentionNotifications:    {"created_at", true, "status IN ('SENT','FAILED','CANCELLED')"},
	model.RetentionStandardKeys:     {"created_at", true, ""},
	// A command older than a day is final: ObservedOutcome reports any
	// unanswered command as UNKNOWN after 30 seconds.
	model.RetentionDeviceCommands: {"created_at", true, ""},
	// Drafts, template preparations and retryable PARTIAL_FAILED batches stay;
	// a completed batch goes first and its rows follow on a later pass.
	model.RetentionOnboarding: {"updated_at", true, `((kind IN ('device-batch','device-credential-expiry') AND status='COMPLETED')
 OR (kind LIKE 'device-batch-row:%' AND NOT EXISTS (SELECT 1 FROM onboarding_record b WHERE b.tenant_id=onboarding_record.tenant_id AND b.kind='device-batch' AND 'device-batch-row:'||b.id=onboarding_record.kind)))`},
	model.RetentionReplays: {"created_at", false, "status IN ('COMPLETED','FAILED','CANCELLED','INTERRUPTED')"},
	// Completed backups own their artifacts and backup coverage; the backup
	// service prunes them. Only failures and finished restores are purged.
	model.RetentionBackupTasks: {"started_at", false, "(status='FAILED' OR (backup_type IN ('RESTORE','RESTORE_DRILL') AND status<>'RUNNING'))"},
	// The latest successful inspection backs the AI overview; it is kept.
	model.RetentionHealthInspections: {"started_at", true, `(status<>'running' AND NOT (status='succeeded' AND started_at >= (SELECT max(j.started_at) FROM health_inspection_job j WHERE j.tenant_id=health_inspection_job.tenant_id AND j.status='succeeded')))`},
}

func retentionBound(millis bool, at time.Time) any {
	if millis {
		return at.UnixMilli()
	}
	return at
}

// PurgeRange deletes at most limit rows of table whose time lies in
// [from, to); a zero from has no lower bound. Rows are selected by ctid so
// one statement removes a bounded batch through the table's time index.
func (r *Repository) PurgeRange(ctx context.Context, table string, from, to time.Time, limit int) (int64, error) {
	spec, ok := retentionTables[table]
	if !ok {
		return 0, fmt.Errorf("unknown retention table %q", table)
	}
	if limit <= 0 {
		limit = 5000
	}
	args := []any{retentionBound(spec.millis, to), limit}
	where := spec.column + " < $1"
	if !from.IsZero() {
		args = append(args, retentionBound(spec.millis, from))
		where += " AND " + spec.column + " >= $3"
	}
	if spec.guard != "" {
		where += " AND " + spec.guard
	}
	// ctid is only unique within one table, so a partitioned table is
	// purged partition by partition.
	leaves, err := r.leafTables(ctx, table)
	if err != nil {
		return 0, err
	}
	var deleted int64
	for _, leaf := range leaves {
		args[1] = int64(limit) - deleted
		name := pgx.Identifier{leaf}.Sanitize()
		batch := `DELETE FROM ` + name + ` WHERE ctid = ANY(ARRAY(SELECT ctid FROM ` + name + ` WHERE ` + where + ` LIMIT $2))`
		var n int64
		if table == model.RetentionAlarms {
			// Attachment files of purged alarms are queued for deletion in
			// the same statement.
			err = r.pool.QueryRow(ctx, `WITH d AS (`+batch+` RETURNING tenant_id, id, body->'attachments' AS att),
q AS (INSERT INTO object_cleanup(bucket, object_key)
  SELECT '`+model.AlarmAttachmentBucket+`', d.tenant_id || '/alarms/' || d.id || '/' || (a->>'id')
  FROM d CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(d.att) = 'array' THEN d.att ELSE '[]'::jsonb END) a
  ON CONFLICT DO NOTHING)
SELECT count(*) FROM d`, args...).Scan(&n)
		} else {
			var tag pgconn.CommandTag
			tag, err = r.pool.Exec(ctx, batch, args...)
			n = tag.RowsAffected()
		}
		if err != nil {
			return deleted, err
		}
		if deleted += n; deleted >= int64(limit) {
			break
		}
	}
	return deleted, nil
}

// OldestRetained returns the time of the oldest purgeable row of table.
func (r *Repository) OldestRetained(ctx context.Context, table string) (time.Time, bool, error) {
	spec, ok := retentionTables[table]
	if !ok {
		return time.Time{}, false, fmt.Errorf("unknown retention table %q", table)
	}
	where := ""
	if spec.guard != "" {
		where = " WHERE " + spec.guard
	}
	if spec.millis {
		var v *int64
		if err := r.pool.QueryRow(ctx, `SELECT min(`+spec.column+`) FROM `+table+where).Scan(&v); err != nil || v == nil {
			return time.Time{}, false, err
		}
		return time.UnixMilli(*v), true, nil
	}
	var v *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT min(`+spec.column+`) FROM `+table+where).Scan(&v); err != nil || v == nil {
		return time.Time{}, false, err
	}
	return *v, true, nil
}

// BackupWindows lists the device message ranges covered by completed
// backups: a DEVICE_DAILY backup covers its recorded day, a FULL backup
// covers everything received before it started.
func (r *Repository) BackupWindows(ctx context.Context) ([]model.BackupWindow, error) {
	rows, err := r.pool.Query(ctx, `SELECT backup_type, started_at,
  NULLIF(details->'components'->>'start',''), NULLIF(details->'components'->>'end','')
FROM backup_task WHERE status='COMPLETED' AND backup_type IN ('DEVICE_DAILY','FULL') AND started_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.BackupWindow{}
	for rows.Next() {
		var kind string
		var started time.Time
		var start, end *string
		if err = rows.Scan(&kind, &started, &start, &end); err != nil {
			return nil, err
		}
		window := model.BackupWindow{End: started}
		if kind == "DEVICE_DAILY" {
			if start == nil || end == nil {
				continue
			}
			from, fromErr := time.Parse(time.RFC3339Nano, *start)
			to, toErr := time.Parse(time.RFC3339Nano, *end)
			if fromErr != nil || toErr != nil {
				continue
			}
			window = model.BackupWindow{Start: from, End: to}
		}
		out = append(out, window)
	}
	return out, rows.Err()
}
