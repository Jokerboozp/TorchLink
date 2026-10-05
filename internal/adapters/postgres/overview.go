package postgres

import (
	"context"
	"fmt"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// DeviceOverviewCounts aggregates registered devices and their states in one
// statement; the result matches the memory store.
func (r *Repository) DeviceOverviewCounts(ctx context.Context, tenant string, restrict bool, ids []string) (model.DeviceOverview, error) {
	rows, err := r.reader().Query(ctx, `
 WITH d AS (
  SELECT id, status, body->>'deviceRole' AS role, coalesce((body->>'autoRegistered')::boolean, false) AS auto
  FROM device_registry WHERE tenant_id=$1 AND (NOT $2::boolean OR id=ANY($3::text[]))
 ), s AS (
  SELECT s.business_status, s.body->>'connectionStatus' AS conn, s.body->>'dataStatus' AS data, s.last_seen_at, d.id IS NOT NULL AS registered
  FROM device_state s LEFT JOIN d ON d.id=s.device_id
  WHERE s.tenant_id=$1 AND (NOT $2::boolean OR s.device_id=ANY($3::text[]))
 )
 SELECT 'total', '', count(*) FROM d
 UNION ALL SELECT 'auto', '', count(*) FILTER (WHERE auto) FROM d
 UNION ALL SELECT 'status', coalesce(nullif(btrim(status),''),'UNKNOWN'), count(*) FROM d GROUP BY 2
 UNION ALL SELECT 'role', coalesce(nullif(btrim(role),''),'UNKNOWN'), count(*) FROM d GROUP BY 2
 UNION ALL SELECT 'reported', '', count(*) FILTER (WHERE registered) FROM s
 UNION ALL SELECT 'discovered', '', count(*) FILTER (WHERE NOT registered) FROM s
 UNION ALL SELECT 'latest', '', coalesce(max(last_seen_at) FILTER (WHERE registered), 0) FROM s
 UNION ALL SELECT 'connection', coalesce(nullif(btrim(conn),''),'UNKNOWN'), count(*) FROM s WHERE registered GROUP BY 2
 UNION ALL SELECT 'data', coalesce(nullif(btrim(data),''),'UNKNOWN'), count(*) FROM s WHERE registered GROUP BY 2
 UNION ALL SELECT 'business', coalesce(nullif(btrim(business_status),''),'UNKNOWN'), count(*) FROM s WHERE registered GROUP BY 2`, tenant, restrict, ids)
	out := model.NewDeviceOverview()
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, key string
		var n int64
		if err := rows.Scan(&kind, &key, &n); err != nil {
			return out, err
		}
		switch kind {
		case "total":
			out.Total = int(n)
		case "auto":
			out.AutoRegistered = int(n)
		case "reported":
			out.Reported = int(n)
		case "discovered":
			out.DiscoveredUnregistered = int(n)
		case "latest":
			out.LatestSeenAt = n
		case "status":
			out.ByStatus[key] = int(n)
		case "role":
			out.ByRole[key] = int(n)
		case "connection":
			out.ConnectionStatus[key] = int(n)
		case "data":
			out.DataStatus[key] = int(n)
		case "business":
			out.BusinessStatus[key] = int(n)
		}
	}
	return out, rows.Err()
}

// AlarmOverviewCounts aggregates in the database; the result matches
// model.AlarmOverview.AddAlarm.
func (r *Repository) AlarmOverviewCounts(ctx context.Context, f ports.AlarmFilter, since int64) (model.AlarmOverview, error) {
	where, args := alarmFilterSQL(f)
	args = append(args, since, model.HighRiskAlarmLevels)
	query := fmt.Sprintf(`WITH a AS (SELECT status, level, source, last_triggered_at FROM alarm_record%[1]s)
 SELECT 'total', '', count(*) FROM a
 UNION ALL SELECT 'active', '', count(*) FILTER (WHERE status='ACTIVE') FROM a
 UNION ALL SELECT 'high', '', count(*) FILTER (WHERE status='ACTIVE' AND level=ANY($%[3]d::text[])) FROM a
 UNION ALL SELECT 'recent', '', count(*) FILTER (WHERE last_triggered_at >= $%[2]d) FROM a
 UNION ALL SELECT 'status', coalesce(nullif(btrim(status),''),'UNKNOWN'), count(*) FROM a GROUP BY 2
 UNION ALL SELECT 'level', coalesce(nullif(btrim(level),''),'UNKNOWN'), count(*) FROM a GROUP BY 2
 UNION ALL SELECT 'source', coalesce(nullif(btrim(source),''),'UNKNOWN'), count(*) FROM a GROUP BY 2`, where, len(args)-1, len(args))
	out := model.NewAlarmOverview()
	rows, err := r.reader().Query(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, key string
		var n int64
		if err := rows.Scan(&kind, &key, &n); err != nil {
			return out, err
		}
		switch kind {
		case "total":
			out.Total = int(n)
		case "active":
			out.Active = int(n)
		case "high":
			out.HighRiskActive = int(n)
		case "recent":
			out.Recent = int(n)
		case "status":
			out.ByStatus[key] = int(n)
		case "level":
			out.ByLevel[key] = int(n)
		case "source":
			out.BySource[key] = int(n)
		}
	}
	return out, rows.Err()
}

func (r *Repository) AIAnalysisOutcomes(ctx context.Context, f ports.AlarmFilter, promptVersion string) ([]model.AIAnalysisOutcome, error) {
	where, args := alarmFilterSQL(f)
	if where == "" {
		where = " WHERE true"
	}
	args = append(args, promptVersion)
	rows, err := r.reader().Query(ctx, fmt.Sprintf(`SELECT coalesce(body->'disposition'->>'aiRiskLevel',''), body->'disposition'->>'result', coalesce(body->'disposition'->>'aiPromptVersion',''), count(*)
 FROM alarm_record%s AND body ? 'disposition' AND ($%d = '' OR body->'disposition'->>'aiPromptVersion' = $%d)
 GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`, where, len(args), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AIAnalysisOutcome{}
	for rows.Next() {
		var v model.AIAnalysisOutcome
		if err := rows.Scan(&v.RiskLevel, &v.Result, &v.PromptVersion, &v.Count); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
