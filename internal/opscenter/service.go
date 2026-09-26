// Package opscenter implements the platform's native operations center on top
// of Prometheus, Loki, Grafana and Alertmanager. It owns validation, query
// limits, rule/config change workflows and dashboard support analysis; the
// components keep storage, query execution, rule evaluation and notification.
package opscenter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type Limits struct {
	QueryTimeout   time.Duration
	ReloadTimeout  time.Duration
	MaxSeries      int
	MaxLogLines    int
	MaxExportLines int
	MaxMetricRange time.Duration
	MaxLogRange    time.Duration
}

type Service struct {
	Metrics     ports.MetricsBackend
	Logs        ports.LogsBackend
	Dashboards  ports.DashboardsBackend
	Alerts      ports.AlertmanagerBackend
	PromRules   ports.ManagedFileStore
	LokiRules   ports.ManagedFileStore
	LokiRuntime ports.ManagedConfigFile
	AMConfig    ports.ManagedConfigFile
	Prefs       ports.OpsPreferenceStore
	Limits      Limits
	Log         *slog.Logger
	Now         func() time.Time
	// PollInterval controls how often reload verification polls components.
	PollInterval time.Duration

	promMu sync.Mutex
	lokiMu sync.Mutex
	amMu   sync.Mutex
	rtMu   sync.Mutex
}

// ValidationError is a user input problem (HTTP 422).
type ValidationError struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string { return e.Message }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// ApplyError reports a change that a component rejected or did not confirm.
// The previous configuration has been restored when RolledBack is true.
type ApplyError struct {
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	RolledBack bool   `json:"rolledBack"`
}

func (e *ApplyError) Error() string { return e.Message }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) pollInterval() time.Duration {
	if s.PollInterval > 0 {
		return s.PollInterval
	}
	return time.Second
}

func (s *Service) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func configured(b interface{ Configured() bool }) bool {
	if b == nil {
		return false
	}
	return b.Configured()
}

// Capabilities tells the UI which functions this deployment supports so it
// can show "未配置" instead of failing requests.
type Capabilities struct {
	Metrics              bool `json:"metrics"`
	Logs                 bool `json:"logs"`
	Dashboards           bool `json:"dashboards"`
	Alerts               bool `json:"alerts"`
	MetricRulesWritable  bool `json:"metricRulesWritable"`
	LogRulesWritable     bool `json:"logRulesWritable"`
	RetentionWritable    bool `json:"retentionWritable"`
	NotificationWritable bool `json:"notificationWritable"`
	Preferences          bool `json:"preferences"`
	MaxSeries            int  `json:"maxSeries"`
	MaxLogLines          int  `json:"maxLogLines"`
	MaxExportLines       int  `json:"maxExportLines"`
	MaxMetricRangeHours  int  `json:"maxMetricRangeHours"`
	MaxLogRangeHours     int  `json:"maxLogRangeHours"`
}

func (s *Service) Capabilities() Capabilities {
	return Capabilities{
		Metrics:              configured(s.Metrics),
		Logs:                 configured(s.Logs),
		Dashboards:           configured(s.Dashboards),
		Alerts:               configured(s.Alerts),
		MetricRulesWritable:  configured(s.Metrics) && configured(s.PromRules),
		LogRulesWritable:     configured(s.Logs) && configured(s.LokiRules),
		RetentionWritable:    configured(s.Logs) && configured(s.LokiRuntime),
		NotificationWritable: configured(s.Alerts) && configured(s.AMConfig),
		Preferences:          s.Prefs != nil,
		MaxSeries:            s.Limits.MaxSeries,
		MaxLogLines:          s.Limits.MaxLogLines,
		MaxExportLines:       s.Limits.MaxExportLines,
		MaxMetricRangeHours:  int(s.Limits.MaxMetricRange / time.Hour),
		MaxLogRangeHours:     int(s.Limits.MaxLogRange / time.Hour),
	}
}

// Components checks every component concurrently.
func (s *Service) Components(ctx context.Context) []model.OpsComponentStatus {
	type statusFn func(context.Context) model.OpsComponentStatus
	checks := []statusFn{
		func(c context.Context) model.OpsComponentStatus {
			return statusOf(c, s.Metrics, "prometheus", "Prometheus")
		},
		func(c context.Context) model.OpsComponentStatus { return statusOf(c, s.Logs, "loki", "Loki") },
		func(c context.Context) model.OpsComponentStatus {
			return statusOf(c, s.Dashboards, "grafana", "Grafana")
		},
		func(c context.Context) model.OpsComponentStatus {
			return statusOf(c, s.Alerts, "alertmanager", "Alertmanager")
		},
	}
	out := make([]model.OpsComponentStatus, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func(i int, check statusFn) {
			defer wg.Done()
			out[i] = check(ctx)
		}(i, check)
	}
	wg.Wait()
	return out
}

type statusChecker interface {
	Configured() bool
	Status(context.Context) model.OpsComponentStatus
}

func statusOf[T statusChecker](ctx context.Context, backend T, id, name string) model.OpsComponentStatus {
	var zero T
	if any(backend) == any(zero) || !backend.Configured() {
		return model.OpsComponentStatus{ID: id, Name: name, State: "unconfigured", Message: "未配置", CheckedAt: time.Now().UnixMilli()}
	}
	return backend.Status(ctx)
}

func requireBackend(b interface{ Configured() bool }) error {
	if !configured(b) {
		return ports.ErrOpsNotConfigured
	}
	return nil
}

// TimeRange validates a query window against the configured maximum.
func TimeRange(start, end time.Time, max time.Duration, now time.Time) (time.Time, time.Time, error) {
	if end.IsZero() {
		end = now
	}
	if start.IsZero() {
		start = end.Add(-time.Hour)
	}
	if !end.After(start) {
		return start, end, invalid("range", "结束时间必须晚于开始时间")
	}
	if end.Sub(start) > max {
		return start, end, invalid("range", "查询时间范围不能超过 %s，请缩小范围", humanDuration(max))
	}
	if end.After(now.Add(5 * time.Minute)) {
		end = now
	}
	return start, end, nil
}

func humanDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%d 天", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%d 小时", d/time.Hour)
	}
	return d.String()
}

// isCanceled reports whether the caller went away; handlers skip responses.
func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// canonicalJSON relies on encoding/json sorting map keys.
func canonicalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
