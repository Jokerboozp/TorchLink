package opscenter

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// LogSearchInput is shared by the structured search (Filter) and raw LogQL
// (Query). Which one is honored is decided by the caller's permission.
type LogSearchInput struct {
	Filter    LogFilter `json:"filter"`
	Query     string    `json:"query"`
	Start     int64     `json:"start"`
	End       int64     `json:"end"`
	Cursor    string    `json:"cursor"`
	Limit     int       `json:"limit"`
	Direction string    `json:"direction"`
	StepMs    int64     `json:"stepMs"`
	MaxPoints int       `json:"maxPoints"`
}

func (in LogSearchInput) logQL(raw bool) (string, error) {
	if raw {
		query := strings.TrimSpace(in.Query)
		return query, checkQueryText("query", query)
	}
	return in.Filter.LogQL()
}

func (s *Service) logWindow(in LogSearchInput) (time.Time, time.Time, error) {
	start, end, err := TimeRange(msTime(in.Start), msTime(in.End), s.Limits.MaxLogRange, s.now())
	if err != nil {
		return start, end, err
	}
	if in.Cursor != "" {
		ns, err := strconv.ParseInt(in.Cursor, 10, 64)
		if err != nil {
			return start, end, invalid("cursor", "分页位置无效")
		}
		cursor := time.Unix(0, ns)
		if in.Direction == "forward" {
			if cursor.After(start) {
				start = cursor
			}
		} else if cursor.Before(end) {
			end = cursor
		}
	}
	return start, end, nil
}

// SearchLogs runs a log or metric LogQL query within the configured range and
// line limits. Results report truncation and a cursor for the next page.
func (s *Service) SearchLogs(ctx context.Context, in LogSearchInput, raw bool) (model.LogQueryResult, error) {
	if err := requireBackend(s.Logs); err != nil {
		return model.LogQueryResult{}, err
	}
	query, err := in.logQL(raw)
	if err != nil {
		return model.LogQueryResult{}, err
	}
	start, end, err := s.logWindow(in)
	if err != nil {
		return model.LogQueryResult{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > s.Limits.MaxLogLines {
		limit = s.Limits.MaxLogLines
	}
	direction := "backward"
	if in.Direction == "forward" {
		direction = "forward"
	}
	result, err := s.Logs.Query(ctx, ports.LogQuery{Query: query, Start: start, End: end, Limit: limit, Direction: direction, Step: Step(start, end, time.Duration(in.StepMs)*time.Millisecond, in.MaxPoints)})
	if err != nil {
		return result, asQueryError(err)
	}
	if len(result.Series) > s.Limits.MaxSeries {
		result.Series, result.Truncated = result.Series[:s.Limits.MaxSeries], true
	}
	return result, nil
}

// LogVolume counts lines matching the structured filter per level over the
// search window; it backs the histogram above the log list.
func (s *Service) LogVolume(ctx context.Context, in LogSearchInput) (model.LogQueryResult, error) {
	if err := requireBackend(s.Logs); err != nil {
		return model.LogQueryResult{}, err
	}
	query, err := in.Filter.LogQL()
	if err != nil {
		return model.LogQueryResult{}, err
	}
	start, end, err := TimeRange(msTime(in.Start), msTime(in.End), s.Limits.MaxLogRange, s.now())
	if err != nil {
		return model.LogQueryResult{}, err
	}
	maxPoints := in.MaxPoints
	if maxPoints <= 0 || maxPoints > 300 {
		maxPoints = 120
	}
	step := Step(start, end, 0, maxPoints)
	if min := end.Sub(start) / time.Duration(maxPoints); step < min {
		step = min.Round(time.Second) + time.Second
	}
	volume := "sum by (level) (count_over_time(" + query + " [" + promDuration(step) + "]))"
	result, err := s.Logs.Query(ctx, ports.LogQuery{Query: volume, Start: start, End: end, Step: step, Limit: s.Limits.MaxSeries})
	return result, asQueryError(err)
}

func (s *Service) ValidateLogQL(ctx context.Context, query string) (string, error) {
	if err := requireBackend(s.Logs); err != nil {
		return "", err
	}
	if err := checkQueryText("query", query); err != nil {
		return "", err
	}
	formatted, err := s.Logs.FormatQuery(ctx, query)
	return formatted, asQueryError(err)
}

func (s *Service) LogLabels(ctx context.Context) ([]string, error) {
	if err := requireBackend(s.Logs); err != nil {
		return nil, err
	}
	labels, err := s.Logs.LabelNames(ctx, s.now().Add(-6*time.Hour), s.now())
	if labels == nil {
		labels = []string{}
	}
	return labels, err
}

func (s *Service) LogLabelValues(ctx context.Context, label string) ([]string, error) {
	if err := requireBackend(s.Logs); err != nil {
		return nil, err
	}
	if !validLabelName(label) {
		return nil, invalid("label", "标签名不合法")
	}
	values, err := s.Logs.LabelValues(ctx, label, "", s.now().Add(-6*time.Hour), s.now())
	if values == nil {
		values = []string{}
	}
	return values, err
}

type LogContext struct {
	Before []model.LogEntry `json:"before"`
	After  []model.LogEntry `json:"after"`
}

// LogContext returns neighbouring lines of the same stream around ts.
func (s *Service) LogContext(ctx context.Context, labels map[string]string, ts string, size int) (LogContext, error) {
	if err := requireBackend(s.Logs); err != nil {
		return LogContext{}, err
	}
	if len(labels) == 0 || len(labels) > 30 {
		return LogContext{}, invalid("labels", "缺少日志流标签")
	}
	selector, err := StreamSelector(labels)
	if err != nil {
		return LogContext{}, err
	}
	ns, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return LogContext{}, invalid("ts", "时间戳无效")
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	at := time.Unix(0, ns)
	before, err := s.Logs.Query(ctx, ports.LogQuery{Query: selector, Start: at.Add(-6 * time.Hour), End: at, Limit: size, Direction: "backward"})
	if err != nil {
		return LogContext{}, err
	}
	after, err := s.Logs.Query(ctx, ports.LogQuery{Query: selector, Start: at, End: at.Add(6 * time.Hour), Limit: size + 1, Direction: "forward"})
	if err != nil {
		return LogContext{}, err
	}
	return LogContext{Before: before.Entries, After: after.Entries}, nil
}

type LogExport struct {
	Content     []byte
	ContentType string
	FileName    string
	Lines       int
	Truncated   bool
}

// ExportLogs pages through Loki up to the export limit and renders JSON
// Lines, CSV or plain text. Truncation is always reported to the caller.
func (s *Service) ExportLogs(ctx context.Context, in LogSearchInput, raw bool, format string) (LogExport, error) {
	if err := requireBackend(s.Logs); err != nil {
		return LogExport{}, err
	}
	query, err := in.logQL(raw)
	if err != nil {
		return LogExport{}, err
	}
	start, end, err := s.logWindow(in)
	if err != nil {
		return LogExport{}, err
	}
	max := in.Limit
	if max <= 0 || max > s.Limits.MaxExportLines {
		max = s.Limits.MaxExportLines
	}
	entries := []model.LogEntry{}
	seen := map[string]bool{}
	truncated := false
	for len(entries) < max {
		page := max - len(entries)
		if page > 1000 {
			page = 1000
		}
		result, err := s.Logs.Query(ctx, ports.LogQuery{Query: query, Start: start, End: end, Limit: page, Direction: "backward"})
		if err != nil {
			return LogExport{}, asQueryError(err)
		}
		if result.ResultType != "" && result.ResultType != "streams" {
			return LogExport{}, invalid("query", "只能导出日志行查询，不能导出统计查询")
		}
		added := 0
		for _, e := range result.Entries {
			key := e.Timestamp + "\x00" + e.Line + "\x00" + fmt.Sprint(e.Labels)
			if !seen[key] && len(entries) < max {
				seen[key] = true
				entries = append(entries, e)
				added++
			}
		}
		if !result.Truncated || result.NextCursor == "" || added == 0 {
			break
		}
		ns, _ := strconv.ParseInt(result.NextCursor, 10, 64)
		end = time.Unix(0, ns)
		if len(entries) >= max {
			truncated = true
		}
	}
	var buf bytes.Buffer
	out := LogExport{Lines: len(entries), Truncated: truncated}
	stamp := s.now().Format("20060102-150405")
	switch format {
	case "csv":
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"time", "service", "level", "line", "labels"})
		for _, e := range entries {
			labels, _ := json.Marshal(e.Labels)
			_ = w.Write([]string{time.UnixMilli(e.TimeMs).Format(time.RFC3339Nano), e.Labels["service_name"], e.Labels["level"], csvSafe(e.Line), string(labels)})
		}
		w.Flush()
		out.ContentType, out.FileName = "text/csv; charset=utf-8", "logs-"+stamp+".csv"
		out.Content = append([]byte("\xef\xbb\xbf"), buf.Bytes()...)
		return out, nil
	case "txt":
		for _, e := range entries {
			fmt.Fprintf(&buf, "%s [%s] %s\n", time.UnixMilli(e.TimeMs).Format("2006-01-02 15:04:05.000"), e.Labels["service_name"], e.Line)
		}
		out.ContentType, out.FileName = "text/plain; charset=utf-8", "logs-"+stamp+".txt"
	default:
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		for _, e := range entries {
			_ = enc.Encode(map[string]any{"ts": e.Timestamp, "time": time.UnixMilli(e.TimeMs).Format(time.RFC3339Nano), "labels": e.Labels, "line": e.Line})
		}
		out.ContentType, out.FileName = "application/x-ndjson; charset=utf-8", "logs-"+stamp+".jsonl"
	}
	out.Content = buf.Bytes()
	return out, nil
}

// csvSafe prevents spreadsheet formula execution when exported logs are
// opened in Excel-like tools.
func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

// TailLogs streams new entries until ctx ends or maxDuration elapses.
func (s *Service) TailLogs(ctx context.Context, in LogSearchInput, raw bool, since time.Time, emit func([]model.LogEntry, int) error) error {
	if err := requireBackend(s.Logs); err != nil {
		return err
	}
	query, err := in.logQL(raw)
	if err != nil {
		return err
	}
	if since.IsZero() || since.Before(s.now().Add(-time.Hour)) {
		since = s.now().Add(-10 * time.Second)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	return asQueryError(s.Logs.Tail(ctx, query, since, 200, emit))
}

// Loki rule groups reuse the rule workflow in rules.go.

// Retention settings live in the Loki runtime overrides file, which Loki
// reloads periodically. Only this tenant's retention keys are changed; any
// other overrides in the file are preserved.

type RetentionInput struct {
	Revision string `json:"revision"`
	Period   string `json:"period"`
	Streams  []struct {
		Matchers []Matcher `json:"matchers"`
		Selector string    `json:"selector"`
		Priority int       `json:"priority"`
		Period   string    `json:"period"`
	} `json:"streams"`
}

func (s *Service) lokiTenantKey(tenant string) string {
	if tenant == "" {
		return "fake"
	}
	return tenant
}

func (s *Service) Retention(ctx context.Context, lokiTenant string) (model.LogRetentionSettings, error) {
	if err := requireBackend(s.Logs); err != nil {
		return model.LogRetentionSettings{}, err
	}
	out := model.LogRetentionSettings{Streams: []model.LogRetentionStream{}, Writable: configured(s.LokiRuntime)}
	if limits, err := s.Logs.Limits(ctx); err == nil {
		out.RetentionEnabled, out.DeletionMode, out.GlobalPeriod, out.CancelPeriod, out.MaxQueryLength = limits.RetentionEnabled, limits.DeletionMode, limits.GlobalPeriod, limits.CancelPeriod, limits.MaxQueryLength
	}
	if state, err := s.Logs.RuntimeState(ctx); err == nil {
		loaded := state.Success
		out.Loaded = &loaded
	}
	if !configured(s.LokiRuntime) {
		return out, nil
	}
	file, err := s.LokiRuntime.Read()
	if err != nil {
		return out, err
	}
	out.Revision = file.Revision
	out.UpdatedBy = headerValue(file.Content, "updated-by")
	if t, err := time.Parse(time.RFC3339Nano, headerValue(file.Content, "updated-at")); err == nil {
		out.UpdatedAt = t.UnixMilli()
	}
	var doc struct {
		Overrides map[string]struct {
			RetentionPeriod string                     `yaml:"retention_period"`
			RetentionStream []model.LogRetentionStream `yaml:"retention_stream"`
		} `yaml:"overrides"`
	}
	if err := yaml.Unmarshal(file.Content, &doc); err != nil {
		return out, fmt.Errorf("parse loki runtime overrides: %w", err)
	}
	if tenant, ok := doc.Overrides[s.lokiTenantKey(lokiTenant)]; ok {
		out.Period = tenant.RetentionPeriod
		for _, stream := range tenant.RetentionStream {
			if matchers, err := ParseSelector(stream.Selector); err == nil {
				for _, m := range matchers {
					stream.Matchers = append(stream.Matchers, model.LabelMatcher(m))
				}
			}
			out.Streams = append(out.Streams, stream)
		}
	}
	return out, nil
}

func (s *Service) SaveRetention(ctx context.Context, lokiTenant string, in RetentionInput, actor string) (model.LogRetentionSettings, error) {
	if err := requireBackend(s.Logs); err != nil {
		return model.LogRetentionSettings{}, err
	}
	if !configured(s.LokiRuntime) {
		return model.LogRetentionSettings{}, ports.ErrOpsReadOnly
	}
	limits, err := s.Logs.Limits(ctx)
	if err != nil {
		return model.LogRetentionSettings{}, err
	}
	if !limits.RetentionEnabled {
		return model.LogRetentionSettings{}, invalid("period", "Loki 未启用 compactor 保留策略（compactor.retention_enabled），保留设置不会生效")
	}
	tenantValues := map[string]any{}
	if in.Period != "" {
		if d, ok := parseDuration(in.Period); !ok || d < 24*time.Hour {
			return model.LogRetentionSettings{}, invalid("period", "保留时长至少 24h，例如 168h、30d")
		}
		tenantValues["retention_period"] = in.Period
	}
	if len(in.Streams) > 50 {
		return model.LogRetentionSettings{}, invalid("streams", "按日志流设置的保留规则最多 50 条")
	}
	streams := []map[string]any{}
	for i, stream := range in.Streams {
		selector, err := Selector("", stream.Matchers)
		if err != nil {
			return model.LogRetentionSettings{}, invalid(fmt.Sprintf("streams[%d]", i), "日志流条件无效：%s", err.Error())
		}
		if d, ok := parseDuration(stream.Period); !ok || d < 24*time.Hour {
			return model.LogRetentionSettings{}, invalid(fmt.Sprintf("streams[%d].period", i), "保留时长至少 24h")
		}
		if stream.Priority < 0 || stream.Priority > 1000 {
			return model.LogRetentionSettings{}, invalid(fmt.Sprintf("streams[%d].priority", i), "优先级范围 0～1000")
		}
		streams = append(streams, map[string]any{"selector": selector, "priority": stream.Priority, "period": stream.Period})
	}
	if len(streams) > 0 {
		tenantValues["retention_stream"] = streams
	}
	s.rtMu.Lock()
	defer s.rtMu.Unlock()
	file, err := s.LokiRuntime.Read()
	if err != nil {
		return model.LogRetentionSettings{}, err
	}
	if in.Revision != "" && in.Revision != file.Revision {
		return model.LogRetentionSettings{}, ports.ErrOpsConflict
	}
	doc, original := map[string]any{}, map[string]any{}
	if len(bytes.TrimSpace(stripHeader(file.Content))) > 0 {
		if err := yaml.Unmarshal(file.Content, &doc); err != nil {
			return model.LogRetentionSettings{}, &ApplyError{Message: "现有 Loki 运行时配置无法解析，已停止修改", Detail: err.Error()}
		}
		_ = yaml.Unmarshal(file.Content, &original)
	}
	overrides, _ := doc["overrides"].(map[string]any)
	if overrides == nil {
		overrides = map[string]any{}
	}
	key := s.lokiTenantKey(lokiTenant)
	existing, _ := overrides[key].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	delete(existing, "retention_period")
	delete(existing, "retention_stream")
	for k, v := range tenantValues {
		existing[k] = v
	}
	if len(existing) == 0 {
		delete(overrides, key)
	} else {
		overrides[key] = existing
	}
	doc["overrides"] = overrides
	body, err := yaml.Marshal(doc)
	if err != nil {
		return model.LogRetentionSettings{}, err
	}
	// Loki's config hash only changes when the effective values change, so an
	// unchanged save must not wait for a reload that will never be reported.
	if previous, err := yaml.Marshal(original); (err == nil && bytes.Equal(previous, body)) || (len(overrides) == 0 && len(original) == 0) {
		return s.Retention(ctx, lokiTenant)
	}
	before, _ := s.Logs.RuntimeState(ctx)
	if err := s.LokiRuntime.Write(withHeader(body, actor, s.now())); err != nil {
		return model.LogRetentionSettings{}, err
	}
	if err := s.waitRuntimeApplied(ctx, before); err != nil {
		_ = s.LokiRuntime.Write(withHeader(file.Content, actor+" (rollback)", s.now()))
		return model.LogRetentionSettings{}, &ApplyError{Message: "Loki 未确认加载新的保留设置，已恢复原配置", Detail: err.Error(), RolledBack: true}
	}
	return s.Retention(ctx, lokiTenant)
}

func (s *Service) waitRuntimeApplied(ctx context.Context, before ports.LokiRuntimeState) error {
	timeout := s.Limits.ReloadTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(s.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("Loki 在 %s 内未重新加载运行时配置（请确认 runtime_config.period 不超过 15s 且文件已挂载）", humanDuration(timeout))
		case <-ticker.C:
		}
		state, err := s.Logs.RuntimeState(ctx)
		if err != nil {
			continue
		}
		if !state.Success {
			return fmt.Errorf("Loki 拒绝加载新的运行时配置")
		}
		if state.Hash != "" && state.Hash != before.Hash {
			return nil
		}
	}
}

type DeleteRequestInput struct {
	Filter LogFilter `json:"filter"`
	Start  int64     `json:"start"`
	End    int64     `json:"end"`
}

func (s *Service) LogDeleteRequests(ctx context.Context) ([]model.LogDeleteRequest, error) {
	if err := requireBackend(s.Logs); err != nil {
		return nil, err
	}
	return s.Logs.DeleteRequests(ctx)
}

// CreateLogDeleteRequest submits a Loki delete request built from the
// structured filter. At least one explicit service or label is required so a
// request can never target every stream by accident.
func (s *Service) CreateLogDeleteRequest(ctx context.Context, in DeleteRequestInput) (string, error) {
	if err := requireBackend(s.Logs); err != nil {
		return "", err
	}
	if len(cleanValues(in.Filter.Services)) == 0 && len(in.Filter.Labels) == 0 {
		return "", invalid("filter", "删除日志必须至少指定一个服务或标签条件")
	}
	query, err := in.Filter.LogQL()
	if err != nil {
		return "", err
	}
	start, end := msTime(in.Start), msTime(in.End)
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return "", invalid("range", "请指定有效的开始和结束时间")
	}
	if end.After(s.now()) {
		end = s.now()
	}
	if _, err := s.Logs.FormatQuery(ctx, query); err != nil {
		return "", asQueryError(err)
	}
	return query, asQueryError(s.Logs.CreateDeleteRequest(ctx, query, start, end))
}

func (s *Service) CancelLogDeleteRequest(ctx context.Context, id string, force bool) error {
	if err := requireBackend(s.Logs); err != nil {
		return err
	}
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/?&#") {
		return invalid("id", "删除请求编号无效")
	}
	return s.Logs.CancelDeleteRequest(ctx, id, force)
}
