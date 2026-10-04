package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// alarmReportBatch is how many alarms one EachAlarm query reads.
const alarmReportBatch = 1000

// AlarmDispositionStats aggregates in the database; the result matches
// model.SummarizeAlarms.
func (r *Repository) AlarmDispositionStats(ctx context.Context, f ports.AlarmFilter) (model.AlarmDispositionStats, error) {
	where, args := alarmFilterSQL(f)
	args = append(args, model.FireAlarmTypes(), model.DispositionFalseAlarm, model.TopFalseAlarmDevices)
	types, falseAlarm, top := len(args)-2, len(args)-1, len(args)
	query := fmt.Sprintf(`WITH a AS (
  SELECT device_id, last_triggered_at, id, body->>'deviceName' AS device_name,
    (upper(level)='CRITICAL' OR upper(body->>'alarmType') = ANY($%[2]d::text[])) AS requires,
    body->'disposition'->>'result' AS result,
    CASE WHEN (body->>'ackedAt')::bigint > 0 AND (body->>'ackedAt')::bigint >= (body->>'firstTriggeredAt')::bigint
      THEN (body->>'ackedAt')::bigint - (body->>'firstTriggeredAt')::bigint END AS ack_ms,
    CASE WHEN body ? 'disposition' AND (body->'disposition'->>'verifiedAt')::bigint >= (body->>'firstTriggeredAt')::bigint
      THEN (body->'disposition'->>'verifiedAt')::bigint - (body->>'firstTriggeredAt')::bigint END AS verify_ms
  FROM alarm_record%[1]s
), devices AS (
  SELECT device_id, (array_agg(device_name ORDER BY last_triggered_at DESC, id DESC))[1] AS device_name,
    count(*) FILTER (WHERE result = $%[3]d) AS false_alarms, count(*) AS alarms
  FROM a GROUP BY device_id
)
SELECT count(*), count(result), count(*) FILTER (WHERE requires), count(*) FILTER (WHERE requires AND result IS NULL),
  count(ack_ms), coalesce(trunc(sum(ack_ms) / nullif(count(ack_ms), 0)), 0)::bigint, coalesce(percentile_disc(0.9) WITHIN GROUP (ORDER BY ack_ms), 0),
  count(verify_ms), coalesce(trunc(sum(verify_ms) / nullif(count(verify_ms), 0)), 0)::bigint, coalesce(percentile_disc(0.9) WITHIN GROUP (ORDER BY verify_ms), 0),
  coalesce((SELECT jsonb_object_agg(result, n) FROM (SELECT result, count(*) AS n FROM a WHERE result IS NOT NULL GROUP BY result) r), '{}'),
  coalesce((SELECT jsonb_agg(jsonb_build_object('deviceId', device_id, 'deviceName', coalesce(device_name, ''), 'falseAlarms', false_alarms, 'alarms', alarms) ORDER BY false_alarms DESC, device_id)
    FROM (SELECT * FROM devices WHERE false_alarms > 0 ORDER BY false_alarms DESC, device_id LIMIT $%[4]d) d), '[]')
FROM a`, where, types, falseAlarm, top)
	var out model.AlarmDispositionStats
	var byResult, devices []byte
	err := r.pool.QueryRow(ctx, query, args...).Scan(&out.Total, &out.Verified, &out.RequiringVerification, &out.Unverified,
		&out.Acknowledge.Count, &out.Acknowledge.AvgMs, &out.Acknowledge.P90Ms, &out.Verify.Count, &out.Verify.AvgMs, &out.Verify.P90Ms, &byResult, &devices)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(byResult, &out.ByResult); err != nil {
		return out, err
	}
	if err = json.Unmarshal(devices, &out.TopFalseAlarmDevices); err != nil {
		return out, err
	}
	out.FalseAlarmRate = model.FalseAlarmRate(out.ByResult[model.DispositionFalseAlarm], out.Verified)
	return out, nil
}

// EachAlarm pages by (last_triggered_at, id) so each batch is a short query
// and rows written meanwhile neither repeat nor shift later pages.
func (r *Repository) EachAlarm(ctx context.Context, f ports.AlarmFilter, fn func(model.Alarm) error) error {
	projection := "body"
	if f.Summary {
		projection = "body - 'details' - 'cameras'"
	}
	var lastAt int64
	var lastID string
	for first := true; ; first = false {
		where, args := alarmFilterSQL(f)
		if !first {
			args = append(args, lastAt, lastID)
			cursor := fmt.Sprintf("(last_triggered_at, id) < ($%d, $%d)", len(args)-1, len(args))
			if where == "" {
				where = " WHERE " + cursor
			} else {
				where += " AND " + cursor
			}
		}
		args = append(args, alarmReportBatch)
		rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT last_triggered_at, id, %s FROM alarm_record%s ORDER BY last_triggered_at DESC, id DESC LIMIT $%d`, projection, where, len(args)), args...)
		if err != nil {
			return err
		}
		batch := make([]model.Alarm, 0, alarmReportBatch)
		for rows.Next() {
			var body []byte
			var a model.Alarm
			if err = rows.Scan(&lastAt, &lastID, &body); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal(body, &a); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, a)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, a := range batch {
			if err = fn(a); err != nil {
				return err
			}
		}
		if len(batch) < alarmReportBatch {
			return nil
		}
	}
}

// AlarmBreakdown aggregates in the database; the result matches
// model.BreakdownAlarms. Days are counted in model.ReportZone.
func (r *Repository) AlarmBreakdown(ctx context.Context, f ports.AlarmFilter) (model.AlarmBreakdown, error) {
	where, args := alarmFilterSQL(f)
	_, offset := time.Now().In(model.ReportZone).Zone()
	args = append(args, int64(offset)*1000, model.TopAlarmDevices)
	shift, top := len(args)-1, len(args)
	query := fmt.Sprintf(`WITH a AS (
  SELECT device_id, level, status, coalesce(body->>'alarmType', '') AS alarm_type, body->>'deviceName' AS device_name, last_triggered_at, id,
    to_char(to_timestamp((last_triggered_at + $%[2]d) / 1000.0) AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day
  FROM alarm_record%[1]s
)
SELECT
  coalesce((SELECT jsonb_object_agg(level, n) FROM (SELECT level, count(*) AS n FROM a GROUP BY level) x), '{}'),
  coalesce((SELECT jsonb_object_agg(alarm_type, n) FROM (SELECT alarm_type, count(*) AS n FROM a GROUP BY alarm_type) x), '{}'),
  coalesce((SELECT jsonb_object_agg(status, n) FROM (SELECT status, count(*) AS n FROM a GROUP BY status) x), '{}'),
  coalesce((SELECT jsonb_agg(jsonb_build_object('day', day, 'count', n) ORDER BY day) FROM (SELECT day, count(*) AS n FROM a GROUP BY day) x), '[]'),
  coalesce((SELECT jsonb_agg(jsonb_build_object('deviceId', device_id, 'deviceName', coalesce(device_name, ''), 'falseAlarms', 0, 'alarms', n) ORDER BY n DESC, device_id)
    FROM (SELECT device_id, (array_agg(device_name ORDER BY last_triggered_at DESC, id DESC))[1] AS device_name, count(*) AS n
      FROM a GROUP BY device_id ORDER BY n DESC, device_id LIMIT $%[3]d) x), '[]')`, where, shift, top)
	var out model.AlarmBreakdown
	var levels, types, statuses, days, devices []byte
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&levels, &types, &statuses, &days, &devices); err != nil {
		return out, err
	}
	for target, raw := range map[any][]byte{&out.ByLevel: levels, &out.ByType: types, &out.ByStatus: statuses, &out.ByDay: days, &out.TopDevices: devices} {
		if err := json.Unmarshal(raw, target); err != nil {
			return out, err
		}
	}
	return out, nil
}
