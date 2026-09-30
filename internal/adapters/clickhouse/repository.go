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
	"strconv"
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
	opts           Options
	batchOnce      sync.Once
	telemetryBatch *insertBatcher
	rawBatch       *insertBatcher
}

// Options select the storage topology. An empty Cluster keeps the single-node
// MergeTree tables. With a cluster name, each shard stores replicated
// *_local tables and the original table names become Distributed tables, so
// every query keeps its table name.
type Options struct {
	Cluster string
	// InsertQuorum is the replica acknowledgement required per insert
	// ("", "auto" or a number). Empty keeps ClickHouse's default (local write).
	InsertQuorum string
}

var clusterNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var quorumPattern = regexp.MustCompile(`^(auto|[1-9])$`)

// storageTables are the telemetry and raw payload tables with their columns
// and ordering; the definitions are shared by both topologies.
var storageTables = []struct{ name, columns, partition, order string }{
	{"iot_telemetry", "tenant_id String, device_id String, product_id String, message_id String, ts DateTime64(3), properties JSON", "toYYYYMM(ts)", "(tenant_id,device_id,ts,message_id)"},
	{"iot_raw_message", "tenant_id String, message_id String, product_id String, device_id String, protocol String, payload_format String, payload_hash String, payload_size UInt64, received_at Int64, body String", "toYYYYMM(fromUnixTimestamp64Milli(received_at))", "(tenant_id,device_id,received_at,message_id)"},
}

// SchemaStatements returns the DDL for a topology; exported so migration and
// cluster initialization tools create exactly what the platform expects.
func SchemaStatements(cluster string) []string {
	var out []string
	for _, t := range storageTables {
		if cluster == "" {
			out = append(out, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s) ENGINE=MergeTree PARTITION BY %s ORDER BY %s", t.name, t.columns, t.partition, t.order))
			// Lookups by message ID carry the device ID so the sort key
			// prunes; the bloom filter covers lookups that know only the
			// message ID. Existing parts get the index as they merge.
			out = append(out, "ALTER TABLE "+t.name+" ADD INDEX IF NOT EXISTS idx_message_id message_id TYPE bloom_filter(0.01) GRANULARITY 1")
			continue
		}
		local := t.name + "_local"
		out = append(out,
			fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s ON CLUSTER %s (%s) ENGINE=ReplicatedMergeTree('/clickhouse/tables/{shard}/{database}/%s','{replica}') PARTITION BY %s ORDER BY %s", local, cluster, t.columns, local, t.partition, t.order),
			"ALTER TABLE "+local+" ON CLUSTER "+cluster+" ADD INDEX IF NOT EXISTS idx_message_id message_id TYPE bloom_filter(0.01) GRANULARITY 1",
			// One device's rows stay on one shard, so per-device history
			// queries and ordering stay local to a shard.
			fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s ON CLUSTER %s AS %s ENGINE=Distributed(%s, currentDatabase(), %s, cityHash64(tenant_id, device_id))", t.name, cluster, local, cluster, local),
		)
	}
	return out
}

var propertyCodePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func New(ctx context.Context, base string, repo ports.Repository) (*Repository, error) {
	return NewWithOptions(ctx, base, repo, Options{})
}

func NewWithOptions(ctx context.Context, base string, repo ports.Repository, opts Options) (*Repository, error) {
	if opts.Cluster != "" && !clusterNamePattern.MatchString(opts.Cluster) {
		return nil, fmt.Errorf("invalid clickhouse cluster name %q", opts.Cluster)
	}
	if opts.InsertQuorum != "" && !quorumPattern.MatchString(opts.InsertQuorum) {
		return nil, fmt.Errorf("clickhouse insert quorum must be auto or 1-9")
	}
	r := &Repository{Repository: repo, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 30 * time.Second}, opts: opts}
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
		create := "CREATE DATABASE IF NOT EXISTS " + database
		if opts.Cluster != "" {
			create += " ON CLUSTER " + opts.Cluster
		}
		if _, err = bootstrap.query(ctx, create, nil); err != nil {
			return nil, err
		}
	}
	if opts.Cluster != "" {
		if err = r.checkClusterSchema(ctx); err != nil {
			return nil, err
		}
	}
	for _, statement := range SchemaStatements(opts.Cluster) {
		if _, err = r.query(ctx, statement, nil); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// checkClusterSchema refuses cluster mode on a database that still holds the
// single-node tables: CREATE ... IF NOT EXISTS would silently keep them.
func (r *Repository) checkClusterSchema(ctx context.Context) error {
	body, err := r.query(ctx, "SELECT name, engine FROM system.tables WHERE database=currentDatabase() AND name IN ('iot_telemetry','iot_raw_message') FORMAT JSONEachRow", nil)
	if err != nil {
		return err
	}
	for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
		var row struct{ Name, Engine string }
		if len(line) == 0 || json.Unmarshal(line, &row) != nil {
			continue
		}
		if row.Engine != "Distributed" {
			return fmt.Errorf("clickhouse table %s uses %s; migrate the single-node tables with cmd/clickhouse-migrate before enabling IOT_CLICKHOUSE_CLUSTER", row.Name, row.Engine)
		}
	}
	return nil
}

// insertStatement writes through the Distributed table synchronously, so the
// acknowledgement covers the shard replicas required by InsertQuorum instead
// of only the local forwarding queue.
func (r *Repository) insertStatement(table string) string {
	if r.opts.Cluster == "" {
		return "INSERT INTO " + table + " FORMAT JSONEachRow"
	}
	settings := []string{"insert_distributed_sync=1"}
	if r.opts.InsertQuorum != "" {
		quorum := r.opts.InsertQuorum
		if quorum == "auto" {
			quorum = "'auto'"
		}
		settings = append(settings, "insert_quorum="+quorum, "insert_quorum_parallel=1")
	}
	return "INSERT INTO " + table + " SETTINGS " + strings.Join(settings, ", ") + " FORMAT JSONEachRow"
}

// BatchStats reports insert batching for metrics.
func (r *Repository) BatchStats() BatchStats {
	b := r.batches()
	return b.telemetry.stats().add(b.raw.stats())
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
	if err == nil {
		if recorder, ok := r.Repository.(ports.MeasurementAvailabilityRecorder); ok {
			err = recorder.RecordMeasurementAvailability(ctx, v.TenantID, v.MessageID, "clickhouse_telemetry_ack")
		}
	}
	return err
}

func (r *Repository) telemetryExists(ctx context.Context, tenantID, deviceID, messageID string) (bool, error) {
	query := fmt.Sprintf(`SELECT count() AS total FROM iot_telemetry WHERE tenant_id=%s AND device_id=%s AND message_id=%s FORMAT JSONEachRow`, quote(tenantID), quote(deviceID), quote(messageID))
	body, err := r.query(ctx, query, nil)
	if err != nil {
		return false, err
	}
	total, err := decodeCount(body)
	return total > 0, err
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

func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage, owner string, lease time.Duration) (model.StandardClaim, error) {
	claim, err := r.Repository.ClaimStandardMessage(ctx, v, owner, lease)
	if err != nil || !claim.ShouldProcess {
		return claim, err
	}
	return claim, r.ensureTelemetry(ctx, v, claim.Created)
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
	total, err := decodeCount(countBody)
	if err != nil {
		return nil, 0, err
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
	return items, total, nil
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
			_, err := r.query(ctx, r.insertStatement("iot_telemetry"), body)
			return err
		})
		r.rawBatch = newInsertBatcher(func(ctx context.Context, body []byte) error {
			_, err := r.query(ctx, r.insertStatement("iot_raw_message"), body)
			return err
		})
	})
	return batchers{telemetry: r.telemetryBatch, raw: r.rawBatch}
}

func quote(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

// ClickHouse quotes UInt64 JSON counters by default. json.Number accepts both
// quoted and unquoted integers without the precision loss of float64. Missing,
// fractional, negative and overflowing counts are errors, never an empty page.
func decodeCount(body []byte) (int, error) {
	var row struct {
		Total json.Number `json:"total"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &row); err != nil {
		return 0, fmt.Errorf("decode clickhouse count: %w", err)
	}
	total, err := strconv.Atoi(row.Total.String())
	if err != nil || total < 0 {
		return 0, fmt.Errorf("invalid clickhouse count %q", row.Total)
	}
	return total, nil
}
