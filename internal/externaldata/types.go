// Package externaldata implements configurable, durable external HTTP data ingestion.
package externaldata

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrNotFound    = errors.New("外部数据记录不存在")
	ErrConflict    = errors.New("记录已变更，请刷新重试")
	ErrInvalid     = errors.New("外部数据配置无效")
	ErrRateLimited = errors.New("外部接口请求频率受限")
)

// Entry is the durable envelope. Revision is an optimistic concurrency fence;
// leased workers must save with the revision returned by Claim.
type Entry struct {
	TenantID   string          `json:"tenantId"`
	Kind       string          `json:"kind"`
	ID         string          `json:"id"`
	SourceID   string          `json:"sourceId,omitempty"`
	EndpointID string          `json:"endpointId,omitempty"`
	Status     string          `json:"status,omitempty"`
	DueAt      int64           `json:"dueAt,omitempty"`
	LeaseUntil int64           `json:"leaseUntil,omitempty"`
	Owner      string          `json:"-"`
	Revision   int64           `json:"revision"`
	CreatedAt  int64           `json:"createdAt"`
	UpdatedAt  int64           `json:"updatedAt"`
	Body       json.RawMessage `json:"body"`
}

type Query struct {
	TenantID, Kind, SourceID, EndpointID, Status string
	Limit, Offset                                int
	JobID                                        string
	UpdatedOrder                                 bool
}

type RuntimeSummary struct {
	LastReceivedAt  int64  `json:"lastReceivedAt"`
	LastProcessedAt int64  `json:"lastProcessedAt"`
	LastPullAt      int64  `json:"lastPullAt"`
	LastError       string `json:"lastError,omitempty"`
	LastErrorAt     int64  `json:"lastErrorAt,omitempty"`
	FailedRecords   int    `json:"failedRecords"`
	PendingRecords  int    `json:"pendingRecords"`
}

type Store interface {
	Get(context.Context, string, string, string) (Entry, error)
	List(context.Context, Query) ([]Entry, int, error)
	Put(context.Context, Entry, int64) (Entry, error)
	Delete(context.Context, string, string, string, int64) error
	// Claim considers only PENDING/RETRY or expired RUNNING rows, DueAt <= now.
	// Empty tenant is allowed only here for the trusted background worker.
	Claim(ctx context.Context, kind, owner string, now, leaseMillis int64) (Entry, error)
}

type Auth struct {
	Type             string         `json:"type"` // none, bearer, api_key, basic, hmac, token
	Header           string         `json:"header,omitempty"`
	Query            string         `json:"query,omitempty"`
	Username         string         `json:"username,omitempty"`
	Secret           string         `json:"secret,omitempty"`
	SecretSet        bool           `json:"secretSet,omitempty"`
	ClearSecret      bool           `json:"clearSecret,omitempty"`
	TimestampHeader  string         `json:"timestampHeader,omitempty"`
	TokenURL         string         `json:"tokenUrl,omitempty"`
	TokenBody        map[string]any `json:"tokenBody,omitempty"`
	TokenPath        string         `json:"tokenPath,omitempty"`
	TokenExpiresPath string         `json:"tokenExpiresPath,omitempty"`
}

type Source struct {
	RequestIntervalMillis int             `json:"requestIntervalMillis,omitempty"`
	Runtime               *RuntimeSummary `json:"runtime,omitempty"`
	ID                    string          `json:"id"`
	Revision              int64           `json:"revision"`
	Name                  string          `json:"name"`
	Username              string          `json:"username"`
	Enabled               bool            `json:"enabled"`
	Auth                  Auth            `json:"auth"`
	AllowedHosts          []string        `json:"allowedHosts"`
	Description           string          `json:"description,omitempty"`
}

type Field struct {
	Target     string         `json:"target"`
	Path       string         `json:"path,omitempty"`
	Value      any            `json:"value,omitempty"`
	Constant   bool           `json:"constant,omitempty"`
	Default    any            `json:"default,omitempty"`
	Type       string         `json:"type,omitempty"`       // string, number, boolean, timestamp, json
	TimeFormat string         `json:"timeFormat,omitempty"` // seconds, milliseconds, RFC3339 or Go layout
	Timezone   string         `json:"timezone,omitempty"`
	Values     map[string]any `json:"values,omitempty"`
	Required   bool           `json:"required,omitempty"`
}

type Filter struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}
type Mapping struct {
	ItemsPath    string   `json:"itemsPath,omitempty"`
	SuccessPath  string   `json:"successPath,omitempty"`
	SuccessValue any      `json:"successValue,omitempty"`
	Fields       []Field  `json:"fields"`
	Filters      []Filter `json:"filters,omitempty"`
	IDFields     []string `json:"idFields,omitempty"`
}

type Pagination struct {
	Mode          string `json:"mode"` // none, page, offset, cursor
	Parameter     string `json:"parameter,omitempty"`
	SizeParameter string `json:"sizeParameter,omitempty"`
	PageSize      int    `json:"pageSize,omitempty"`
	Start         int    `json:"start,omitempty"`
	NextPath      string `json:"nextPath,omitempty"`
	TotalPath     string `json:"totalPath,omitempty"`
	MaxPages      int    `json:"maxPages,omitempty"`
}

type Endpoint struct {
	Runtime         *RuntimeSummary   `json:"runtime,omitempty"`
	ID              string            `json:"id"`
	Revision        int64             `json:"revision"`
	SourceID        string            `json:"sourceId"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	Mode            string            `json:"mode"` // push, pull
	Kind            string            `json:"kind"` // video_alarm, alarm, property, state, event
	URL             string            `json:"url,omitempty"`
	Method          string            `json:"method,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Query           map[string]string `json:"query,omitempty"`
	RequestBody     map[string]any    `json:"requestBody,omitempty"`
	Auth            *Auth             `json:"auth,omitempty"` // nil inherits source authentication
	Mapping         Mapping           `json:"mapping"`
	Pagination      Pagination        `json:"pagination"`
	IntervalSeconds int               `json:"intervalSeconds,omitempty"`
	StartAt         int64             `json:"startAt,omitempty"`
	OverlapSeconds  int               `json:"overlapSeconds,omitempty"`
	TimeoutSeconds  int               `json:"timeoutSeconds,omitempty"`
	MaxAttempts     int               `json:"maxAttempts,omitempty"`
	ResponseStatus  int               `json:"responseStatus,omitempty"`
	ResponseBody    json.RawMessage   `json:"responseBody,omitempty"`
	PushKey         string            `json:"pushKey,omitempty"`
	PushKeySet      bool              `json:"pushKeySet,omitempty"`
}

type Binding struct {
	ID         string `json:"id"`
	Revision   int64  `json:"revision"`
	SourceID   string `json:"sourceId"`
	ExternalID string `json:"externalId"`
	Kind       string `json:"kind"` // device, camera
	TargetID   string `json:"targetId"`
}

type Event struct {
	ID           string         `json:"id"`
	ObjectID     string         `json:"objectId"`
	Timestamp    int64          `json:"timestamp"`
	Version      int64          `json:"version,omitempty"`
	Status       string         `json:"status,omitempty"`
	AlarmType    string         `json:"alarmType,omitempty"`
	AlarmLevel   string         `json:"alarmLevel,omitempty"`
	Content      string         `json:"content,omitempty"`
	Confidence   float64        `json:"confidence,omitempty"`
	SnapshotURL  string         `json:"snapshotUrl,omitempty"`
	VideoClipURL string         `json:"videoClipUrl,omitempty"`
	Online       *bool          `json:"online,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

type Record struct {
	ExtractionError bool            `json:"extractionError,omitempty"`
	ReceiptID       string          `json:"receiptId"`
	JobID           string          `json:"jobId,omitempty"`
	Raw             json.RawMessage `json:"raw"`
	Mapping         Mapping         `json:"mapping"`
	ConfigRevision  int64           `json:"configRevision"`
	Event           *Event          `json:"event,omitempty"`
	Attempts        int             `json:"attempts"`
	Error           string          `json:"error,omitempty"`
	DeviceID        string          `json:"deviceId,omitempty"`
	CameraID        string          `json:"cameraId,omitempty"`
	MessageID       string          `json:"messageId,omitempty"`
	AlarmID         string          `json:"alarmId,omitempty"`
}

type Job struct {
	Processed      int    `json:"processed"`
	Failed         int    `json:"failed"`
	Pending        int    `json:"pending"`
	Manual         bool   `json:"manual"`
	From           int64  `json:"from"`
	To             int64  `json:"to"`
	Page           int    `json:"page"`
	Cursor         string `json:"cursor,omitempty"`
	Pages          int    `json:"pages"`
	Received       int    `json:"received"`
	Attempts       int    `json:"attempts"`
	Error          string `json:"error,omitempty"`
	ConfigRevision int64  `json:"configRevision"`
}

type Result struct {
	DeviceID  string   `json:"deviceId,omitempty"`
	CameraID  string   `json:"cameraId,omitempty"`
	MessageID string   `json:"messageId,omitempty"`
	AlarmID   string   `json:"alarmId,omitempty"`
	AlarmIDs  []string `json:"alarmIds,omitempty"`
}
