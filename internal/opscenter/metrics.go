package opscenter

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type MetricQueryInput struct {
	Query     string `json:"query"`
	Instant   bool   `json:"instant"`
	Time      int64  `json:"time"`
	Start     int64  `json:"start"`
	End       int64  `json:"end"`
	StepMs    int64  `json:"stepMs"`
	MaxPoints int    `json:"maxPoints"`
}

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// QueryMetrics executes a PromQL instant or range query with the platform's
// series, range, resolution and timeout limits.
func (s *Service) QueryMetrics(ctx context.Context, in MetricQueryInput) (model.MetricQueryResult, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return model.MetricQueryResult{}, err
	}
	in.Query = strings.TrimSpace(in.Query)
	if err := checkQueryText("query", in.Query); err != nil {
		return model.MetricQueryResult{}, err
	}
	q := ports.MetricQuery{Expr: in.Query, Instant: in.Instant, Limit: s.Limits.MaxSeries, Timeout: s.Limits.QueryTimeout}
	if in.Instant {
		q.Time = msTime(in.Time)
		if q.Time.IsZero() {
			q.Time = s.now()
		}
	} else {
		start, end, err := TimeRange(msTime(in.Start), msTime(in.End), s.Limits.MaxMetricRange, s.now())
		if err != nil {
			return model.MetricQueryResult{}, err
		}
		q.Start, q.End = start, end
		q.Step = Step(start, end, time.Duration(in.StepMs)*time.Millisecond, in.MaxPoints)
	}
	result, err := s.Metrics.Query(ctx, q)
	return result, asQueryError(err)
}

// asQueryError turns a component's rejection of a query into a validation
// error the editor can show next to the input.
func asQueryError(err error) error {
	var upstream *ports.OpsUpstreamError
	if errors.As(err, &upstream) && upstream.Status >= 400 && upstream.Status < 500 && upstream.Status != 401 && upstream.Status != 403 {
		return invalid("query", "%s", upstream.Message)
	}
	return err
}

func (s *Service) ValidatePromQL(ctx context.Context, query string) (string, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return "", err
	}
	if err := checkQueryText("query", query); err != nil {
		return "", err
	}
	formatted, err := s.Metrics.FormatQuery(ctx, query)
	return formatted, asQueryError(err)
}

type ExploreInput struct {
	Metric    string    `json:"metric"`
	Matchers  []Matcher `json:"matchers"`
	Fn        string    `json:"fn"`
	Window    string    `json:"window"`
	By        string    `json:"by"`
	Start     int64     `json:"start"`
	End       int64     `json:"end"`
	StepMs    int64     `json:"stepMs"`
	MaxPoints int       `json:"maxPoints"`
}

// ExploreMetric charts one metric from structured inputs. It is available to
// viewers who are not allowed to run free-form PromQL.
func (s *Service) ExploreMetric(ctx context.Context, in ExploreInput) (model.MetricQueryResult, error) {
	query, err := ExploreQuery(in.Metric, in.Matchers, in.Fn, in.Window, in.By)
	if err != nil {
		return model.MetricQueryResult{}, err
	}
	return s.QueryMetrics(ctx, MetricQueryInput{Query: query, Start: in.Start, End: in.End, StepMs: in.StepMs, MaxPoints: in.MaxPoints})
}

type MetricCatalog struct {
	Items     []model.MetricInfo `json:"items"`
	Total     int                `json:"total"`
	Truncated bool               `json:"truncated"`
}

func (s *Service) MetricCatalog(ctx context.Context, search string, limit int) (MetricCatalog, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return MetricCatalog{}, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	names, err := s.Metrics.MetricNames(ctx, s.now().Add(-time.Hour), s.now())
	if err != nil {
		return MetricCatalog{}, err
	}
	meta, err := s.Metrics.Metadata(ctx, "", 0)
	if err != nil {
		meta = nil
	}
	byName := map[string]model.MetricInfo{}
	for _, m := range meta {
		byName[m.Name] = m
	}
	search = strings.ToLower(strings.TrimSpace(search))
	out := MetricCatalog{Items: []model.MetricInfo{}}
	sort.Strings(names)
	for _, name := range names {
		info, ok := byName[name]
		if !ok {
			info = model.MetricInfo{Name: name}
		}
		if search != "" && !strings.Contains(strings.ToLower(name), search) && !strings.Contains(strings.ToLower(info.Help), search) {
			continue
		}
		out.Total++
		if len(out.Items) < limit {
			out.Items = append(out.Items, info)
		}
	}
	out.Truncated = out.Total > len(out.Items)
	return out, nil
}

func metricMatch(metric string) ([]string, error) {
	if metric == "" {
		return nil, nil
	}
	if !validMetricName(metric) {
		return nil, invalid("metric", "指标名不合法")
	}
	return []string{metric}, nil
}

func (s *Service) MetricLabels(ctx context.Context, metric string) ([]string, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return nil, err
	}
	match, err := metricMatch(metric)
	if err != nil {
		return nil, err
	}
	names, err := s.Metrics.LabelNames(ctx, match, s.now().Add(-time.Hour), s.now())
	if names == nil {
		names = []string{}
	}
	return names, err
}

func (s *Service) MetricLabelValues(ctx context.Context, label, metric string) ([]string, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return nil, err
	}
	if !validLabelName(label) && label != "__name__" {
		return nil, invalid("label", "标签名不合法")
	}
	match, err := metricMatch(metric)
	if err != nil {
		return nil, err
	}
	values, err := s.Metrics.LabelValues(ctx, label, match, s.now().Add(-time.Hour), s.now(), 1000)
	if values == nil {
		values = []string{}
	}
	return values, err
}

func (s *Service) Targets(ctx context.Context) ([]model.ScrapeTarget, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return nil, err
	}
	return s.Metrics.Targets(ctx)
}
