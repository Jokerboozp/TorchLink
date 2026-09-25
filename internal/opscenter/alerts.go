package opscenter

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Infrastructure alerting chain: Prometheus evaluates metric alert rules, the
// Loki ruler evaluates log alert rules, and both send to Alertmanager, which is
// the only component that groups, silences and notifies. Grafana alerting is
// disabled in the deployment so nothing is evaluated or notified twice.
// Fire-protection business alarms stay in the platform's own alarm center.

type AlertListInput struct {
	Matchers  []Matcher `json:"matchers"`
	Silenced  bool      `json:"silenced"`
	Inhibited bool      `json:"inhibited"`
	Receiver  string    `json:"receiver"`
}

func (in AlertListInput) filter() (ports.AlertFilter, error) {
	f := ports.AlertFilter{Active: true, Silenced: in.Silenced, Inhibited: in.Inhibited}
	for _, m := range in.Matchers {
		if err := m.validate(); err != nil {
			return f, err
		}
		f.Matchers = append(f.Matchers, m.String())
	}
	if in.Receiver != "" {
		if len(in.Receiver) > 200 {
			return f, invalid("receiver", "接收人名称过长")
		}
		f.Receiver = "^(?:" + regexp.QuoteMeta(in.Receiver) + ")$"
	}
	return f, nil
}

func (s *Service) CurrentAlerts(ctx context.Context, in AlertListInput) ([]model.OpsAlert, error) {
	if err := requireBackend(s.Alerts); err != nil {
		return nil, err
	}
	f, err := in.filter()
	if err != nil {
		return nil, err
	}
	return s.Alerts.Alerts(ctx, f)
}

func (s *Service) AlertGroups(ctx context.Context, in AlertListInput) ([]model.OpsAlertGroup, error) {
	if err := requireBackend(s.Alerts); err != nil {
		return nil, err
	}
	f, err := in.filter()
	if err != nil {
		return nil, err
	}
	return s.Alerts.AlertGroups(ctx, f)
}

// AlertRules lists alerting rules from both evaluators with their live state.
func (s *Service) AlertRules(ctx context.Context) ([]model.OpsRuleGroup, []string, error) {
	out := []model.OpsRuleGroup{}
	warnings := []string{}
	for _, source := range []string{SourcePrometheus, SourceLoki} {
		groups, err := s.RuleGroups(ctx, source)
		if err != nil {
			if source == SourceLoki && !configured(s.Logs) {
				continue
			}
			if source == SourcePrometheus && !configured(s.Metrics) {
				continue
			}
			warnings = append(warnings, source+" 规则读取失败")
			continue
		}
		for _, g := range groups {
			alerts := []model.OpsRule{}
			for _, r := range g.Rules {
				if r.Kind == "alert" {
					alerts = append(alerts, r)
				}
			}
			if len(alerts) > 0 {
				g.Rules = alerts
				out = append(out, g)
			}
		}
	}
	return out, warnings, nil
}

// AlertHistory rebuilds firing intervals from Prometheus's ALERTS series.
// Log-rule alerts from the Loki ruler are not recorded there.
// AlertHistory rebuilds firing intervals from Prometheus's ALERTS series.
// Samples are one query step apart, so an interval is reported as ending one
// step after its last firing sample; the step is returned as the precision.
func (s *Service) AlertHistory(ctx context.Context, start, end int64) ([]model.OpsAlertHistoryItem, bool, int64, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return nil, false, 0, err
	}
	from, to, err := TimeRange(msTime(start), msTime(end), s.Limits.MaxMetricRange, s.now())
	if err != nil {
		return nil, false, 0, err
	}
	step := Step(from, to, 15*time.Second, 2000)
	result, err := s.Metrics.Query(ctx, ports.MetricQuery{Expr: `ALERTS{alertstate="firing"}`, Start: from, End: to, Step: step, Limit: s.Limits.MaxSeries, Timeout: s.Limits.QueryTimeout})
	if err != nil {
		return nil, false, 0, err
	}
	closeAt := func(last int64) int64 {
		if end := last + step.Milliseconds(); end < to.UnixMilli() {
			return end
		}
		return to.UnixMilli()
	}
	gap := step.Milliseconds()*3/2 + 1
	out := []model.OpsAlertHistoryItem{}
	for _, series := range result.Series {
		labels := map[string]string{}
		for k, v := range series.Labels {
			if k != "alertstate" && k != "__name__" {
				labels[k] = v
			}
		}
		var current *model.OpsAlertHistoryItem
		var last int64
		for i, ts := range series.Timestamps {
			if series.Values[i] == nil {
				continue
			}
			if current != nil && ts-last > gap {
				current.End = closeAt(last)
				out = append(out, *current)
				current = nil
			}
			if current == nil {
				current = &model.OpsAlertHistoryItem{Labels: labels, Start: ts}
			}
			last = ts
		}
		if current != nil {
			current.End = closeAt(last)
			current.Active = to.UnixMilli()-last <= 2*step.Milliseconds()
			out = append(out, *current)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start > out[j].Start })
	truncated := result.Truncated
	if len(out) > 1000 {
		out, truncated = out[:1000], true
	}
	return out, truncated, step.Milliseconds(), nil
}

func (s *Service) Silences(ctx context.Context) ([]model.OpsSilence, error) {
	if err := requireBackend(s.Alerts); err != nil {
		return nil, err
	}
	return s.Alerts.Silences(ctx)
}

type SilenceInput struct {
	ID       string    `json:"id"`
	Matchers []Matcher `json:"matchers"`
	StartsAt string    `json:"startsAt"`
	EndsAt   string    `json:"endsAt"`
	Comment  string    `json:"comment"`
}

// SaveSilence creates a silence, or replaces one (Alertmanager expires the old
// silence and returns a new ID when an existing one is edited).
func (s *Service) SaveSilence(ctx context.Context, in SilenceInput, actor string) (string, error) {
	if err := requireBackend(s.Alerts); err != nil {
		return "", err
	}
	if len(in.Matchers) == 0 || len(in.Matchers) > 20 {
		return "", invalid("matchers", "静默需要 1～20 个匹配条件")
	}
	matchers := []model.OpsMatcher{}
	matchesNonEmpty := false
	for _, m := range in.Matchers {
		if err := m.validate(); err != nil {
			return "", err
		}
		om := model.OpsMatcher{Name: m.Name, Value: m.Value, IsRegex: strings.HasSuffix(m.Op, "~"), IsEqual: !strings.HasPrefix(m.Op, "!")}
		if om.IsEqual && !(om.IsRegex && regexp.MustCompile("^(?:"+m.Value+")$").MatchString("")) && !(!om.IsRegex && m.Value == "") {
			matchesNonEmpty = true
		}
		matchers = append(matchers, om)
	}
	if !matchesNonEmpty {
		return "", invalid("matchers", "至少需要一个不匹配空值的“等于”条件，避免静默所有告警")
	}
	now := s.now()
	startsAt := now
	if in.StartsAt != "" {
		t, err := time.Parse(time.RFC3339, in.StartsAt)
		if err != nil {
			return "", invalid("startsAt", "开始时间格式无效")
		}
		startsAt = t
	}
	endsAt, err := time.Parse(time.RFC3339, in.EndsAt)
	if err != nil {
		return "", invalid("endsAt", "结束时间格式无效")
	}
	if !endsAt.After(startsAt) || !endsAt.After(now) {
		return "", invalid("endsAt", "结束时间必须晚于开始时间和当前时间")
	}
	if endsAt.Sub(startsAt) > 366*24*time.Hour {
		return "", invalid("endsAt", "静默时长不能超过 366 天")
	}
	comment := strings.TrimSpace(in.Comment)
	if comment == "" || len([]rune(comment)) > 500 {
		return "", invalid("comment", "请填写 1～500 字的静默原因")
	}
	if in.ID != "" && !silenceIDPattern.MatchString(in.ID) {
		return "", invalid("id", "静默编号无效")
	}
	return s.Alerts.SaveSilence(ctx, model.OpsSilence{ID: in.ID, Matchers: matchers, StartsAt: startsAt.UTC().Format(time.RFC3339), EndsAt: endsAt.UTC().Format(time.RFC3339), CreatedBy: actor, Comment: comment})
}

var silenceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)

func (s *Service) ExpireSilence(ctx context.Context, id string) error {
	if err := requireBackend(s.Alerts); err != nil {
		return err
	}
	if !silenceIDPattern.MatchString(id) {
		return invalid("id", "静默编号无效")
	}
	return s.Alerts.ExpireSilence(ctx, id)
}
