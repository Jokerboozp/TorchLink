package opscenter

import (
	"context"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// KPI is a fixed overview indicator. Only metrics that the platform, the
// backup service, node-exporter or Loki actually expose are listed; each one
// names the scrape job whose health decides "采集失败" versus "无样本".
type KPI struct {
	ID          string   `json:"id"`
	Group       string   `json:"group"`
	Title       string   `json:"title"`
	Unit        string   `json:"unit"`
	Description string   `json:"description"`
	Expr        string   `json:"expr"`
	Series      string   `json:"series"`
	Job         string   `json:"job,omitempty"`
	Warn        *float64 `json:"warn,omitempty"`
	Crit        *float64 `json:"crit,omitempty"`
	EmptyIsZero bool     `json:"-"`
	NoDataHint  string   `json:"-"`
	LogService  string   `json:"logService,omitempty"`
	LogKeyword  string   `json:"logKeyword,omitempty"`
}

func threshold(v float64) *float64 { return &v }

var overviewKPIs = []KPI{
	{ID: "ingest_rate", Group: "platform", Title: "上报速率", Unit: "条/秒", Description: "近 5 分钟原始报文归档速率", Expr: `sum(rate(raw_archive_success_total{job="iot-platform"}[5m]))`, Job: "iot-platform"},
	{ID: "parse_success", Group: "platform", Title: "解析成功", Unit: "条/5分钟", Description: "近 5 分钟成功解析的报文数", Expr: `sum(increase(parse_success_total{job="iot-platform"}[5m]))`, Job: "iot-platform"},
	{ID: "parse_failed", Group: "platform", Title: "解析失败", Unit: "条/5分钟", Description: "近 5 分钟解析失败的报文数", Expr: `sum(increase(parse_failed_total{job="iot-platform"}[5m]))`, Job: "iot-platform", Warn: threshold(1), Crit: threshold(100), LogService: "platform-api", LogKeyword: "parsing failed"},
	{ID: "archive_failed", Group: "platform", Title: "归档失败", Unit: "条/5分钟", Description: "近 5 分钟原始报文归档失败数", Expr: `sum(increase(raw_archive_failed_total{job="iot-platform"}[5m]))`, Job: "iot-platform", Crit: threshold(1), LogService: "platform-api"},
	{ID: "publish_failed", Group: "platform", Title: "队列发布失败", Unit: "条/5分钟", Description: "近 5 分钟写入内部消息队列失败数", Expr: `sum(increase(raw_publish_failed_total{job="iot-platform"}[5m]))`, Job: "iot-platform", Crit: threshold(1), LogService: "platform-api"},
	{ID: "mqtt_backlog", Group: "platform", Title: "MQTT 接收积压", Unit: "条", Description: "MQTT 持久收件箱中待处理的消息数", Expr: `sum(mqtt_inbox_pending{job="iot-platform"})`, Job: "iot-platform", Warn: threshold(1000), Crit: threshold(10000), NoDataHint: "当前进程未启用 MQTT 接收"},
	{ID: "ai_failed", Group: "platform", Title: "AI 调用失败", Unit: "次/1小时", Description: "近 1 小时告警研判调用失败次数", Expr: `sum(increase(ai_analysis_failed_total{job="iot-platform"}[1h]))`, Job: "iot-platform", Warn: threshold(1), Crit: threshold(10), LogService: "platform-api"},
	{ID: "alarm_triggered", Group: "platform", Title: "业务告警触发", Unit: "次/1小时", Description: "近 1 小时全平台触发的消防业务告警次数（仅计数，不含租户明细）", Expr: `sum(increase(alarm_trigger_total{job="iot-platform"}[1h]))`, Job: "iot-platform"},
	{ID: "backup_age", Group: "backup", Title: "距上次成功备份", Unit: "秒", Description: "备份服务最近一次成功备份距今的时间", Expr: `time() - max(backup_last_success_timestamp_seconds{job="backup-service"} > 0)`, Job: "backup-service", Warn: threshold(26 * 3600), Crit: threshold(50 * 3600), NoDataHint: "备份服务尚无成功备份记录", LogService: "backup-service"},
	{ID: "backup_failed", Group: "backup", Title: "备份失败", Unit: "次/24小时", Description: "近 24 小时备份或恢复演练失败次数", Expr: `sum(increase(backup_failed_total{job="backup-service"}[24h]))`, Job: "backup-service", Crit: threshold(1), LogService: "backup-service"},
	{ID: "host_cpu", Group: "host", Title: "CPU 使用率", Unit: "%", Description: "主机 CPU 平均使用率（node-exporter）", Expr: `100 * (1 - avg(rate(node_cpu_seconds_total{job="node",mode="idle"}[5m])))`, Job: "node", Warn: threshold(80), Crit: threshold(95)},
	{ID: "host_memory", Group: "host", Title: "内存使用率", Unit: "%", Description: "主机内存使用率（MemAvailable 口径）", Expr: `100 * (1 - sum(node_memory_MemAvailable_bytes{job="node"}) / sum(node_memory_MemTotal_bytes{job="node"}))`, Job: "node", Warn: threshold(85), Crit: threshold(95)},
	{ID: "host_disk", Group: "host", Title: "根分区使用率", Unit: "%", Description: "主机根分区空间使用率", Expr: `100 * max(1 - node_filesystem_avail_bytes{job="node",mountpoint="/",fstype!~"tmpfs|overlay|squashfs"} / node_filesystem_size_bytes{job="node",mountpoint="/",fstype!~"tmpfs|overlay|squashfs"})`, Job: "node", Warn: threshold(80), Crit: threshold(90)},
	{ID: "log_ingest", Group: "observability", Title: "日志接收速率", Unit: "行/秒", Description: "Loki 近 5 分钟接收的日志行速率", Expr: `sum(rate(loki_distributor_lines_received_total{job="loki"}[5m]))`, Job: "loki", Warn: nil},
	{ID: "infra_alerts", Group: "observability", Title: "触发中的监控告警", Unit: "条", Description: "Prometheus 当前处于 firing 状态的告警数", Expr: `count(ALERTS{alertstate="firing"})`, EmptyIsZero: true, Warn: threshold(1)},
}

func init() {
	for i := range overviewKPIs {
		if overviewKPIs[i].Series == "" {
			overviewKPIs[i].Series = overviewKPIs[i].Expr
		}
	}
}

type KPIValue struct {
	KPI
	Status  string   `json:"status"`
	Level   string   `json:"level,omitempty"`
	Value   *float64 `json:"value,omitempty"`
	Message string   `json:"message,omitempty"`
}

type JobHealth struct {
	Up        int    `json:"up"`
	Total     int    `json:"total"`
	LastError string `json:"lastError,omitempty"`
}

type Overview struct {
	Components []model.OpsComponentStatus `json:"components"`
	KPIs       []KPIValue                 `json:"kpis"`
	Jobs       map[string]JobHealth       `json:"jobs"`
	CheckedAt  int64                      `json:"checkedAt"`
}

// Overview checks the components and evaluates every KPI in one response.
// The overview page requests the same data in parts (Component, OverviewKPIs)
// so that one slow component does not hold back the rest of the page.
func (s *Service) Overview(ctx context.Context) Overview {
	now := s.now()
	out := Overview{CheckedAt: now.UnixMilli()}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		out.Components = s.Components(ctx)
	}()
	group := s.evaluateKPIs(ctx, overviewKPIs, now)
	out.KPIs, out.Jobs = group.KPIs, group.Jobs
	wg.Wait()
	return out
}

// KPIGroup is one overview section's indicators together with the scrape job
// health they were judged against.
type KPIGroup struct {
	Group     string               `json:"group"`
	KPIs      []KPIValue           `json:"kpis"`
	Jobs      map[string]JobHealth `json:"jobs"`
	CheckedAt int64                `json:"checkedAt"`
}

// OverviewKPIs evaluates the indicators of one overview group.
func (s *Service) OverviewKPIs(ctx context.Context, group string) (KPIGroup, error) {
	selected := []KPI{}
	for _, k := range overviewKPIs {
		if k.Group == group {
			selected = append(selected, k)
		}
	}
	if len(selected) == 0 {
		return KPIGroup{}, invalid("group", "未知的指标分组")
	}
	now := s.now()
	out := s.evaluateKPIs(ctx, selected, now)
	out.Group = group
	return out, nil
}

func (s *Service) evaluateKPIs(ctx context.Context, kpis []KPI, now time.Time) KPIGroup {
	out := KPIGroup{KPIs: make([]KPIValue, len(kpis)), Jobs: map[string]JobHealth{}, CheckedAt: now.UnixMilli()}
	if !configured(s.Metrics) {
		for i, k := range kpis {
			out.KPIs[i] = KPIValue{KPI: k, Status: "unconfigured", Message: "未配置 Prometheus"}
		}
		return out
	}
	targets, targetErr := s.overviewTargets(ctx)
	for _, t := range targets {
		job := out.Jobs[t.Job]
		job.Total++
		if t.Health == "up" {
			job.Up++
		} else if job.LastError == "" {
			job.LastError = t.LastError
		}
		out.Jobs[t.Job] = job
	}
	var wg sync.WaitGroup
	for i, k := range kpis {
		wg.Add(1)
		go func(i int, k KPI) {
			defer wg.Done()
			out.KPIs[i] = s.evaluateKPI(ctx, k, out.Jobs, targetErr, now)
		}(i, k)
	}
	wg.Wait()
	return out
}

const (
	// overviewTargetsTTL lets the overview's parallel requests and concurrent
	// viewers share one scrape target listing; it is shorter than the default
	// scrape interval.
	overviewTargetsTTL = 10 * time.Second
	// overviewTargetsTimeout bounds the listing so a slow Prometheus cannot hold
	// the overview for the full query timeout.
	overviewTargetsTimeout = 5 * time.Second
)

type targetsCache struct {
	mu    sync.Mutex
	at    time.Time
	items []model.ScrapeTarget
	call  *targetsCall
}

type targetsCall struct {
	done  chan struct{}
	items []model.ScrapeTarget
	err   error
}

// overviewTargets returns the scrape targets used to judge KPI job health.
// Concurrent callers share one in-flight request; a successful result is
// reused for overviewTargetsTTL. Failures are not cached.
func (s *Service) overviewTargets(ctx context.Context) ([]model.ScrapeTarget, error) {
	c := &s.targets
	c.mu.Lock()
	if c.call == nil && !c.at.IsZero() && time.Since(c.at) < overviewTargetsTTL {
		items := c.items
		c.mu.Unlock()
		return items, nil
	}
	call := c.call
	if call == nil {
		call = &targetsCall{done: make(chan struct{})}
		c.call = call
		// The shared request must not be cancelled by whichever caller started it.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), overviewTargetsTimeout)
		go func() {
			defer cancel()
			items, err := s.Metrics.Targets(fetchCtx)
			c.mu.Lock()
			call.items, call.err = items, err
			if err == nil {
				c.items, c.at = items, time.Now()
			}
			c.call = nil
			c.mu.Unlock()
			close(call.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-call.done:
		return call.items, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) evaluateKPI(ctx context.Context, k KPI, jobs map[string]JobHealth, targetErr error, now time.Time) KPIValue {
	v := KPIValue{KPI: k}
	if targetErr != nil {
		v.Status, v.Message = "error", "无法读取采集目标"
		return v
	}
	if k.Job != "" {
		job, ok := jobs[k.Job]
		switch {
		case !ok || job.Total == 0:
			v.Status, v.Message = "no_target", "Prometheus 未配置采集目标 "+k.Job
			return v
		case job.Up == 0:
			v.Status, v.Message = "scrape_failed", "采集失败"
			if job.LastError != "" {
				v.Message += "：" + job.LastError
			}
			return v
		}
	}
	result, err := s.Metrics.Query(ctx, ports.MetricQuery{Expr: k.Expr, Instant: true, Time: now, Limit: 10, Timeout: 10 * time.Second})
	if err != nil {
		v.Status, v.Message = "error", "查询失败"
		return v
	}
	if len(result.Series) == 0 || len(result.Series[0].Values) == 0 || result.Series[0].Values[0] == nil {
		if k.EmptyIsZero {
			zero := 0.0
			v.Value, v.Status = &zero, "zero"
			return v
		}
		v.Status, v.Message = "no_data", "暂无样本"
		if k.NoDataHint != "" {
			v.Message = k.NoDataHint
		}
		return v
	}
	value := *result.Series[0].Values[0]
	v.Value = &value
	if value == 0 {
		v.Status = "zero"
	} else {
		v.Status = "ok"
	}
	switch {
	case k.Crit != nil && value >= *k.Crit:
		v.Level = "critical"
	case k.Warn != nil && value >= *k.Warn:
		v.Level = "warning"
	}
	return v
}

type KPISeries struct {
	ID     string                  `json:"id"`
	Result model.MetricQueryResult `json:"result"`
	Error  string                  `json:"error,omitempty"`
}

// OverviewSeries runs the fixed trend queries for the requested indicators.
// Viewers of the overview cannot inject expressions here.
func (s *Service) OverviewSeries(ctx context.Context, ids []string, start, end int64, maxPoints int) ([]KPISeries, error) {
	if err := requireBackend(s.Metrics); err != nil {
		return nil, err
	}
	from, to, err := TimeRange(msTime(start), msTime(end), s.Limits.MaxMetricRange, s.now())
	if err != nil {
		return nil, err
	}
	step := Step(from, to, 0, maxPoints)
	selected := []KPI{}
	for _, id := range ids {
		for _, k := range overviewKPIs {
			if k.ID == id {
				selected = append(selected, k)
			}
		}
	}
	out := make([]KPISeries, len(selected))
	var wg sync.WaitGroup
	for i, k := range selected {
		wg.Add(1)
		go func(i int, k KPI) {
			defer wg.Done()
			result, err := s.Metrics.Query(ctx, ports.MetricQuery{Expr: k.Series, Start: from, End: to, Step: step, Limit: 20, Timeout: s.Limits.QueryTimeout})
			out[i] = KPISeries{ID: k.ID, Result: result}
			if err != nil {
				out[i].Error = "查询失败"
			}
		}(i, k)
	}
	wg.Wait()
	return out, nil
}
