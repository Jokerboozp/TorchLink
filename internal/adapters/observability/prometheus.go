package observability

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Prometheus talks to the Prometheus HTTP API (v1). Only read endpoints are
// used: rule changes go through managed rule files and automatic reload.
type Prometheus struct{ c *client }

func NewPrometheus(baseURL string, timeout time.Duration) *Prometheus {
	return &Prometheus{c: newClient(baseURL, timeout)}
}

func (p *Prometheus) Configured() bool { return p != nil && p.c.configured() }

type promEnvelope struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
	Infos     []string        `json:"infos"`
}

func (p *Prometheus) call(ctx context.Context, method, path string, values url.Values, timeout time.Duration, out any) (promEnvelope, error) {
	req := request{method: method, path: path, timeout: timeout}
	if method == http.MethodPost {
		req.rawBody = []byte(values.Encode())
		req.contentType = "application/x-www-form-urlencoded"
	} else {
		req.query = values
	}
	data, _, err := p.c.do(ctx, req)
	if err != nil {
		return promEnvelope{}, err
	}
	var env promEnvelope
	if err := decodeJSON(data, &env); err != nil {
		return env, err
	}
	if env.Status != "success" {
		return env, &ports.OpsUpstreamError{Status: http.StatusBadRequest, Kind: env.ErrorType, Message: env.Error}
	}
	if out != nil {
		if err := decodeJSON(env.Data, out); err != nil {
			return env, err
		}
	}
	return env, nil
}

func (p *Prometheus) Status(ctx context.Context) model.OpsComponentStatus {
	status := model.OpsComponentStatus{ID: "prometheus", Name: "Prometheus", Configured: p.Configured(), CheckedAt: time.Now().UnixMilli()}
	if !status.Configured {
		status.State, status.Message = "unconfigured", "未配置 Prometheus 地址"
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, _, err := p.c.do(ctx, request{method: http.MethodGet, path: "/-/ready", accept: "text/plain"}); err != nil {
		status.State, status.Message = "down", describe(err)
		return status
	}
	status.State = "ok"
	var build struct {
		Version string `json:"version"`
	}
	if _, err := p.call(ctx, http.MethodGet, "/api/v1/status/buildinfo", nil, 0, &build); err == nil {
		status.Version = build.Version
	}
	details := map[string]any{}
	if runtime, err := p.Runtime(ctx); err == nil {
		details["reloadConfigSuccess"] = runtime.ReloadSuccess
		details["lastConfigTime"] = runtime.LastConfig.UnixMilli()
		details["storageRetention"] = runtime.Retention
		if !runtime.ReloadSuccess {
			status.State, status.Message = "degraded", "最近一次配置或规则加载失败"
		}
	}
	if targets, err := p.Targets(ctx); err == nil {
		up := 0
		for _, target := range targets {
			if target.Health == "up" {
				up++
			}
		}
		details["targetsUp"], details["targetsTotal"] = up, len(targets)
		if up < len(targets) && status.State == "ok" {
			status.State, status.Message = "degraded", "部分采集目标异常"
		}
	}
	status.Details = details
	return status
}

func (p *Prometheus) Query(ctx context.Context, q ports.MetricQuery) (model.MetricQueryResult, error) {
	values := url.Values{"query": {q.Expr}}
	limit := q.Limit
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit+1))
	}
	if q.Timeout > 0 {
		values.Set("timeout", strconv.FormatFloat(q.Timeout.Seconds(), 'f', 0, 64)+"s")
	}
	path := "/api/v1/query"
	result := model.MetricQueryResult{Query: q.Expr, Limit: limit}
	if q.Instant {
		if !q.Time.IsZero() {
			values.Set("time", unixSeconds(q.Time))
			result.End = q.Time.UnixMilli()
		}
	} else {
		path = "/api/v1/query_range"
		values.Set("start", unixSeconds(q.Start))
		values.Set("end", unixSeconds(q.End))
		values.Set("step", strconv.FormatFloat(q.Step.Seconds(), 'f', -1, 64))
		result.Start, result.End, result.StepMs = q.Start.UnixMilli(), q.End.UnixMilli(), q.Step.Milliseconds()
	}
	var data struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	}
	timeout := time.Duration(0)
	if q.Timeout > 0 {
		timeout = q.Timeout + 2*time.Second
	}
	env, err := p.call(ctx, http.MethodPost, path, values, timeout, &data)
	if err != nil {
		return result, err
	}
	result.ResultType, result.Warnings, result.Infos = data.ResultType, env.Warnings, env.Infos
	series, err := parsePromResult(data.ResultType, data.Result)
	if err != nil {
		return result, err
	}
	if limit > 0 && len(series) > limit {
		series, result.Truncated = series[:limit], true
	}
	result.Series = series
	return result, nil
}

func parsePromResult(resultType string, raw json.RawMessage) ([]model.MetricSeries, error) {
	switch resultType {
	case "matrix":
		var items []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		}
		if err := decodeJSON(raw, &items); err != nil {
			return nil, err
		}
		out := make([]model.MetricSeries, 0, len(items))
		for _, item := range items {
			s := model.MetricSeries{Labels: nonNilLabels(item.Metric), Timestamps: make([]int64, 0, len(item.Values)), Values: make([]*float64, 0, len(item.Values))}
			for _, point := range item.Values {
				ts, value := promPoint(point)
				s.Timestamps, s.Values = append(s.Timestamps, ts), append(s.Values, value)
			}
			out = append(out, s)
		}
		return out, nil
	case "vector":
		var items []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		}
		if err := decodeJSON(raw, &items); err != nil {
			return nil, err
		}
		out := make([]model.MetricSeries, 0, len(items))
		for _, item := range items {
			ts, value := promPoint(item.Value)
			out = append(out, model.MetricSeries{Labels: nonNilLabels(item.Metric), Timestamps: []int64{ts}, Values: []*float64{value}})
		}
		return out, nil
	case "scalar", "string":
		var point [2]any
		if err := decodeJSON(raw, &point); err != nil {
			return nil, err
		}
		ts, value := promPoint(point)
		labels := map[string]string{}
		if resultType == "string" {
			labels["value"], _ = point[1].(string)
		}
		return []model.MetricSeries{{Labels: labels, Timestamps: []int64{ts}, Values: []*float64{value}}}, nil
	}
	return []model.MetricSeries{}, nil
}

func promPoint(point [2]any) (int64, *float64) {
	seconds, _ := point[0].(float64)
	ts := int64(math.Round(seconds * 1000))
	text, _ := point[1].(string)
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return ts, nil
	}
	return ts, &value
}

func nonNilLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	return labels
}

func (p *Prometheus) FormatQuery(ctx context.Context, expr string) (string, error) {
	var formatted string
	_, err := p.call(ctx, http.MethodPost, "/api/v1/format_query", url.Values{"query": {expr}}, 10*time.Second, &formatted)
	return formatted, err
}

func (p *Prometheus) Metadata(ctx context.Context, metric string, limit int) ([]model.MetricInfo, error) {
	values := url.Values{"limit_per_metric": {"1"}}
	if metric != "" {
		values.Set("metric", metric)
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var data map[string][]struct {
		Type string `json:"type"`
		Help string `json:"help"`
		Unit string `json:"unit"`
	}
	if _, err := p.call(ctx, http.MethodGet, "/api/v1/metadata", values, 0, &data); err != nil {
		return nil, err
	}
	out := make([]model.MetricInfo, 0, len(data))
	for name, items := range data {
		info := model.MetricInfo{Name: name}
		if len(items) > 0 {
			info.Type, info.Help, info.Unit = items[0].Type, items[0].Help, items[0].Unit
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (p *Prometheus) MetricNames(ctx context.Context, start, end time.Time) ([]string, error) {
	return p.LabelValues(ctx, "__name__", nil, start, end, 0)
}

func timeRange(values url.Values, start, end time.Time) {
	if !start.IsZero() {
		values.Set("start", unixSeconds(start))
	}
	if !end.IsZero() {
		values.Set("end", unixSeconds(end))
	}
}

func (p *Prometheus) LabelNames(ctx context.Context, matchers []string, start, end time.Time) ([]string, error) {
	values := url.Values{}
	for _, m := range matchers {
		values.Add("match[]", m)
	}
	timeRange(values, start, end)
	var out []string
	_, err := p.call(ctx, http.MethodPost, "/api/v1/labels", values, 0, &out)
	return out, err
}

func (p *Prometheus) LabelValues(ctx context.Context, label string, matchers []string, start, end time.Time, limit int) ([]string, error) {
	values := url.Values{}
	for _, m := range matchers {
		values.Add("match[]", m)
	}
	timeRange(values, start, end)
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var out []string
	_, err := p.call(ctx, http.MethodGet, "/api/v1/label/"+url.PathEscape(label)+"/values", values, 0, &out)
	return out, err
}

func (p *Prometheus) Targets(ctx context.Context) ([]model.ScrapeTarget, error) {
	var data struct {
		ActiveTargets []struct {
			DiscoveredLabels   map[string]string `json:"discoveredLabels"`
			Labels             map[string]string `json:"labels"`
			ScrapePool         string            `json:"scrapePool"`
			LastError          string            `json:"lastError"`
			LastScrape         string            `json:"lastScrape"`
			LastScrapeDuration float64           `json:"lastScrapeDuration"`
			Health             string            `json:"health"`
			ScrapeInterval     string            `json:"scrapeInterval"`
			ScrapeTimeout      string            `json:"scrapeTimeout"`
		} `json:"activeTargets"`
	}
	if _, err := p.call(ctx, http.MethodGet, "/api/v1/targets", url.Values{"state": {"active"}}, 0, &data); err != nil {
		return nil, err
	}
	out := make([]model.ScrapeTarget, 0, len(data.ActiveTargets))
	for _, t := range data.ActiveTargets {
		out = append(out, model.ScrapeTarget{
			Job: firstNonEmpty(t.Labels["job"], t.ScrapePool), Instance: t.Labels["instance"], Labels: nonNilLabels(t.Labels),
			Health: t.Health, LastError: t.LastError, LastScrape: t.LastScrape, LastScrapeDuration: t.LastScrapeDuration,
			ScrapeInterval: t.ScrapeInterval, ScrapeTimeout: t.ScrapeTimeout, MetricsPath: t.DiscoveredLabels["__metrics_path__"],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Job != out[j].Job {
			return out[i].Job < out[j].Job
		}
		return out[i].Instance < out[j].Instance
	})
	return out, nil
}

type promRuleGroup struct {
	Name           string  `json:"name"`
	File           string  `json:"file"`
	Interval       float64 `json:"interval"`
	Limit          int     `json:"limit"`
	LastEvaluation string  `json:"lastEvaluation"`
	Rules          []struct {
		Type           string            `json:"type"`
		Name           string            `json:"name"`
		Query          string            `json:"query"`
		Duration       float64           `json:"duration"`
		KeepFiringFor  float64           `json:"keepFiringFor"`
		Labels         map[string]string `json:"labels"`
		Annotations    map[string]string `json:"annotations"`
		Health         string            `json:"health"`
		LastError      string            `json:"lastError"`
		State          string            `json:"state"`
		LastEvaluation string            `json:"lastEvaluation"`
		EvaluationTime float64           `json:"evaluationTime"`
		Alerts         []json.RawMessage `json:"alerts"`
	} `json:"rules"`
}

func convertRuleGroups(source string, groups []promRuleGroup) []model.OpsRuleGroup {
	out := make([]model.OpsRuleGroup, 0, len(groups))
	for _, g := range groups {
		group := model.OpsRuleGroup{Source: source, Name: g.Name, File: g.File, Interval: secondsDuration(g.Interval), Limit: g.Limit, Loaded: true, Enabled: true, Rules: []model.OpsRule{}}
		for _, r := range g.Rules {
			rule := model.OpsRule{Name: r.Name, Expr: r.Query, Labels: r.Labels, Annotations: r.Annotations, Health: r.Health, LastError: r.LastError, State: r.State, LastEvaluation: r.LastEvaluation, EvaluationTime: r.EvaluationTime, ActiveAlerts: len(r.Alerts)}
			if r.Type == "alerting" {
				rule.Kind = "alert"
				rule.For = secondsDuration(r.Duration)
				rule.KeepFiringFor = secondsDuration(r.KeepFiringFor)
			} else {
				rule.Kind = "record"
			}
			group.Rules = append(group.Rules, rule)
		}
		out = append(out, group)
	}
	return out
}

func secondsDuration(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	return promDuration(time.Duration(seconds * float64(time.Second)))
}

// promDuration renders durations in Prometheus notation (1h30m, 45s).
func promDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	var b strings.Builder
	for _, unit := range []struct {
		suffix string
		size   time.Duration
	}{{"d", 24 * time.Hour}, {"h", time.Hour}, {"m", time.Minute}, {"s", time.Second}, {"ms", time.Millisecond}} {
		if n := d / unit.size; n > 0 {
			b.WriteString(strconv.FormatInt(int64(n), 10) + unit.suffix)
			d -= n * unit.size
		}
	}
	return b.String()
}

func (p *Prometheus) RuleGroups(ctx context.Context) ([]model.OpsRuleGroup, error) {
	var data struct {
		Groups []promRuleGroup `json:"groups"`
	}
	if _, err := p.call(ctx, http.MethodGet, "/api/v1/rules", nil, 0, &data); err != nil {
		return nil, err
	}
	return convertRuleGroups("prometheus", data.Groups), nil
}

func (p *Prometheus) Runtime(ctx context.Context) (ports.PrometheusRuntime, error) {
	var data struct {
		ReloadConfigSuccess bool   `json:"reloadConfigSuccess"`
		LastConfigTime      string `json:"lastConfigTime"`
		ServerTime          string `json:"serverTime"`
		StorageRetention    string `json:"storageRetention"`
		TimeSeriesCount     int64  `json:"timeSeriesCount"`
	}
	if _, err := p.call(ctx, http.MethodGet, "/api/v1/status/runtimeinfo", nil, 10*time.Second, &data); err != nil {
		return ports.PrometheusRuntime{}, err
	}
	out := ports.PrometheusRuntime{ReloadSuccess: data.ReloadConfigSuccess, Retention: data.StorageRetention, Series: data.TimeSeriesCount}
	out.LastConfig, _ = time.Parse(time.RFC3339Nano, data.LastConfigTime)
	out.ServerTime, _ = time.Parse(time.RFC3339Nano, data.ServerTime)
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
