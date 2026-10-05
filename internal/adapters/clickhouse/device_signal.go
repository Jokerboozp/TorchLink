package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const maxPropertyStatRows = 200000

// DevicePropertyStats aggregates numeric telemetry properties in ClickHouse,
// where telemetry properties are kept while ClickHouse is configured.
func (r *Repository) DevicePropertyStats(ctx context.Context, tenant string, start, end int64, ranges []model.PropertyRange) ([]model.DevicePropertyStat, error) {
	keys, lows, highs := make([]string, len(ranges)), make([]string, len(ranges)), make([]string, len(ranges))
	for i, rg := range ranges {
		keys[i] = "(" + quote(rg.ProductID) + "," + quote(rg.Property) + ")"
		lows[i], highs[i] = "-inf", "inf"
		if rg.Min != nil {
			lows[i] = strconv.FormatFloat(*rg.Min, 'g', -1, 64)
		}
		if rg.Max != nil {
			highs[i] = strconv.FormatFloat(*rg.Max, 'g', -1, 64)
		}
	}
	q := fmt.Sprintf(`WITH CAST([%s], 'Array(Tuple(String, String))') AS ranges, CAST([%s], 'Array(Float64)') AS lows, CAST([%s], 'Array(Float64)') AS highs
SELECT device_id AS deviceId, any(product_id) AS productId, property, count() AS count, min(v) AS min, max(v) AS max, avg(v) AS mean, stddevPop(v) AS stdDev,
  countIf(v < lo OR v > hi) AS outOfRange
FROM (
  SELECT device_id, product_id, kv.1 AS property, toFloat64OrNull(kv.2) AS v, indexOf(ranges, (product_id, kv.1)) AS ri,
    if(ri > 0, lows[ri], -inf) AS lo, if(ri > 0, highs[ri], inf) AS hi
  FROM iot_telemetry ARRAY JOIN JSONExtractKeysAndValuesRaw(if(properties_text = '', toJSONString(properties), properties_text)) AS kv
  WHERE tenant_id = %s AND ts >= fromUnixTimestamp64Milli(%d) AND ts <= fromUnixTimestamp64Milli(%d)
)
WHERE v IS NOT NULL AND isFinite(v)
GROUP BY device_id, property
ORDER BY device_id, property
LIMIT %d
FORMAT JSONEachRow SETTINGS output_format_json_quote_64bit_integers = 0`, strings.Join(keys, ","), strings.Join(lows, ","), strings.Join(highs, ","), quote(tenant), start, end, maxPropertyStatRows)
	body, err := r.query(ctx, q, nil)
	if err != nil {
		return nil, fmt.Errorf("clickhouse property statistics: %w", err)
	}
	out := []model.DevicePropertyStat{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var v model.DevicePropertyStat
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, fmt.Errorf("decode clickhouse property statistics: %w", err)
		}
		if math.IsNaN(v.StdDev) {
			v.StdDev = 0
		}
		out = append(out, v)
	}
	return out, nil
}

// DeviceReportStats counts reports in the underlying store, which keeps every
// standard message (only telemetry properties move to ClickHouse).
func (r *Repository) DeviceReportStats(ctx context.Context, tenant string, start, end int64) ([]model.DeviceReportStat, error) {
	base, ok := r.Repository.(ports.DeviceTelemetryStats)
	if !ok {
		return nil, errors.New("the underlying store cannot count device reports")
	}
	return base.DeviceReportStats(ctx, tenant, start, end)
}

var _ ports.DeviceTelemetryStats = (*Repository)(nil)
