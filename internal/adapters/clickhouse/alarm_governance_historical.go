package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.GovernanceHistoricalArchiveReader = (*Repository)(nil)

// The telemetry archive does not preserve original message type, event or
// parser metadata. Return that limitation with actual stored scalar values;
// do not fabricate an ALARM_REPORT or original production decision.
func (r *Repository) ListGovernanceHistoricalArchiveMessages(ctx context.Context, tenant string, f ports.AlarmObservationFilter) (model.FactPage[model.StandardMessage], error) {
	page := model.FactPage[model.StandardMessage]{Items: []model.StandardMessage{}, FactPageMeta: model.FactPageMeta{Source: model.FactSourceCoverage{Source: "clickhouse_iot_telemetry", ReadAt: time.Now().UnixMilli(), CoverageStart: f.Start, CoverageEnd: f.End, Status: "AVAILABLE", HistoricalReconstructionQuality: "PARTIAL", Limitations: []string{"ORIGINAL_STANDARD_METADATA_MISSING", "INDEPENDENT_SOURCE_READ_CUTOFF", "HISTORICAL_COLLECTION_COVERAGE_UNKNOWN"}}}}
	if tenant == "" || len(f.DeviceIDs) == 0 || f.Start < 0 || f.End <= f.Start || f.Limit < 1 || f.Limit > 1000 {
		return page, model.ErrAnalysisInvalid
	}
	ids := slices.Clone(f.DeviceIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	quoted := make([]string, len(ids))
	for i, id := range ids {
		if id == "" {
			return page, model.ErrAnalysisInvalid
		}
		quoted[i] = quote(id)
	}
	where := fmt.Sprintf("tenant_id=%s AND device_id IN (%s) AND ts>=fromUnixTimestamp64Milli(%d) AND ts<fromUnixTimestamp64Milli(%d)", quote(tenant), strings.Join(quoted, ","), f.Start, f.End)
	if f.Cursor != "" {
		parts := strings.SplitN(f.Cursor, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return page, model.ErrAnalysisInvalid
		}
		at, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || at < f.Start || at >= f.End {
			return page, model.ErrAnalysisInvalid
		}
		where += fmt.Sprintf(" AND (ts>fromUnixTimestamp64Milli(%d) OR (ts=fromUnixTimestamp64Milli(%d) AND message_id>%s))", at, at, quote(parts[1]))
	}
	// Group exact source slots, but retain all distinct stored values so a
	// conflicting duplicate cannot silently become a trustworthy observation.
	sql := fmt.Sprintf("SELECT device_id,message_id,any(product_id) AS product_id,toUnixTimestamp64Milli(ts) AS timestamp,groupUniqArray(toJSONString(properties)) AS values FROM iot_telemetry WHERE %s GROUP BY device_id,message_id,ts ORDER BY ts,message_id LIMIT %d SETTINGS output_format_json_quote_64bit_integers=0 FORMAT JSONEachRow", where, f.Limit+1)
	data, err := r.query(ctx, sql, nil)
	if err != nil {
		return page, err
	}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row struct {
			DeviceID  string   `json:"device_id"`
			MessageID string   `json:"message_id"`
			ProductID string   `json:"product_id"`
			Timestamp int64    `json:"timestamp"`
			Values    []string `json:"values"`
		}
		if json.Unmarshal(line, &row) != nil || len(row.Values) != 1 || !slices.Contains(ids, row.DeviceID) || row.MessageID == "" || row.Timestamp < f.Start || row.Timestamp >= f.End {
			return page, model.ErrAnalysisConflict
		}
		if len(page.Items) >= f.Limit {
			page.HasMore = true
			break
		}
		msg := model.StandardMessage{TenantID: tenant, DeviceID: row.DeviceID, MessageID: row.MessageID, ProductID: row.ProductID, Timestamp: row.Timestamp}
		if json.Unmarshal([]byte(row.Values[0]), &msg.Properties) != nil {
			return page, model.ErrAnalysisConflict
		}
		// Some historical protocols put a component snapshot into properties.
		// Reuse only that actually stored structure, with unverified metadata.
		if components, ok := msg.Properties["components"]; ok {
			msg.Event = map[string]any{"components": components}
			// This type only permits validation of the archived structure; it
			// is not evidence of the original production message category.
			msg.MessageType = model.EventReport
		}
		page.Items = append(page.Items, msg)
		page.Cursor = fmt.Sprintf("%019d:%s", row.Timestamp, row.MessageID)
	}
	return page, nil
}
