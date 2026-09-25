package opscenter

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Structured filters are turned into PromQL/LogQL here. Names are checked
// against the languages' identifier grammar and every value is emitted as a
// quoted string literal (both languages use Go string-literal escaping), so a
// value can never break out of its matcher.

var (
	labelNamePattern  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	metricNamePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
)

const maxQueryLength = 16 << 10

type Matcher struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

func validLabelName(name string) bool { return labelNamePattern.MatchString(name) }

func validMetricName(name string) bool { return metricNamePattern.MatchString(name) }

func quote(value string) string { return strconv.Quote(value) }

func (m Matcher) validate() error {
	if !validLabelName(m.Name) {
		return invalid("matchers", "标签名 %q 不合法", m.Name)
	}
	switch m.Op {
	case "=", "!=":
	case "=~", "!~":
		if _, err := regexp.Compile("^(?:" + m.Value + ")$"); err != nil {
			return invalid("matchers", "标签 %s 的正则表达式无效", m.Name)
		}
	default:
		return invalid("matchers", "不支持的匹配方式 %q", m.Op)
	}
	if len(m.Value) > 1024 {
		return invalid("matchers", "标签值过长")
	}
	return nil
}

func (m Matcher) String() string { return m.Name + m.Op + quote(m.Value) }

// Selector renders {a="b",c=~"d"} with optional metric name prefix.
func Selector(metric string, matchers []Matcher) (string, error) {
	if metric != "" && !validMetricName(metric) {
		return "", invalid("metric", "指标名 %q 不合法", metric)
	}
	parts := make([]string, 0, len(matchers))
	for _, m := range matchers {
		if err := m.validate(); err != nil {
			return "", err
		}
		parts = append(parts, m.String())
	}
	if metric == "" && len(parts) == 0 {
		return "", invalid("matchers", "至少需要一个标签条件")
	}
	return metric + "{" + strings.Join(parts, ",") + "}", nil
}

// ParseSelector reads a stream selector such as {a="b", c=~"d"} back into
// matchers. Values are decoded as Go/LogQL string literals, never by pattern
// substitution, so the result renders back to an equivalent selector.
func ParseSelector(text string) ([]Matcher, error) {
	rest := strings.TrimSpace(text)
	if !strings.HasPrefix(rest, "{") {
		return nil, invalid("selector", "日志流条件必须以 { 开始")
	}
	rest = strings.TrimSpace(rest[1:])
	matchers := []Matcher{}
	for {
		if strings.HasPrefix(rest, "}") {
			if strings.TrimSpace(rest[1:]) != "" {
				return nil, invalid("selector", "日志流条件末尾有多余内容")
			}
			return matchers, nil
		}
		end := 0
		for end < len(rest) && (rest[end] == '_' || rest[end] >= 'a' && rest[end] <= 'z' || rest[end] >= 'A' && rest[end] <= 'Z' || end > 0 && rest[end] >= '0' && rest[end] <= '9') {
			end++
		}
		m := Matcher{Name: rest[:end]}
		rest = strings.TrimSpace(rest[end:])
		for _, op := range []string{"=~", "!~", "!=", "="} {
			if strings.HasPrefix(rest, op) {
				m.Op, rest = op, strings.TrimSpace(rest[len(op):])
				break
			}
		}
		literal, err := strconv.QuotedPrefix(rest)
		if err != nil || m.Op == "" || literal[0] == '\'' {
			return nil, invalid("selector", "日志流条件格式无效")
		}
		if m.Value, err = strconv.Unquote(literal); err != nil {
			return nil, invalid("selector", "日志流条件格式无效")
		}
		if err := m.validate(); err != nil {
			return nil, err
		}
		matchers = append(matchers, m)
		rest = strings.TrimSpace(rest[len(literal):])
		if strings.HasPrefix(rest, ",") {
			rest = strings.TrimSpace(rest[1:])
		} else if !strings.HasPrefix(rest, "}") {
			return nil, invalid("selector", "日志流条件格式无效")
		}
	}
}

var exploreWindows = map[string]bool{"1m": true, "5m": true, "15m": true, "1h": true}

// ExploreQuery builds the metric browser's structured query.
func ExploreQuery(metric string, matchers []Matcher, fn, window, by string) (string, error) {
	sel, err := Selector(metric, matchers)
	if err != nil {
		return "", err
	}
	if window == "" {
		window = "5m"
	}
	if !exploreWindows[window] {
		return "", invalid("window", "不支持的时间窗口")
	}
	grouping := ""
	if by != "" {
		labels := strings.Split(by, ",")
		for i, label := range labels {
			labels[i] = strings.TrimSpace(label)
			if !validLabelName(labels[i]) {
				return "", invalid("by", "分组标签 %q 不合法", labels[i])
			}
		}
		grouping = " by (" + strings.Join(labels, ",") + ")"
	}
	switch fn {
	case "", "raw":
		return sel, nil
	case "rate", "increase", "irate", "avg_over_time", "max_over_time", "min_over_time":
		return fn + "(" + sel + "[" + window + "])", nil
	case "sum", "avg", "max", "min", "count":
		return fn + grouping + "(" + sel + ")", nil
	case "sum_rate":
		return "sum" + grouping + "(rate(" + sel + "[" + window + "]))", nil
	case "sum_increase":
		return "sum" + grouping + "(increase(" + sel + "[" + window + "]))", nil
	}
	return "", invalid("fn", "不支持的聚合函数 %q", fn)
}

// LogFilter is the structured log search used by viewers who are not granted
// raw LogQL.
type LogFilter struct {
	Services []string  `json:"services"`
	Levels   []string  `json:"levels"`
	Labels   []Matcher `json:"labels"`
	Keyword  string    `json:"keyword"`
	Regex    bool      `json:"regex"`
	Exclude  string    `json:"exclude"`
}

var logLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true, "fatal": true, "unknown": true}

// LogQL renders the structured filter. Stream selectors must contain at least
// one non-empty matcher, so an empty filter selects every service.
func (f LogFilter) LogQL() (string, error) {
	matchers := []Matcher{}
	services := cleanValues(f.Services)
	switch len(services) {
	case 0:
		matchers = append(matchers, Matcher{Name: "service_name", Op: "=~", Value: ".+"})
	case 1:
		matchers = append(matchers, Matcher{Name: "service_name", Op: "=", Value: services[0]})
	default:
		matchers = append(matchers, Matcher{Name: "service_name", Op: "=~", Value: regexAlternation(services)})
	}
	levels := cleanValues(f.Levels)
	for _, level := range levels {
		if !logLevels[level] {
			return "", invalid("levels", "不支持的日志级别 %q", level)
		}
	}
	if len(levels) == 1 {
		matchers = append(matchers, Matcher{Name: "level", Op: "=", Value: levels[0]})
	} else if len(levels) > 1 {
		matchers = append(matchers, Matcher{Name: "level", Op: "=~", Value: regexAlternation(levels)})
	}
	for _, m := range f.Labels {
		if m.Name == "service_name" || m.Name == "level" {
			return "", invalid("labels", "服务和级别请使用专用筛选项")
		}
		matchers = append(matchers, m)
	}
	selector, err := Selector("", matchers)
	if err != nil {
		return "", err
	}
	query := selector
	if keyword := strings.TrimSpace(f.Keyword); keyword != "" {
		if len(keyword) > 512 {
			return "", invalid("keyword", "关键词过长")
		}
		if f.Regex {
			if _, err := regexp.Compile(keyword); err != nil {
				return "", invalid("keyword", "关键词正则表达式无效")
			}
			query += " |~ " + quote(keyword)
		} else {
			query += " |= " + quote(keyword)
		}
	}
	if exclude := strings.TrimSpace(f.Exclude); exclude != "" {
		if len(exclude) > 512 {
			return "", invalid("exclude", "排除词过长")
		}
		query += " != " + quote(exclude)
	}
	return query, nil
}

func cleanValues(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] && len(v) <= 256 {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func regexAlternation(values []string) string {
	escaped := make([]string, len(values))
	for i, v := range values {
		escaped[i] = regexp.QuoteMeta(v)
	}
	return strings.Join(escaped, "|")
}

// StreamSelector rebuilds the exact stream selector for log context lookups
// from the labels returned with an entry.
func StreamSelector(labels map[string]string) (string, error) {
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	matchers := make([]Matcher, 0, len(names))
	for _, name := range names {
		matchers = append(matchers, Matcher{Name: name, Op: "=", Value: labels[name]})
	}
	return Selector("", matchers)
}

// Step picks a resolution that keeps range queries below Prometheus's
// 11,000-point limit and near the requested chart width.
func Step(start, end time.Time, requested time.Duration, maxPoints int) time.Duration {
	if maxPoints <= 0 || maxPoints > 11000 {
		maxPoints = 1100
	}
	span := end.Sub(start)
	min := span / time.Duration(maxPoints)
	step := requested
	if step <= 0 {
		step = span / 300
	}
	if step < min {
		step = min
	}
	if step < time.Second {
		step = time.Second
	}
	return step.Round(time.Second)
}

func checkQueryText(field, query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return invalid(field, "查询语句不能为空")
	}
	if len(query) > maxQueryLength {
		return invalid(field, "查询语句过长")
	}
	return nil
}
