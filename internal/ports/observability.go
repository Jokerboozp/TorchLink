package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"iot-platform/internal/model"
)

// Errors returned by observability adapters. HTTP handlers map them to stable
// status codes without exposing upstream addresses or credentials.
var (
	ErrOpsNotConfigured = errors.New("component not configured")
	ErrOpsUnavailable   = errors.New("component unreachable")
	ErrOpsTimeout       = errors.New("component timed out")
	ErrOpsNotFound      = errors.New("resource not found")
	ErrOpsConflict      = errors.New("resource changed by another operation")
	ErrOpsReadOnly      = errors.New("resource is read-only")
)

// OpsUpstreamError carries a component's own error message (for example a
// PromQL parse error). Message is safe to show; it never includes URLs.
type OpsUpstreamError struct {
	Status  int
	Kind    string
	Message string
}

func (e *OpsUpstreamError) Error() string { return e.Message }

type MetricQuery struct {
	Expr    string
	Instant bool
	Time    time.Time
	Start   time.Time
	End     time.Time
	Step    time.Duration
	Limit   int
	Timeout time.Duration
}

type LogQuery struct {
	Query     string
	Instant   bool
	Start     time.Time
	End       time.Time
	Limit     int
	Direction string
	Step      time.Duration
}

type PrometheusRuntime struct {
	ReloadSuccess bool
	LastConfig    time.Time
	ServerTime    time.Time
	Retention     string
	Series        int64
}

type MetricsBackend interface {
	Configured() bool
	Status(context.Context) model.OpsComponentStatus
	Query(context.Context, MetricQuery) (model.MetricQueryResult, error)
	FormatQuery(context.Context, string) (string, error)
	Metadata(context.Context, string, int) ([]model.MetricInfo, error)
	MetricNames(context.Context, time.Time, time.Time) ([]string, error)
	LabelNames(context.Context, []string, time.Time, time.Time) ([]string, error)
	LabelValues(context.Context, string, []string, time.Time, time.Time, int) ([]string, error)
	Targets(context.Context) ([]model.ScrapeTarget, error)
	RuleGroups(context.Context) ([]model.OpsRuleGroup, error)
	Runtime(context.Context) (PrometheusRuntime, error)
}

type LokiLimits struct {
	RetentionEnabled bool
	DeletionMode     string
	GlobalPeriod     string
	CancelPeriod     string
	MaxQueryLength   string
	MaxEntries       int
}

type LokiRuntimeState struct {
	Hash    string
	Success bool
}

type LogsBackend interface {
	Configured() bool
	Status(context.Context) model.OpsComponentStatus
	Query(context.Context, LogQuery) (model.LogQueryResult, error)
	FormatQuery(context.Context, string) (string, error)
	LabelNames(context.Context, time.Time, time.Time) ([]string, error)
	LabelValues(context.Context, string, string, time.Time, time.Time) ([]string, error)
	Tail(context.Context, string, time.Time, int, func([]model.LogEntry, int) error) error
	RuleGroups(context.Context) ([]model.OpsRuleGroup, error)
	DeleteRequests(context.Context) ([]model.LogDeleteRequest, error)
	CreateDeleteRequest(context.Context, string, time.Time, time.Time) error
	CancelDeleteRequest(context.Context, string, bool) error
	Limits(context.Context) (LokiLimits, error)
	RuntimeState(context.Context) (LokiRuntimeState, error)
}

type DashboardSearch struct {
	Query      string
	FolderUIDs []string
	Tags       []string
	UIDs       []string
	Limit      int
}

type DashboardSaveResult struct {
	UID     string `json:"uid"`
	Version int    `json:"version"`
}

type PanelQueryRequest struct {
	From          time.Time
	To            time.Time
	Queries       []map[string]any
	MaxDataPoints int
	IntervalMs    int64
}

type DashboardsBackend interface {
	Configured() bool
	Status(context.Context) model.OpsComponentStatus
	SearchDashboards(context.Context, DashboardSearch) ([]model.OpsDashboardSummary, error)
	GetDashboard(context.Context, string) (map[string]any, map[string]any, error)
	SaveDashboard(context.Context, map[string]any, string, string, bool) (DashboardSaveResult, error)
	DeleteDashboard(context.Context, string) error
	Folders(context.Context) ([]model.OpsFolder, error)
	SaveFolder(context.Context, string, string, int) (model.OpsFolder, error)
	DeleteFolder(context.Context, string) error
	DataSources(context.Context) ([]model.OpsDataSource, error)
	DataSource(context.Context, string) (model.OpsDataSource, error)
	SaveDataSource(context.Context, string, map[string]any) (model.OpsDataSource, error)
	DeleteDataSource(context.Context, string) error
	TestDataSource(context.Context, string) (string, string, error)
	QueryData(context.Context, PanelQueryRequest) (map[string][]model.DataFrame, map[string]string, error)
	DataSourceResource(context.Context, string, string, map[string][]string) (json.RawMessage, error)
}

type AlertFilter struct {
	Matchers  []string
	Active    bool
	Silenced  bool
	Inhibited bool
	Receiver  string
}

type AlertmanagerBackend interface {
	Configured() bool
	Status(context.Context) model.OpsComponentStatus
	Alerts(context.Context, AlertFilter) ([]model.OpsAlert, error)
	AlertGroups(context.Context, AlertFilter) ([]model.OpsAlertGroup, error)
	Silences(context.Context) ([]model.OpsSilence, error)
	SaveSilence(context.Context, model.OpsSilence) (string, error)
	ExpireSilence(context.Context, string) error
	PostAlerts(context.Context, []map[string]any) error
	Reload(context.Context) error
}

// ManagedFile is one platform-owned configuration file shared with a
// component (rule groups, Alertmanager configuration, Loki runtime overrides).
type ManagedFile struct {
	Name     string
	Enabled  bool
	Content  []byte
	Revision string
	ModTime  time.Time
}

type ManagedFileStore interface {
	Configured() bool
	List() ([]ManagedFile, error)
	Read(string) (ManagedFile, error)
	Write(string, []byte, bool) error
	Remove(string) error
}

// ManagedConfigFile is a single platform-owned file such as the Alertmanager
// configuration or Loki runtime overrides.
type ManagedConfigFile interface {
	Configured() bool
	Read() (ManagedFile, error)
	Write([]byte) error
}

type OpsPreferenceStore interface {
	ListOpsItems(context.Context, string, string, string, int) ([]model.OpsUserItem, error)
	SaveOpsItem(context.Context, model.OpsUserItem) error
	DeleteOpsItem(context.Context, string, string, string, string) (bool, error)
	TrimOpsItems(context.Context, string, string, string, int) error
}
