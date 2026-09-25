package observability

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Loki talks to the Loki HTTP API. Log rules and retention overrides are
// managed through files (the local ruler store does not accept API writes).
type Loki struct {
	c      *client
	tenant string
}

func NewLoki(baseURL, tenant string, timeout time.Duration) *Loki {
	l := &Loki{c: newClient(baseURL, timeout), tenant: tenant}
	// Separate stream labels from structured metadata and parsed labels so
	// context lookups can rebuild an exact stream selector.
	l.c.headers["X-Loki-Response-Encoding-Flags"] = "categorize-labels"
	if tenant != "" {
		l.c.headers["X-Scope-OrgID"] = tenant
	}
	return l
}

func (l *Loki) Configured() bool { return l != nil && l.c.configured() }

type lokiEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  string          `json:"error"`
}

func (l *Loki) Status(ctx context.Context) model.OpsComponentStatus {
	status := model.OpsComponentStatus{ID: "loki", Name: "Loki", Configured: l.Configured(), CheckedAt: time.Now().UnixMilli()}
	if !status.Configured {
		status.State, status.Message = "unconfigured", "未配置 Loki 地址"
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, _, err := l.c.do(ctx, request{method: http.MethodGet, path: "/ready", accept: "text/plain"})
	if err != nil {
		status.State, status.Message = "down", describe(err)
		var upstream *ports.OpsUpstreamError
		if errors.As(err, &upstream) && upstream.Status == http.StatusServiceUnavailable {
			status.State, status.Message = "degraded", "Loki 正在启动："+strings.TrimSpace(string(body))
		}
		return status
	}
	status.State = "ok"
	var build struct {
		Version string `json:"version"`
	}
	if err := l.c.getJSON(ctx, "/loki/api/v1/status/buildinfo", nil, &build); err == nil {
		status.Version = build.Version
	}
	if state, err := l.metricsState(ctx); err == nil {
		status.Details = map[string]any{"rulerReloadSuccess": state.rulerOK, "runtimeConfigReloadSuccess": state.runtime.Success}
		if !state.rulerOK || !state.runtime.Success {
			status.State, status.Message = "degraded", "日志规则或运行时配置加载失败"
		}
	}
	return status
}

func (l *Loki) Query(ctx context.Context, q ports.LogQuery) (model.LogQueryResult, error) {
	values := url.Values{"query": {q.Query}}
	result := model.LogQueryResult{Query: q.Query, Limit: q.Limit, Direction: q.Direction, Entries: []model.LogEntry{}}
	if q.Limit > 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Direction != "" {
		values.Set("direction", q.Direction)
	}
	path := "/loki/api/v1/query_range"
	if q.Instant {
		path = "/loki/api/v1/query"
		values.Set("time", strconv.FormatInt(q.End.UnixNano(), 10))
		result.End = q.End.UnixMilli()
	} else {
		values.Set("start", strconv.FormatInt(q.Start.UnixNano(), 10))
		values.Set("end", strconv.FormatInt(q.End.UnixNano(), 10))
		if q.Step > 0 {
			values.Set("step", strconv.FormatFloat(q.Step.Seconds(), 'f', -1, 64))
		}
		result.Start, result.End = q.Start.UnixMilli(), q.End.UnixMilli()
	}
	var env lokiEnvelope
	if err := l.c.getJSON(ctx, path, values, &env); err != nil {
		return result, err
	}
	var data struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
		Stats      struct {
			Summary map[string]any `json:"summary"`
		} `json:"stats"`
	}
	if err := decodeJSON(env.Data, &data); err != nil {
		return result, err
	}
	result.ResultType = data.ResultType
	if len(data.Stats.Summary) > 0 {
		result.Stats = map[string]any{}
		for _, key := range []string{"bytesProcessedPerSecond", "linesProcessedPerSecond", "totalBytesProcessed", "totalLinesProcessed", "execTime", "totalEntriesReturned"} {
			if v, ok := data.Stats.Summary[key]; ok {
				result.Stats[key] = v
			}
		}
	}
	if data.ResultType != "streams" {
		series, err := parsePromResult(data.ResultType, data.Result)
		result.Series = series
		return result, err
	}
	var streams []lokiStream
	if err := decodeJSON(data.Result, &streams); err != nil {
		return result, err
	}
	for _, s := range streams {
		result.Entries = append(result.Entries, s.entries()...)
	}
	sortEntries(result.Entries, q.Direction != "forward")
	if q.Limit > 0 && len(result.Entries) >= q.Limit {
		result.Truncated = true
		edge := result.Entries[len(result.Entries)-1].Timestamp
		if ns, err := strconv.ParseInt(edge, 10, 64); err == nil {
			// Loki's end bound is exclusive and its start bound inclusive; move one
			// nanosecond past the edge so entries sharing the edge timestamp are
			// returned again and deduplicated by the caller instead of skipped.
			if q.Direction == "forward" {
				result.NextCursor = strconv.FormatInt(ns, 10)
			} else {
				result.NextCursor = strconv.FormatInt(ns+1, 10)
			}
		}
	}
	return result, nil
}

type lokiStream struct {
	Stream map[string]string   `json:"stream"`
	Values [][]json.RawMessage `json:"values"`
}

// entries accepts both [ts, line] and the categorized [ts, line, {...}] form.
func (s lokiStream) entries() []model.LogEntry {
	out := make([]model.LogEntry, 0, len(s.Values))
	for _, v := range s.Values {
		if len(v) < 2 {
			continue
		}
		var ts, line string
		if json.Unmarshal(v[0], &ts) != nil || json.Unmarshal(v[1], &line) != nil {
			continue
		}
		ns, _ := strconv.ParseInt(ts, 10, 64)
		entry := model.LogEntry{Timestamp: ts, TimeMs: ns / int64(time.Millisecond), Line: line, Labels: nonNilLabels(s.Stream)}
		if len(v) > 2 {
			var extra struct {
				StructuredMetadata map[string]string `json:"structuredMetadata"`
				Parsed             map[string]string `json:"parsed"`
			}
			if json.Unmarshal(v[2], &extra) == nil {
				entry.Metadata, entry.Parsed = extra.StructuredMetadata, extra.Parsed
			}
		}
		out = append(out, entry)
	}
	return out
}

func sortEntries(entries []model.LogEntry, newestFirst bool) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, _ := strconv.ParseInt(entries[i].Timestamp, 10, 64)
		b, _ := strconv.ParseInt(entries[j].Timestamp, 10, 64)
		if newestFirst {
			return a > b
		}
		return a < b
	})
}

func (l *Loki) FormatQuery(ctx context.Context, query string) (string, error) {
	var env lokiEnvelope
	data, _, err := l.c.do(ctx, request{method: http.MethodPost, path: "/loki/api/v1/format_query", rawBody: []byte(url.Values{"query": {query}}.Encode()), contentType: "application/x-www-form-urlencoded", timeout: 10 * time.Second})
	if err != nil {
		return "", err
	}
	if err := decodeJSON(data, &env); err != nil {
		return "", err
	}
	var formatted string
	_ = json.Unmarshal(env.Data, &formatted)
	return formatted, nil
}

func (l *Loki) LabelNames(ctx context.Context, start, end time.Time) ([]string, error) {
	values := url.Values{}
	lokiRange(values, start, end)
	var env struct {
		Data []string `json:"data"`
	}
	err := l.c.getJSON(ctx, "/loki/api/v1/labels", values, &env)
	return env.Data, err
}

func (l *Loki) LabelValues(ctx context.Context, name, selector string, start, end time.Time) ([]string, error) {
	values := url.Values{}
	lokiRange(values, start, end)
	if selector != "" {
		values.Set("query", selector)
	}
	var env struct {
		Data []string `json:"data"`
	}
	err := l.c.getJSON(ctx, "/loki/api/v1/label/"+url.PathEscape(name)+"/values", values, &env)
	return env.Data, err
}

func lokiRange(values url.Values, start, end time.Time) {
	if !start.IsZero() {
		values.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	}
	if !end.IsZero() {
		values.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	}
}

// Tail relays Loki's WebSocket tail stream. emit receives each batch and the
// number of entries Loki reported as dropped; returning an error stops tailing.
func (l *Loki) Tail(ctx context.Context, query string, start time.Time, limit int, emit func([]model.LogEntry, int) error) error {
	if !l.Configured() {
		return ports.ErrOpsNotConfigured
	}
	target, err := url.Parse(l.c.base + "/loki/api/v1/tail")
	if err != nil {
		return err
	}
	switch target.Scheme {
	case "https":
		target.Scheme = "wss"
	default:
		target.Scheme = "ws"
	}
	values := url.Values{"query": {query}, "limit": {strconv.Itoa(limit)}, "delay_for": {"1"}}
	if !start.IsZero() {
		values.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	}
	target.RawQuery = values.Encode()
	header := http.Header{}
	for key, value := range l.c.headers {
		header.Set(key, value)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	conn, resp, err := dialer.DialContext(ctx, target.String(), header)
	if err != nil {
		if resp != nil {
			var body bytes.Buffer
			_, _ = body.ReadFrom(resp.Body)
			resp.Body.Close()
			return upstreamError(resp.StatusCode, body.Bytes())
		}
		return networkError(ctx, err)
	}
	defer conn.Close()
	conn.SetReadLimit(16 << 20)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	for {
		var message struct {
			Streams        []lokiStream      `json:"streams"`
			DroppedEntries []json.RawMessage `json:"dropped_entries"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) && closeErr.Text != "" {
				return &ports.OpsUpstreamError{Status: http.StatusBadGateway, Kind: "tail_closed", Message: closeErr.Text}
			}
			return ports.ErrOpsUnavailable
		}
		entries := []model.LogEntry{}
		for _, s := range message.Streams {
			entries = append(entries, s.entries()...)
		}
		sortEntries(entries, false)
		if err := emit(entries, len(message.DroppedEntries)); err != nil {
			return err
		}
	}
}

func (l *Loki) RuleGroups(ctx context.Context) ([]model.OpsRuleGroup, error) {
	var env struct {
		Status string `json:"status"`
		Data   struct {
			Groups []promRuleGroup `json:"groups"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := l.c.getJSON(ctx, "/prometheus/api/v1/rules", nil, &env); err != nil {
		return nil, err
	}
	if env.Status != "success" {
		return nil, &ports.OpsUpstreamError{Status: http.StatusBadGateway, Message: env.Error}
	}
	return convertRuleGroups("loki", env.Data.Groups), nil
}

func (l *Loki) DeleteRequests(ctx context.Context) ([]model.LogDeleteRequest, error) {
	var items []struct {
		RequestID string  `json:"request_id"`
		StartTime float64 `json:"start_time"`
		EndTime   float64 `json:"end_time"`
		Query     string  `json:"query"`
		Status    string  `json:"status"`
		CreatedAt float64 `json:"created_at"`
	}
	if err := l.c.getJSON(ctx, "/loki/api/v1/delete", nil, &items); err != nil {
		return nil, err
	}
	out := make([]model.LogDeleteRequest, 0, len(items))
	for _, item := range items {
		out = append(out, model.LogDeleteRequest{RequestID: item.RequestID, Query: item.Query, Status: item.Status, StartTime: item.StartTime, EndTime: item.EndTime, CreatedAt: item.CreatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (l *Loki) CreateDeleteRequest(ctx context.Context, query string, start, end time.Time) error {
	values := url.Values{"query": {query}, "start": {strconv.FormatInt(start.Unix(), 10)}, "end": {strconv.FormatInt(end.Unix(), 10)}}
	_, _, err := l.c.do(ctx, request{method: http.MethodPost, path: "/loki/api/v1/delete", query: values})
	return err
}

func (l *Loki) CancelDeleteRequest(ctx context.Context, id string, force bool) error {
	values := url.Values{"request_id": {id}}
	if force {
		values.Set("force", "true")
	}
	_, _, err := l.c.do(ctx, request{method: http.MethodDelete, path: "/loki/api/v1/delete", query: values})
	return err
}

func (l *Loki) Limits(ctx context.Context) (ports.LokiLimits, error) {
	data, _, err := l.c.do(ctx, request{method: http.MethodGet, path: "/config", accept: "text/plain"})
	if err != nil {
		return ports.LokiLimits{}, err
	}
	var cfg struct {
		Compactor struct {
			RetentionEnabled          bool   `yaml:"retention_enabled"`
			DeleteRequestCancelPeriod string `yaml:"delete_request_cancel_period"`
		} `yaml:"compactor"`
		Limits struct {
			RetentionPeriod string `yaml:"retention_period"`
			DeletionMode    string `yaml:"deletion_mode"`
			MaxQueryLength  string `yaml:"max_query_length"`
			MaxEntries      int    `yaml:"max_entries_limit_per_query"`
		} `yaml:"limits_config"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ports.LokiLimits{}, &ports.OpsUpstreamError{Status: http.StatusBadGateway, Message: "无法解析 Loki 配置"}
	}
	return ports.LokiLimits{RetentionEnabled: cfg.Compactor.RetentionEnabled, DeletionMode: cfg.Limits.DeletionMode, GlobalPeriod: cfg.Limits.RetentionPeriod, CancelPeriod: cfg.Compactor.DeleteRequestCancelPeriod, MaxQueryLength: cfg.Limits.MaxQueryLength, MaxEntries: cfg.Limits.MaxEntries}, nil
}

type lokiMetricsState struct {
	runtime ports.LokiRuntimeState
	rulerOK bool
}

func (l *Loki) metricsState(ctx context.Context) (lokiMetricsState, error) {
	data, _, err := l.c.do(ctx, request{method: http.MethodGet, path: "/metrics", accept: "text/plain", timeout: 10 * time.Second})
	if err != nil {
		return lokiMetricsState{}, err
	}
	state := lokiMetricsState{rulerOK: true}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "loki_runtime_config_hash{"):
			if i := strings.Index(line, `sha256="`); i >= 0 {
				rest := line[i+8:]
				if j := strings.IndexByte(rest, '"'); j >= 0 {
					state.runtime.Hash = rest[:j]
				}
			}
		case strings.HasPrefix(line, "loki_runtime_config_last_reload_successful{"):
			state.runtime.Success = strings.HasSuffix(line, " 1")
		case strings.HasPrefix(line, "loki_ruler_config_last_reload_successful{"):
			state.rulerOK = state.rulerOK && strings.HasSuffix(line, " 1")
		}
	}
	return state, nil
}

func (l *Loki) RuntimeState(ctx context.Context) (ports.LokiRuntimeState, error) {
	state, err := l.metricsState(ctx)
	return state.runtime, err
}
