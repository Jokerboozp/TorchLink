package postgres

import (
	"context"
	"encoding/json"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// maxTelemetryStatRows bounds the per-device property groups one tenant's
// statistics return.
const maxTelemetryStatRows = 200000

func (r *Repository) DevicePropertyStats(ctx context.Context, tenant string, start, end int64, ranges []model.PropertyRange) ([]model.DevicePropertyStat, error) {
	products, properties := make([]string, len(ranges)), make([]string, len(ranges))
	lows, highs := make([]*float64, len(ranges)), make([]*float64, len(ranges))
	for i, rg := range ranges {
		products[i], properties[i], lows[i], highs[i] = rg.ProductID, rg.Property, rg.Min, rg.Max
	}
	rows, err := r.reader().Query(ctx, `
 WITH r AS (SELECT * FROM unnest($4::text[], $5::text[], $6::float8[], $7::float8[]) AS r(product_id, property, lo, hi)),
 v AS (
  SELECT m.device_id, m.product_id, p.key AS property, (p.value #>> '{}')::float8 AS v
  FROM standard_message m CROSS JOIN LATERAL jsonb_each(m.properties) p
  WHERE m.tenant_id=$1 AND m.ts >= $2 AND m.ts <= $3 AND jsonb_typeof(p.value)='number'
 )
 SELECT v.device_id, max(v.product_id), v.property, count(*), min(v.v), max(v.v), avg(v.v), coalesce(stddev_pop(v.v), 0),
  count(*) FILTER (WHERE v.v < r.lo OR v.v > r.hi)
 FROM v LEFT JOIN r ON r.product_id=v.product_id AND r.property=v.property
 GROUP BY v.device_id, v.property
 ORDER BY v.device_id, v.property
 LIMIT $8`, tenant, start, end, products, properties, lows, highs, maxTelemetryStatRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := []model.DevicePropertyStat{}
	for rows.Next() {
		var v model.DevicePropertyStat
		if err := rows.Scan(&v.DeviceID, &v.ProductID, &v.Property, &v.Count, &v.Min, &v.Max, &v.Mean, &v.StdDev, &v.OutOfRange); err != nil {
			return nil, err
		}
		stats = append(stats, v)
	}
	return stats, rows.Err()
}

func (r *Repository) DeviceReportStats(ctx context.Context, tenant string, start, end int64) ([]model.DeviceReportStat, error) {
	rows, err := r.reader().Query(ctx, `SELECT device_id, max(product_id), count(*), min(ts), max(ts) FROM standard_message
 WHERE tenant_id=$1 AND ts >= $2 AND ts <= $3 GROUP BY device_id ORDER BY device_id`, tenant, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := []model.DeviceReportStat{}
	for rows.Next() {
		var v model.DeviceReportStat
		if err := rows.Scan(&v.DeviceID, &v.ProductID, &v.Count, &v.FirstAt, &v.LastAt); err != nil {
			return nil, err
		}
		reports = append(reports, v)
	}
	return reports, rows.Err()
}

func (r *Repository) ReplaceDeviceSignals(ctx context.Context, tenant string, signals []model.DeviceSignal) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM device_signal WHERE tenant_id=$1`, tenant); err != nil {
		return err
	}
	if len(signals) > 0 {
		n := len(signals)
		devices, types, properties, products, evidence := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		strengths := make([]float64, n)
		starts, ends, updated := make([]int64, n), make([]int64, n), make([]int64, n)
		for i, s := range signals {
			b, _ := json.Marshal(s.Evidence)
			if s.Evidence == nil {
				b = []byte("{}")
			}
			devices[i], types[i], properties[i], products[i], evidence[i] = s.DeviceID, s.SignalType, s.Property, s.ProductID, string(b)
			strengths[i], starts[i], ends[i], updated[i] = s.Strength, s.WindowStart, s.WindowEnd, s.UpdatedAt
		}
		if _, err = tx.Exec(ctx, `INSERT INTO device_signal(tenant_id,device_id,signal_type,property,product_id,strength,window_start,window_end,evidence,updated_at)
 SELECT $1, d, t, p, pr, s, ws, we, e::jsonb, u FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::float8[], $7::bigint[], $8::bigint[], $9::text[], $10::bigint[]) AS x(d, t, p, pr, s, ws, we, e, u)
 ON CONFLICT (tenant_id, device_id, signal_type, property) DO NOTHING`, tenant, devices, types, properties, products, strengths, starts, ends, evidence, updated); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListDeviceSignals(ctx context.Context, tenant string, deviceIDs []string, limit int) ([]model.DeviceSignal, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := r.reader().Query(ctx, `SELECT device_id, product_id, signal_type, property, strength, window_start, window_end, evidence, updated_at FROM device_signal
 WHERE tenant_id=$1 AND ($2::text[] IS NULL OR device_id = ANY($2)) ORDER BY strength DESC, device_id, signal_type, property LIMIT $3`, tenant, deviceIDs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DeviceSignal{}
	for rows.Next() {
		v := model.DeviceSignal{TenantID: tenant}
		var evidence []byte
		if err := rows.Scan(&v.DeviceID, &v.ProductID, &v.SignalType, &v.Property, &v.Strength, &v.WindowStart, &v.WindowEnd, &evidence, &v.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(evidence, &v.Evidence)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) SignalTenants(ctx context.Context) ([]string, error) {
	rows, err := r.reader().Query(ctx, `SELECT DISTINCT tenant_id FROM device_state ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var tenant string
		if err := rows.Scan(&tenant); err != nil {
			return nil, err
		}
		out = append(out, tenant)
	}
	return out, rows.Err()
}

var (
	_ ports.DeviceSignalStore    = (*Repository)(nil)
	_ ports.DeviceTelemetryStats = (*Repository)(nil)
)
