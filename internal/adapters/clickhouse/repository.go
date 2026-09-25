package clickhouse /* 声明 clickhouse 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"
	"time" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Repository decorates durable business storage with ClickHouse telemetry writes and queries.
type Repository struct { /* 定义 Repository 类型。 */
	ports.Repository              /* 执行当前语句并推进处理流程。 */
	base             string       /* 执行当前语句并推进处理流程。 */
	http             *http.Client /* 执行当前语句并推进处理流程。 */
	batchOnce        sync.Once
	telemetryBatch   *insertBatcher
	rawBatch         *insertBatcher
} /* 结束当前表达式或代码块。 */

var propertyCodePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`) /* 声明 propertyCodePattern。 */

func New(ctx context.Context, base string, repo ports.Repository) (*Repository, error) { /* 定义 New 函数。 */
	r := &Repository{Repository: repo, base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 30 * time.Second}} /* 更新 r 的值。 */
	u, err := url.Parse(r.base)                                                                                           /* 更新 err 的值。 */
	if err != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	database := u.Query().Get("database") /* 更新 database 的值。 */
	if database != "" {                   /* 判断条件并选择处理分支。 */
		if !propertyCodePattern.MatchString(database) { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("invalid clickhouse database name %q", database) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		bootstrapURL := *u                                                                                            /* 更新 bootstrapURL 的值。 */
		params := bootstrapURL.Query()                                                                                /* 更新 params 的值。 */
		params.Del("database")                                                                                        /* 执行当前语句并推进处理流程。 */
		bootstrapURL.RawQuery = params.Encode()                                                                       /* 更新 bootstrapURL.RawQuery 的值。 */
		bootstrap := &Repository{Repository: repo, base: strings.TrimRight(bootstrapURL.String(), "/"), http: r.http} /* 更新 bootstrap 的值。 */
		if _, err = bootstrap.query(ctx, "CREATE DATABASE IF NOT EXISTS "+database, nil); err != nil {                /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	schema := `CREATE TABLE IF NOT EXISTS iot_telemetry (tenant_id String, device_id String, product_id String, message_id String, ts DateTime64(3), properties JSON) ENGINE=MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (tenant_id,device_id,ts,message_id)` /* 更新 schema 的值。 */
	if _, err = r.query(ctx, schema, nil); err != nil {                                                                                                                                                                                                        /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rawSchema := `CREATE TABLE IF NOT EXISTS iot_raw_message (tenant_id String, message_id String, product_id String, device_id String, protocol String, payload_format String, payload_hash String, payload_size UInt64, received_at Int64, body String) ENGINE=MergeTree PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(received_at)) ORDER BY (tenant_id,device_id,received_at,message_id)` /* 更新 rawSchema 的值。 */
	if _, err = r.query(ctx, rawSchema, nil); err != nil {                                                                                                                                                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error { /* 定义 SaveStandardMessage 函数。 */
	_, err := r.SaveStandardMessageIfAbsent(ctx, v) /* 更新 err 的值。 */
	return err                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessageIfAbsent(ctx context.Context, v model.StandardMessage) (bool, error) { /* 定义 SaveStandardMessageIfAbsent 函数。 */
	created, err := r.Repository.SaveStandardMessageIfAbsent(ctx, v) /* 更新 err 的值。 */
	if err != nil {                                                  /* 判断条件并选择处理分支。 */
		return created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = r.ensureTelemetry(ctx, v, created); err != nil { /* 判断条件并选择处理分支。 */
		return created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return created, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func telemetryMessage(v model.StandardMessage) bool { /* 定义 telemetryMessage 函数。 */
	return v.MessageType == model.PropertyReport || v.MessageType == model.AlarmReport /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) ensureTelemetry(ctx context.Context, v model.StandardMessage, created bool) error { /* 定义 ensureTelemetry 函数。 */
	if !telemetryMessage(v) { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !created { /* 判断条件并选择处理分支。 */
		exists, err := r.telemetryExists(ctx, v.TenantID, v.MessageID) /* 更新 err 的值。 */
		if err != nil {                                                /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if exists { /* 判断条件并选择处理分支。 */
			return nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	row := map[string]any{"tenant_id": v.TenantID, "device_id": v.DeviceID, "product_id": v.ProductID, "message_id": v.MessageID, "ts": time.UnixMilli(v.Timestamp).UTC().Format("2006-01-02 15:04:05.000"), "properties": v.Properties} /* 更新 row 的值。 */
	b, _ := json.Marshal(row)                                                                                                                                                                                                            /* 更新 _ 的值。 */
	b = append(b, '\n')                                                                                                                                                                                                                  /* 更新 b 的值。 */
	err := r.batches().telemetry.add(ctx, b)                                                                                                                                                                                             /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) telemetryExists(ctx context.Context, tenantID, messageID string) (bool, error) { /* 定义 telemetryExists 函数。 */
	query := fmt.Sprintf(`SELECT count() AS total FROM iot_telemetry WHERE tenant_id=%s AND message_id=%s FORMAT JSONEachRow`, quote(tenantID), quote(messageID)) /* 更新 query 的值。 */
	body, err := r.query(ctx, query, nil)                                                                                                                         /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                               /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var result struct { /* 声明 result。 */
		Total int64 `json:"total"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(bytes.TrimSpace(body), &result); err != nil { /* 判断条件并选择处理分支。 */
		return false, fmt.Errorf("decode clickhouse telemetry existence: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return result.Total > 0, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveRawMessage(ctx context.Context, v model.RawMessage) error { /* 定义 SaveRawMessage 函数。 */
	body, err := json.Marshal(v) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	row := map[string]any{"tenant_id": v.TenantID, "message_id": v.MessageID, "product_id": v.ProductID, "device_id": v.DeviceID, "protocol": v.Protocol, "payload_format": v.PayloadFormat, "payload_hash": v.PayloadHash(), "payload_size": len(v.Payload), "received_at": v.ReceivedAt, "body": string(body)} /* 更新 row 的值。 */
	data, _ := json.Marshal(row)                                                                                                                                                                                                                                                                                 /* 更新 _ 的值。 */
	data = append(data, '\n')                                                                                                                                                                                                                                                                                    /* 更新 data 的值。 */
	err = r.batches().raw.add(ctx, data)                                                                                                                                                                                                                                                                         /* 更新 err 的值。 */
	return err                                                                                                                                                                                                                                                                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetRawMessage(ctx context.Context, tenant, messageID string) (model.RawMessage, error) { /* 定义 GetRawMessage 函数。 */
	data, err := r.query(ctx, fmt.Sprintf(`SELECT body FROM iot_raw_message WHERE tenant_id=%s AND message_id=%s LIMIT 1 FORMAT JSONEachRow`, quote(tenant), quote(messageID)), nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") { /* 循环处理当前数据。 */
		if line == "" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var row struct { /* 声明 row。 */
			Body string `json:"body"` /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal([]byte(line), &row); err != nil { /* 判断条件并选择处理分支。 */
			return model.RawMessage{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if row.Body == "" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var value model.RawMessage                                      /* 声明 value。 */
		if err = json.Unmarshal([]byte(row.Body), &value); err != nil { /* 判断条件并选择处理分支。 */
			return model.RawMessage{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return value, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return model.RawMessage{}, fmt.Errorf("raw message not found") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) ClaimStandardMessage(ctx context.Context, v model.StandardMessage) (bool, bool, error) { /* 定义 ClaimStandardMessage 函数。 */
	shouldProcess, created, err := r.Repository.ClaimStandardMessage(ctx, v) /* 更新 err 的值。 */
	if err != nil || !shouldProcess {                                        /* 判断条件并选择处理分支。 */
		return shouldProcess, created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	err = r.ensureTelemetry(ctx, v, created) /* 更新 err 的值。 */
	return shouldProcess, created, err       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	if err := r.Repository.Health(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err := r.query(ctx, "SELECT 1", nil) /* 检查错误并决定后续处理。 */
	return err                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) PropertyHistory(ctx context.Context, tenant, device, property string, start, end int64, limit int) ([]map[string]any, error) { /* 定义 PropertyHistory 函数。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 1000 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if end <= 0 { /* 判断条件并选择处理分支。 */
		end = time.Now().UnixMilli() /* 更新 end 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !propertyCodePattern.MatchString(property) { /* 判断条件并选择处理分支。 */
		return r.Repository.PropertyHistory(ctx, tenant, device, property, start, end, limit) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	safeProperty := property                                                                                                                                                                                                                                                                                                                                             /* 更新 safeProperty 的值。 */
	q := fmt.Sprintf(`SELECT toUnixTimestamp64Milli(ts) AS timestamp, properties.%s AS value, message_id AS messageId FROM iot_telemetry WHERE tenant_id=%s AND device_id=%s AND ts >= fromUnixTimestamp64Milli(%d) AND ts <= fromUnixTimestamp64Milli(%d) ORDER BY ts DESC LIMIT %d FORMAT JSONEachRow`, safeProperty, quote(tenant), quote(device), start, end, limit) /* 更新 q 的值。 */
	b, err := r.query(ctx, q, nil)                                                                                                                                                                                                                                                                                                                                       /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		return r.Repository.PropertyHistory(ctx, tenant, device, property, start, end, limit) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	lines := strings.Split(strings.TrimSpace(string(b)), "\n") /* 更新 lines 的值。 */
	out := []map[string]any{}                                  /* 更新 out 的值。 */
	for i := len(lines) - 1; i >= 0; i-- {                     /* 循环处理当前数据。 */
		var v map[string]any                                               /* 声明 v。 */
		if lines[i] != "" && json.Unmarshal([]byte(lines[i]), &v) == nil { /* 判断条件并选择处理分支。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) PropertyHistoryPage(ctx context.Context, tenant, device, property string, start, end int64, limit, offset int) ([]map[string]any, int, error) { /* 定义 PropertyHistoryPage 函数。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 20 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if limit > 100 { /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset < 0 { /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	if end <= 0 { /* 判断条件并选择处理分支。 */
		end = time.Now().UnixMilli() /* 更新 end 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !propertyCodePattern.MatchString(property) { /* 判断条件并选择处理分支。 */
		return r.Repository.PropertyHistoryPage(ctx, tenant, device, property, start, end, limit, offset) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	where := fmt.Sprintf(`tenant_id=%s AND device_id=%s AND ts >= fromUnixTimestamp64Milli(%d) AND ts <= fromUnixTimestamp64Milli(%d)`, quote(tenant), quote(device), start, end) /* 更新 where 的值。 */
	countBody, err := r.query(ctx, fmt.Sprintf(`SELECT count() AS total FROM iot_telemetry WHERE %s FORMAT JSONEachRow`, where), nil)                                             /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                               /* 判断条件并选择处理分支。 */
		return r.Repository.PropertyHistoryPage(ctx, tenant, device, property, start, end, limit, offset) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var countRow struct { /* 声明 countRow。 */
		Total int `json:"total"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, line := range strings.Split(strings.TrimSpace(string(countBody)), "\n") { /* 循环处理当前数据。 */
		if line != "" && json.Unmarshal([]byte(line), &countRow) == nil { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	data, err := r.query(ctx, fmt.Sprintf(`SELECT toUnixTimestamp64Milli(ts) AS timestamp, properties.%s AS value, message_id AS messageId FROM iot_telemetry WHERE %s ORDER BY ts DESC, message_id DESC LIMIT %d OFFSET %d FORMAT JSONEachRow`, property, where, limit, offset), nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		return r.Repository.PropertyHistoryPage(ctx, tenant, device, property, start, end, limit, offset) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	lines := strings.Split(strings.TrimSpace(string(data)), "\n") /* 更新 lines 的值。 */
	items := make([]map[string]any, 0, limit)                     /* 更新 items 的值。 */
	for i := len(lines) - 1; i >= 0; i-- {                        /* 循环处理当前数据。 */
		var item map[string]any                                               /* 声明 item。 */
		if lines[i] != "" && json.Unmarshal([]byte(lines[i]), &item) == nil { /* 判断条件并选择处理分支。 */
			items = append(items, item) /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return items, countRow.Total, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) query(ctx context.Context, q string, body []byte) ([]byte, error) { /* 定义 query 函数。 */
	u, err := url.Parse(r.base) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	params := u.Query()                                                                             /* 更新 params 的值。 */
	params.Set("query", q)                                                                          /* 执行当前语句并推进处理流程。 */
	u.RawQuery = params.Encode()                                                                    /* 更新 u.RawQuery 的值。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body)) /* 更新 err 的值。 */
	if err != nil {                                                                                 /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := r.http.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                 /* 安排函数结束时执行清理。 */
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) /* 更新 _ 的值。 */
	if resp.StatusCode/100 != 2 {                           /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("clickhouse %s: %s", resp.Status, string(out)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
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

func quote(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" } /* 定义 quote 函数。 */
