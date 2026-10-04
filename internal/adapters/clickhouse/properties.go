package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

// PostgreSQL stores telemetry messages without their properties while
// ClickHouse is configured (see postgres.Repository.SetExternalTelemetryProperties);
// these readers restore them from iot_telemetry.properties_text.

func (r *Repository) GetStandardMessageByRaw(ctx context.Context, tenant, rawID string) (model.StandardMessage, error) {
	v, err := r.Repository.GetStandardMessageByRaw(ctx, tenant, rawID)
	if err != nil {
		return v, err
	}
	return v, r.fillOne(ctx, &v)
}

func (r *Repository) GetLatestMessage(ctx context.Context, tenant, device string) (model.StandardMessage, error) {
	v, err := r.Repository.GetLatestMessage(ctx, tenant, device)
	if err != nil {
		return v, err
	}
	return v, r.fillOne(ctx, &v)
}

func (r *Repository) ListDeviceMessages(ctx context.Context, tenant, device string, kind model.MessageType, limit, offset int) ([]model.StandardMessage, int, error) {
	out, total, err := r.Repository.ListDeviceMessages(ctx, tenant, device, kind, limit, offset)
	if err != nil {
		return out, total, err
	}
	refs := make([]*model.StandardMessage, len(out))
	for i := range out {
		refs[i] = &out[i]
	}
	return out, total, r.fillProperties(ctx, refs)
}

func (r *Repository) GetStandardMessagesByRawIDs(ctx context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) {
	out, err := r.Repository.GetStandardMessagesByRawIDs(ctx, tenant, ids)
	if err != nil {
		return out, err
	}
	keys := make([]string, 0, len(out))
	values := make([]model.StandardMessage, 0, len(out))
	for k, v := range out {
		keys, values = append(keys, k), append(values, v)
	}
	refs := make([]*model.StandardMessage, len(values))
	for i := range values {
		refs[i] = &values[i]
	}
	if err = r.fillProperties(ctx, refs); err != nil {
		return out, err
	}
	for i, k := range keys {
		out[k] = values[i]
	}
	return out, nil
}

func (r *Repository) fillOne(ctx context.Context, v *model.StandardMessage) error {
	return r.fillProperties(ctx, []*model.StandardMessage{v})
}

// fillChunk bounds the message IDs of one ClickHouse lookup.
const fillChunk = 200

// fillProperties reads the properties of telemetry messages that came back
// without them, per tenant, narrowed by device and time so the sort key
// prunes the scan.
func (r *Repository) fillProperties(ctx context.Context, messages []*model.StandardMessage) error {
	byTenant := map[string][]*model.StandardMessage{}
	for _, m := range messages {
		if m.Telemetry() && len(m.Properties) == 0 && m.MessageID != "" {
			byTenant[m.TenantID] = append(byTenant[m.TenantID], m)
		}
	}
	for tenant, list := range byTenant {
		for start := 0; start < len(list); start += fillChunk {
			if err := r.fillChunk(ctx, tenant, list[start:min(start+fillChunk, len(list))]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Repository) fillChunk(ctx context.Context, tenant string, list []*model.StandardMessage) error {
	devices, ids := map[string]bool{}, map[string][]*model.StandardMessage{}
	first, last := list[0].Timestamp, list[0].Timestamp
	for _, m := range list {
		devices[m.DeviceID] = true
		ids[m.MessageID] = append(ids[m.MessageID], m)
		first, last = min(first, m.Timestamp), max(last, m.Timestamp)
	}
	quoteAll := func(values []string) string {
		out := make([]string, len(values))
		for i, v := range values {
			out[i] = quote(v)
		}
		return strings.Join(out, ",")
	}
	deviceList := make([]string, 0, len(devices))
	for d := range devices {
		deviceList = append(deviceList, d)
	}
	idList := make([]string, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	q := fmt.Sprintf(`SELECT message_id, any(properties_text) AS text FROM iot_telemetry WHERE tenant_id=%s AND device_id IN (%s) AND ts >= fromUnixTimestamp64Milli(%d) AND ts <= fromUnixTimestamp64Milli(%d) AND message_id IN (%s) AND properties_text != '' GROUP BY message_id FORMAT JSONEachRow`,
		quote(tenant), quoteAll(deviceList), first, last, quoteAll(idList))
	body, err := r.query(ctx, q, nil)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var row struct {
			MessageID string `json:"message_id"`
			Text      string `json:"text"`
		}
		if err = json.Unmarshal([]byte(line), &row); err != nil {
			return err
		}
		var properties map[string]any
		if err = json.Unmarshal([]byte(row.Text), &properties); err != nil {
			return fmt.Errorf("telemetry properties of %s: %w", row.MessageID, err)
		}
		for _, m := range ids[row.MessageID] {
			m.Properties = properties
		}
	}
	return nil
}

// textPropertyHistory reads a property whose name is not a plain identifier
// from the exact properties text, oldest first like the other history reads.
// Rows written before properties_text existed are not found here; callers
// fall back to PostgreSQL, which still holds those properties.
func (r *Repository) textPropertyHistory(ctx context.Context, tenant, device, property string, start, end int64, limit, offset int, count bool) ([]map[string]any, int, error) {
	where := fmt.Sprintf(`tenant_id=%s AND device_id=%s AND ts >= fromUnixTimestamp64Milli(%d) AND ts <= fromUnixTimestamp64Milli(%d) AND JSONHas(properties_text, %s)`, quote(tenant), quote(device), start, end, quote(property))
	total := 0
	if count {
		body, err := r.query(ctx, `SELECT count() AS total FROM iot_telemetry WHERE `+where+` FORMAT JSONEachRow`, nil)
		if err != nil {
			return nil, 0, err
		}
		if total, err = decodeCount(body); err != nil || total == 0 {
			return nil, total, err
		}
	}
	body, err := r.query(ctx, fmt.Sprintf(`SELECT toUnixTimestamp64Milli(ts) AS timestamp, JSONExtractRaw(properties_text, %s) AS raw, message_id AS messageId FROM iot_telemetry WHERE %s ORDER BY ts DESC, message_id DESC LIMIT %d OFFSET %d FORMAT JSONEachRow`, quote(property), where, limit, offset), nil)
	if err != nil {
		return nil, 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	out := make([]map[string]any, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		var row struct {
			Timestamp json.RawMessage `json:"timestamp"`
			Raw       string          `json:"raw"`
			MessageID string          `json:"messageId"`
		}
		if lines[i] == "" || json.Unmarshal([]byte(lines[i]), &row) != nil {
			continue
		}
		var value any
		_ = json.Unmarshal([]byte(row.Raw), &value)
		// 64-bit integers come quoted in JSONEachRow by default.
		ts, _ := strconv.ParseInt(strings.Trim(string(row.Timestamp), `"`), 10, 64)
		out = append(out, map[string]any{"timestamp": ts, "value": value, "messageId": row.MessageID})
	}
	if !count {
		total = len(out)
	}
	return out, total, nil
}
