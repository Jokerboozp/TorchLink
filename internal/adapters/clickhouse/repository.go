package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Repository decorates durable business storage with ClickHouse telemetry writes and queries.
type Repository struct {
	ports.Repository
	base           string
	http           *http.Client
	batchOnce      sync.Once
	telemetryBatch *insertBatcher
	rawBatch       *insertBatcher
}

var propertyCodePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func New(ctx context.Context, base string, repo ports.Repository) (*Repository, error) {
	r := &Repository{Repository: repo, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 30 * time.Second}}
	u, err := url.Parse(r.base)
	if err != nil {
		return nil, err
	}
	database := u.Query().Get("database")
	if database != "" {
		if !propertyCodePattern.MatchString(database) {
			return nil, fmt.Errorf("invalid clickhouse database name %q", database)
		}
		bootstrapURL := *u
		params := bootstrapURL.Query()
		params.Del("database")
		bootstrapURL.RawQuery = params.Encode()
		bootstrap := &Repository{Repository: repo, base: strings.TrimRight(bootstrapURL.String(), "/"), http: r.http}
		if _, err = bootstrap.query(ctx, "CREATE DATABASE IF NOT EXISTS "+database, nil); err != nil {
			return nil, err
		}
	}
	schema := `CREATE TABLE IF NOT EXISTS iot_telemetry (tenant_id String, device_id String, product_id String, message_id String, ts DateTime64(3), properties JSON) ENGINE=MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (tenant_id,device_id,ts,message_id)`
	if _, err = r.query(ctx, schema, nil); err != nil {
		return nil, err
	}
	rawSchema := `CREATE TABLE IF NOT EXISTS iot_raw_message (tenant_id String, message_id String, product_id String, device_id String, protocol String, payload_format String, payload_hash String, payload_size UInt64, received_at Int64, body String) ENGINE=MergeTree PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(received_at)) ORDER BY (tenant_id,device_id,received_at,message_id)`
	if _, err = r.query(ctx, rawSchema, nil); err != nil {
		return nil, err
	}
	// Lookups by message ID carry the device ID so the sort key prunes; the
	// bloom filter covers lookups that know only the message ID. Existing
	// parts get the index as they merge.
	for _, table := range []string{"iot_raw_message", "iot_telemetry"} {
		if _, err = r.query(ctx, "ALTER TABLE "+table+" ADD INDEX IF NOT EXISTS idx_message_id message_id TYPE bloom_filter(0.01) GRANULARITY 1", nil); err != nil {
			return nil, err
		}
	}
	return r, nil
}
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error {
	_, err := r.SaveStandardMessageIfAbsent(ctx, v)
	return err
}
func (r *Repository) SaveStandardMessageIfAbsent(ctx context.Context, v model.StandardMessage) (bool, error) {
	created, err := r.Repository.SaveStandardMessageIfAbsent(ctx, v)
	if err != nil {
		return created, err
	}
	if err = r.ensureTelemetry(ctx, v, created); err != nil {
		return created, err
	}
	return created, nil
}

func telemetryMessage(v model.StandardMessage) bool {
	return v.MessageType == model.PropertyReport || v.MessageType == model.AlarmReport
}

func (r *Repository) ensureTelemetry(ctx context.Context, v model.StandardMessage, created bool) error {
	if !telemetryMessage(v) {
		return nil
	}
	if !created {
		exists, err := r.telemetryExists(ctx, v.TenantID, v.DeviceID, v.MessageID)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}
	row := map[string]any{"tenant_id": v.TenantID, "device_id": v.DeviceID, "product_id": v.ProductID, "message_id": v.MessageID, "ts": time.UnixMilli(v.Timestamp).UTC().Format("2006-01-02 15:04:05.000"), "properties": v.Properties}
	b, _ := json.Marshal(row)
	b = append(b, '\n')
	err := r.batches().telemetry.add(ctx, b)
	return err
}

func (r *Repository) telemetryExists(ctx context.Context, tenantID, deviceID, messageID string) (bool, error) {
	query := fmt.Sprintf(`SELECT count() AS total FROM iot_telemetry WHERE tenant_id=%s AND device_id=%s AND message_id=%s FORMAT JSONEachRow`, quote(tenantID), quote(deviceID), quote(messageID))
	body, err := r.query(ctx, query, nil)
	if err != nil {
		return false, err
	}
	var result struct {
		Total int64 `json:"total"`
	}
	if err = json.Unmarshal(bytes.TrimSpace(body), &result); err != nil {
		return false, fmt.Errorf("decode clickhouse telemetry existence: %w", err)
	}
	return result.Total > 0, nil
}

func (r *Repository) SaveRawMessage(ctx context.Context, v model.RawMessage) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	row := map[string]any{"tenant_id": v.TenantID, "message_id": v.MessageID, "product_id": v.ProductID, "device_id": v.DeviceID, "protocol": v.Protocol, "payload_format": v.PayloadFormat, "payload_hash": v.PayloadHash(), "payload_size": len(v.Payload), "received_at": v.ReceivedAt, "body": string(body)}
	data, _ := json.Marshal(row)
	data = append(data, '\n')
	err = r.batches().raw.add(ctx, data)
	return err
}

func (r *Repository) GetRawMessage(ctx context.Context, tenant, messageID string) (model.RawMessage, error) {
	return r.GetDeviceRawMessage(ctx, tenant, "", messageID)
}

// GetDeviceRawMessage reads one raw message; a known device ID narrows the
// read to that device's range of the sort key instead of the whole tenant.
func (r *Repository) GetDeviceRawMessage(ctx context.Context, tenant, device, messageID string) (model.RawMessage, error) {
	filter := fmt.Sprintf("tenant_id=%s AND message_id=%s", quote(tenant), quote(messageID))
	if device != "" {
		filter = fmt.Sprintf("tenant_id=%s AND device_id=%s AND message_id=%s", quote(tenant), quote(device), quote(messageID))
	}
	data, err := r.query(ctx, `SELECT body FROM iot_raw_message WHERE `+filter+` LIMIT 1 FORMAT JSONEachRow`, nil)
	if err != nil {
		return model.RawMessage{}, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var row struct {
			Body string `json:"body"`
		}
		if err = json.Unmarshal([]byte(line), &row); err != nil {
			return model.RawMessage{}, err
		}
		if row.Body == "" {
			continue
		}
		var value model.RawMessage
		if err = json.Unmarshal([]byte(row.Body), &value); err != nil {
			return model.RawMessage{}, err
		}
		return value, nil
	}
	return model.RawMessage{}, fmt.Errorf("raw message not found")
}

func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage) (bool, bool, error) {
	shouldProcess, created, err := r.Repository.ClaimStandardMessage(ctx, v)
	if err != nil || !shouldProcess {
		return shouldProcess, created, err
	}
	err = r.ensureTelemetry(ctx, v, created)
	return shouldProcess, created, err
}

func (r *Repository) Health(ctx context.Context) error {
	if err := r.Repository.Health(ctx); err != nil {
		return err
	}
	_, err := r.query(ctx, "SELECT 1", nil)
	return err
}
func (r *Repository) PropertyHistory(ctx context.Context, tenant, device, property string, start, end int64, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 1000
	}
	if end <= 0 {
		end = time.Now().UnixMilli()
	}
	if !propertyCodePattern.MatchString(property) {
		return r.Repository.PropertyHistory(ctx, tenant, device, property, start, end, limit)
	}
	safeProperty := property
	q := fmt.Sprintf(`SELECT toUnixTimestamp64Milli(ts) AS timestamp, properties.%s AS value, message_id AS messageId FROM iot_telemetry WHERE tenant_id=%s AND device_id=%s AND ts >= fromUnixTimestamp64Milli(%d) AND ts <= fromUnixTimestamp64Milli(%d) ORDER BY ts DESC LIMIT %d FORMAT JSONEachRow`, safeProperty, quote(tenant), quote(device), start, end, limit)
	b, err := r.query(ctx, q, nil)
	if err != nil {
		return r.Repository.PropertyHistory(ctx, tenant, device, property, start, end, limit)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	out := []map[string]any{}
	for i := len(lines) - 1; i >= 0; i-- {
		var v map[string]any
		if lines[i] != "" && json.Unmarshal([]byte(lines[i]), &v) == nil {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *Repository) PropertyHistoryPage(ctx context.Context, tenant, device, property string, start, end int64, limit, offset int) ([]map[string]any, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if end <= 0 {
		end = time.Now().UnixMilli()
	}
	if !propertyCodePattern.MatchString(property) {
		return r.Repository.PropertyHistoryPage(ctx, tenant, device, property, start, end, limit, offset)
	}
	where := fmt.Sprintf(`tenant_id=%s AND device_id=%s AND ts >= fromUnixTimestamp64Milli(%d) AND ts <= fromUnixTimestamp64Milli(%d)`, quote(tenant), quote(device), start, end)
	countBody, err := r.query(ctx, fmt.Sprintf(`SELECT count() AS total FROM iot_telemetry WHERE %s FORMAT JSONEachRow`, where), nil)
	if err != nil {
		return r.Repository.PropertyHistoryPage(ctx, tenant, device, property, start, end, limit, offset)
	}
	var countRow struct {
		Total int `json:"total"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(countBody)), "\n") {
		if line != "" && json.Unmarshal([]byte(line), &countRow) == nil {
			break
		}
	}
	data, err := r.query(ctx, fmt.Sprintf(`SELECT toUnixTimestamp64Milli(ts) AS timestamp, properties.%s AS value, message_id AS messageId FROM iot_telemetry WHERE %s ORDER BY ts DESC, message_id DESC LIMIT %d OFFSET %d FORMAT JSONEachRow`, property, where, limit, offset), nil)
	if err != nil {
		return r.Repository.PropertyHistoryPage(ctx, tenant, device, property, start, end, limit, offset)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	items := make([]map[string]any, 0, limit)
	for i := len(lines) - 1; i >= 0; i-- {
		var item map[string]any
		if lines[i] != "" && json.Unmarshal([]byte(lines[i]), &item) == nil {
			items = append(items, item)
		}
	}
	return items, countRow.Total, nil
}
func (r *Repository) query(ctx context.Context, q string, body []byte) ([]byte, error) {
	u, err := url.Parse(r.base)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	params.Set("query", q)
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("clickhouse %s: %s", resp.Status, string(out))
	}
	return out, nil
}

type batchers struct{ telemetry, raw *insertBatcher }

// batches lazily creates one batcher per table so concurrent writes share
// INSERT requests.
func (r *Repository) batches() batchers {
	r.batchOnce.Do(func() {
		r.telemetryBatch = newInsertBatcher(func(ctx context.Context, body []byte) error {
			_, err := r.query(ctx, "INSERT INTO iot_telemetry FORMAT JSONEachRow", body)
			return err
		})
		r.rawBatch = newInsertBatcher(func(ctx context.Context, body []byte) error {
			_, err := r.query(ctx, "INSERT INTO iot_raw_message FORMAT JSONEachRow", body)
			return err
		})
	})
	return batchers{telemetry: r.telemetryBatch, raw: r.rawBatch}
}

func quote(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
