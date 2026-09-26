package model

// Ops center models describe infrastructure observability data. They are
// separate from fire-protection business alarms and never carry tenant data.

type OpsComponentStatus struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Configured bool           `json:"configured"`
	State      string         `json:"state"`
	Version    string         `json:"version,omitempty"`
	Message    string         `json:"message,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
	CheckedAt  int64          `json:"checkedAt"`
}

// MetricSeries stores points column-wise so charts can consume them directly.
// Values are nil for NaN and infinities, which JSON cannot represent.
type MetricSeries struct {
	Labels     map[string]string `json:"labels"`
	Timestamps []int64           `json:"timestamps"`
	Values     []*float64        `json:"values"`
}

type MetricQueryResult struct {
	Query      string         `json:"query"`
	ResultType string         `json:"resultType"`
	Series     []MetricSeries `json:"series"`
	Truncated  bool           `json:"truncated"`
	Limit      int            `json:"limit"`
	Start      int64          `json:"start,omitempty"`
	End        int64          `json:"end,omitempty"`
	StepMs     int64          `json:"stepMs,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Infos      []string       `json:"infos,omitempty"`
}

type MetricInfo struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Help string `json:"help,omitempty"`
	Unit string `json:"unit,omitempty"`
}

type ScrapeTarget struct {
	Job                string            `json:"job"`
	Instance           string            `json:"instance"`
	Labels             map[string]string `json:"labels"`
	Health             string            `json:"health"`
	LastError          string            `json:"lastError,omitempty"`
	LastScrape         string            `json:"lastScrape,omitempty"`
	LastScrapeDuration float64           `json:"lastScrapeDuration"`
	ScrapeInterval     string            `json:"scrapeInterval,omitempty"`
	ScrapeTimeout      string            `json:"scrapeTimeout,omitempty"`
	MetricsPath        string            `json:"metricsPath,omitempty"`
}

// LogEntry keeps the nanosecond timestamp as a string because browsers cannot
// hold 64-bit integers exactly.
type LogEntry struct {
	Timestamp string            `json:"ts"`
	TimeMs    int64             `json:"timeMs"`
	Line      string            `json:"line"`
	Labels    map[string]string `json:"labels"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Parsed    map[string]string `json:"parsed,omitempty"`
}

type LogQueryResult struct {
	Query      string         `json:"query"`
	ResultType string         `json:"resultType"`
	Entries    []LogEntry     `json:"entries"`
	Series     []MetricSeries `json:"series,omitempty"`
	Truncated  bool           `json:"truncated"`
	Limit      int            `json:"limit"`
	Direction  string         `json:"direction,omitempty"`
	NextCursor string         `json:"nextCursor,omitempty"`
	Start      int64          `json:"start,omitempty"`
	End        int64          `json:"end,omitempty"`
	Stats      map[string]any `json:"stats,omitempty"`
}

type LogDeleteRequest struct {
	RequestID string  `json:"requestId"`
	Query     string  `json:"query"`
	Status    string  `json:"status"`
	StartTime float64 `json:"startTime"`
	EndTime   float64 `json:"endTime"`
	CreatedAt float64 `json:"createdAt"`
}

type LogRetentionStream struct {
	Selector string         `json:"selector"`
	Matchers []LabelMatcher `json:"matchers,omitempty" yaml:"-"`
	Priority int            `json:"priority"`
	Period   string         `json:"period"`
}

// LabelMatcher is one label condition of a PromQL/LogQL selector.
type LabelMatcher struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type LogRetentionSettings struct {
	Revision         string               `json:"revision"`
	Writable         bool                 `json:"writable"`
	RetentionEnabled bool                 `json:"retentionEnabled"`
	DeletionMode     string               `json:"deletionMode,omitempty"`
	GlobalPeriod     string               `json:"globalPeriod,omitempty"`
	CancelPeriod     string               `json:"cancelPeriod,omitempty"`
	MaxQueryLength   string               `json:"maxQueryLength,omitempty"`
	Period           string               `json:"period"`
	Streams          []LogRetentionStream `json:"streams"`
	Loaded           *bool                `json:"loaded,omitempty"`
	UpdatedAt        int64                `json:"updatedAt,omitempty"`
	UpdatedBy        string               `json:"updatedBy,omitempty"`
}

type OpsRule struct {
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Expr           string            `json:"expr"`
	For            string            `json:"for,omitempty"`
	KeepFiringFor  string            `json:"keepFiringFor,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	Health         string            `json:"health,omitempty"`
	LastError      string            `json:"lastError,omitempty"`
	State          string            `json:"state,omitempty"`
	LastEvaluation string            `json:"lastEvaluation,omitempty"`
	EvaluationTime float64           `json:"evaluationTime,omitempty"`
	ActiveAlerts   int               `json:"activeAlerts,omitempty"`
}

type OpsRuleGroup struct {
	Source    string    `json:"source"`
	Name      string    `json:"name"`
	Interval  string    `json:"interval,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	File      string    `json:"file,omitempty"`
	Managed   bool      `json:"managed"`
	Enabled   bool      `json:"enabled"`
	Loaded    bool      `json:"loaded"`
	Revision  string    `json:"revision,omitempty"`
	UpdatedAt int64     `json:"updatedAt,omitempty"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	Rules     []OpsRule `json:"rules"`
}

type OpsMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

type OpsAlert struct {
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
	EndsAt      string            `json:"endsAt,omitempty"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
	State       string            `json:"state"`
	SilencedBy  []string          `json:"silencedBy"`
	InhibitedBy []string          `json:"inhibitedBy"`
	Receivers   []string          `json:"receivers"`
	Expr        string            `json:"expr,omitempty"`
}

type OpsAlertGroup struct {
	Labels   map[string]string `json:"labels"`
	Receiver string            `json:"receiver"`
	Alerts   []OpsAlert        `json:"alerts"`
}

type OpsSilence struct {
	ID        string       `json:"id"`
	Matchers  []OpsMatcher `json:"matchers"`
	StartsAt  string       `json:"startsAt"`
	EndsAt    string       `json:"endsAt"`
	UpdatedAt string       `json:"updatedAt,omitempty"`
	CreatedBy string       `json:"createdBy"`
	Comment   string       `json:"comment"`
	State     string       `json:"state"`
}

type OpsAlertHistoryItem struct {
	Labels map[string]string `json:"labels"`
	Start  int64             `json:"start"`
	End    int64             `json:"end"`
	Active bool              `json:"active"`
}

// OpsSecret is how credential fields cross the API boundary: responses only
// say whether a value exists, requests choose keep, replace or clear.
type OpsSecret struct {
	Set   bool   `json:"set"`
	Hint  string `json:"hint,omitempty"`
	Mode  string `json:"mode,omitempty"`
	Value string `json:"value,omitempty"`
}

type OpsWebhookReceiver struct {
	SourceIndex  *int      `json:"sourceIndex,omitempty"`
	URL          OpsSecret `json:"url"`
	BearerToken  OpsSecret `json:"bearerToken"`
	SendResolved bool      `json:"sendResolved"`
	MaxAlerts    int       `json:"maxAlerts,omitempty"`
	Timeout      string    `json:"timeout,omitempty"`
}

type OpsEmailReceiver struct {
	SourceIndex  *int      `json:"sourceIndex,omitempty"`
	To           string    `json:"to"`
	From         string    `json:"from,omitempty"`
	Smarthost    string    `json:"smarthost,omitempty"`
	AuthUsername string    `json:"authUsername,omitempty"`
	AuthPassword OpsSecret `json:"authPassword"`
	RequireTLS   *bool     `json:"requireTls,omitempty"`
	SendResolved bool      `json:"sendResolved"`
}

type OpsReceiver struct {
	Name         string               `json:"name"`
	OriginalName string               `json:"originalName,omitempty"`
	Webhooks     []OpsWebhookReceiver `json:"webhooks"`
	Emails       []OpsEmailReceiver   `json:"emails"`
	ReadOnly     []string             `json:"readOnly,omitempty"`
}

type OpsRoute struct {
	Receiver          string       `json:"receiver,omitempty"`
	GroupBy           []string     `json:"groupBy,omitempty"`
	GroupWait         string       `json:"groupWait,omitempty"`
	GroupInterval     string       `json:"groupInterval,omitempty"`
	RepeatInterval    string       `json:"repeatInterval,omitempty"`
	Matchers          []OpsMatcher `json:"matchers,omitempty"`
	Continue          bool         `json:"continue,omitempty"`
	MuteTimeIntervals []string     `json:"muteTimeIntervals,omitempty"`
	Routes            []OpsRoute   `json:"routes,omitempty"`
}

type OpsNotificationConfig struct {
	DeviceAlarmReceiver string        `json:"deviceAlarmReceiver"`
	DeviceAlarmSince    int64         `json:"deviceAlarmSince,omitempty"`
	Revision            string        `json:"revision"`
	Writable            bool          `json:"writable"`
	Route               OpsRoute      `json:"route"`
	Receivers           []OpsReceiver `json:"receivers"`
	InhibitRules        int           `json:"inhibitRules"`
	Preserved           []string      `json:"preserved,omitempty"`
	UpdatedAt           int64         `json:"updatedAt,omitempty"`
	UpdatedBy           string        `json:"updatedBy,omitempty"`
}

type OpsDashboardSummary struct {
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	FolderUID   string   `json:"folderUid,omitempty"`
	FolderTitle string   `json:"folderTitle,omitempty"`
	Tags        []string `json:"tags"`
	Favorite    bool     `json:"favorite"`
}

type OpsFolder struct {
	UID       string `json:"uid"`
	Title     string `json:"title"`
	ParentUID string `json:"parentUid,omitempty"`
	CanEdit   bool   `json:"canEdit"`
	Version   int    `json:"version,omitempty"`
}

type OpsDataSource struct {
	UID           string          `json:"uid"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	IsDefault     bool            `json:"isDefault"`
	ReadOnly      bool            `json:"readOnly"`
	Supported     bool            `json:"supported"`
	URL           string          `json:"url,omitempty"`
	Access        string          `json:"access,omitempty"`
	BasicAuth     bool            `json:"basicAuth,omitempty"`
	BasicAuthUser string          `json:"basicAuthUser,omitempty"`
	JSONData      map[string]any  `json:"jsonData,omitempty"`
	SecureFields  map[string]bool `json:"secureFields,omitempty"`
}

// DataFrame is the normalized form of Grafana data frames returned by panel
// queries. Values are column-major, matching Grafana's wire format.
type DataFrame struct {
	RefID  string         `json:"refId"`
	Name   string         `json:"name,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
	Fields []DataField    `json:"fields"`
}

type DataField struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`
	Config map[string]any    `json:"config,omitempty"`
	Values []any             `json:"values"`
}

type OpsUserItem struct {
	TenantID  string         `json:"-"`
	Username  string         `json:"-"`
	Kind      string         `json:"kind"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Body      map[string]any `json:"body"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
}
