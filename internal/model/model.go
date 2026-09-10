package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TopicRaw         = "iot.raw.message"
	TopicParseFailed = "iot.parse.failed"
	// TopicParsed is the Kafka topic for parsed message types that do not have
	// a more specific downstream topic yet (for example command replies and
	// device logs). Raw messages stay on TopicRaw for the internal parser
	// pipeline; external consumers must use the parsed topics below.
	TopicParsed          = "iot.parsed.message"
	TopicPropertyReport  = "iot.property.report"
	TopicEventReport     = "iot.event.report"
	TopicDeviceState     = "iot.device.state"
	TopicVideoAlarm      = "iot.video.alarm"
	TopicAlarmRaised     = "iot.alarm.raised"
	TopicAlarmRecovered  = "iot.alarm.recovered"
	TopicAlarmConfirmed  = "iot.alarm.confirmed"
	TopicAlarmAIAnalysis = "iot.alarm.ai-analysis"
	TopicUIAction        = "iot.ui-action"
	TopicReplayRequest   = "iot.replay.request"
)

// ErrRawConflict indicates reuse of a message identity with different content.
var ErrRawConflict = errors.New("message id already exists with different content")

type RawMessage struct {
	MessageID         string            `json:"messageId"`
	Source            string            `json:"source"`
	TenantID          string            `json:"tenantId"`
	ProductID         string            `json:"productId"`
	DeviceID          string            `json:"deviceId"`
	DeviceName        string            `json:"deviceName,omitempty"`
	GatewayID         string            `json:"gatewayId,omitempty"`
	Protocol          string            `json:"protocol"`
	Transport         string            `json:"transport"`
	ReceivedAt        int64             `json:"receivedAt"`
	PayloadFormat     string            `json:"payloadFormat"`
	Payload           json.RawMessage   `json:"payload"`
	ClientID          string            `json:"clientId,omitempty"`
	RemoteAddress     string            `json:"remoteAddress,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	ParserVersion     string            `json:"parserVersion,omitempty"`
	ProtocolID        string            `json:"protocolId,omitempty"`
	ProtocolVersion   string            `json:"protocolVersion,omitempty"`
	PointTableVersion string            `json:"pointTableVersion,omitempty"`
	CollectorID       string            `json:"collectorId,omitempty"`
	Metadata          map[string]any    `json:"metadata,omitempty"`
}

func (m *RawMessage) Normalize(now time.Time) {
	if m.MessageID == "" {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%s", m.TenantID, m.DeviceID, now.UnixNano(), m.Payload)))
		m.MessageID = "raw_" + hex.EncodeToString(h[:12])
	}
	if m.Source == "" {
		m.Source = "external-ingest"
	}
	if m.ReceivedAt == 0 {
		m.ReceivedAt = now.UnixMilli()
	}
	if m.PayloadFormat == "" {
		m.PayloadFormat = "json"
	}
}

func (m RawMessage) Validate() error {
	missing := make([]string, 0, 4)
	for name, value := range map[string]string{"messageId": m.MessageID, "tenantId": m.TenantID, "productId": m.ProductID, "deviceId": m.DeviceID} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	if len(m.Payload) == 0 {
		return errors.New("payload is required")
	}
	return nil
}

func (m RawMessage) PayloadHash() string {
	h := sha256.Sum256(m.Payload)
	return hex.EncodeToString(h[:])
}

type RawArchiveIndex struct {
	MessageID        string `json:"messageId"`
	TenantID         string `json:"tenantId"`
	ProductID        string `json:"productId"`
	DeviceID         string `json:"deviceId"`
	Protocol         string `json:"protocol"`
	PayloadFormat    string `json:"payloadFormat"`
	ObjectBucket     string `json:"objectBucket"`
	ObjectKey        string `json:"objectKey"`
	ObjectOffset     int64  `json:"objectOffset"`
	PayloadHash      string `json:"payloadHash"`
	PayloadSize      int    `json:"payloadSize"`
	ReceivedAt       int64  `json:"receivedAt"`
	ArchivedAt       int64  `json:"archivedAt"`
	PublishedAt      int64  `json:"publishedAt,omitempty"`
	PublishAttempts  int    `json:"publishAttempts,omitempty"`
	LastPublishError string `json:"lastPublishError,omitempty"`
	ParseAttemptedAt int64  `json:"parseAttemptedAt,omitempty"`
	ParseError       string `json:"parseError,omitempty"`
	// Parsed and parsedMessageType are response metadata populated by the API;
	// they are not persisted in the archive index table.
	Parsed            bool   `json:"parsed,omitempty"`
	ParsedMessageType string `json:"parsedMessageType,omitempty"`
	Parser            string `json:"parser,omitempty"`
}

type MessageType string

const (
	PropertyReport MessageType = "PROPERTY_REPORT"
	EventReport    MessageType = "EVENT_REPORT"
	StateChange    MessageType = "STATE_CHANGE"
	AlarmReport    MessageType = "ALARM_REPORT"
	CommandReply   MessageType = "COMMAND_REPLY"
	LogReport      MessageType = "LOG_REPORT"
)

type StandardMessage struct {
	MessageID     string            `json:"messageId"`
	RawMessageID  string            `json:"rawMessageId"`
	TenantID      string            `json:"tenantId"`
	ProductID     string            `json:"productId"`
	DeviceID      string            `json:"deviceId"`
	MessageType   MessageType       `json:"messageType"`
	Timestamp     int64             `json:"timestamp"`
	Properties    map[string]any    `json:"properties,omitempty"`
	Event         map[string]any    `json:"event,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	Raw           map[string]any    `json:"raw,omitempty"`
	Parser        string            `json:"parser"`
	ParserVersion string            `json:"parserVersion"`
}

// MQTTTopic returns the tenant-scoped external topic for every successfully
// parsed standard message. Keep tenant/product/device before messageType so a
// client can subscribe to /iot/parsed/{tenant}/# without crossing tenants.
func (m StandardMessage) MQTTTopic() string {
	clean := func(value string) string {
		value = strings.Trim(strings.ReplaceAll(value, "/", "_"), " ")
		if value == "" {
			return "unknown"
		}
		return value
	}
	return fmt.Sprintf("/iot/parsed/%s/%s/%s/%s", clean(m.TenantID), clean(m.ProductID), clean(m.DeviceID), clean(string(m.MessageType)))
}

type DeviceState struct {
	TenantID            string `json:"tenantId"`
	ProductID           string `json:"productId"`
	DeviceID            string `json:"deviceId"`
	ConnectionStatus    string `json:"connectionStatus"`
	DataStatus          string `json:"dataStatus"`
	BusinessStatus      string `json:"businessStatus"`
	LastConnectAt       int64  `json:"lastConnectAt,omitempty"`
	LastDisconnectAt    int64  `json:"lastDisconnectAt,omitempty"`
	LastSeenAt          int64  `json:"lastSeenAt,omitempty"`
	LastMessageID       string `json:"lastMessageId,omitempty"`
	ReportIntervalSec   int64  `json:"reportIntervalSec"`
	OfflineToleranceSec int64  `json:"offlineToleranceSec"`
	OfflineAt           int64  `json:"offlineAt,omitempty"`
	OfflineDetectedAt   int64  `json:"offlineDetectedAt,omitempty"`
	StatusSource        string `json:"statusSource"`
	Reason              string `json:"reason,omitempty"`
}

// Product describes a managed device product and the protocol package used to
// turn its raw payloads into standard platform messages.
type Product struct {
	ThingModel        *ThingModel    `json:"thingModel,omitempty"`
	ID                string         `json:"id"`
	TenantID          string         `json:"tenantId"`
	Name              string         `json:"name"`
	Category          string         `json:"category"`
	ProtocolPackageID string         `json:"protocolPackageId"`
	Transport         string         `json:"transport"`
	PayloadFormat     string         `json:"payloadFormat"`
	Status            string         `json:"status"`
	Description       string         `json:"description,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         int64          `json:"createdAt"`
	UpdatedAt         int64          `json:"updatedAt"`
}

// ProtocolPackage is a declarative protocol-package release. ParserType points
// at a reviewed parser registered in the Go process. The sandbox JavaScript
// parser stores a pure transformation source in Config; go_protocol_parser
// stores a checksum-verified, compiled worker artifact in Config and invokes it
// through the external JSON-lines contract.
type ProtocolPackage struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenantId"`
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	Protocol      string         `json:"protocol"`
	Transport     string         `json:"transport"`
	PayloadFormat string         `json:"payloadFormat"`
	ParserType    string         `json:"parserType"`
	Status        string         `json:"status"`
	Description   string         `json:"description,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
	CreatedAt     int64          `json:"createdAt"`
	UpdatedAt     int64          `json:"updatedAt"`
}

// ProtocolDefinition is the stable identity of a protocol family. Executable
// and mapping content lives in immutable ProtocolRelease records.
type ProtocolDefinition struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	Vendor      string `json:"vendor,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// ModbusPoint is a normalized point-table row. Address is always the zero
// based PDU address; AddressNotation records how the uploaded value was
// interpreted so that conversions remain auditable.
type ModbusPoint struct {
	Identifier      string         `json:"identifier"`
	Name            string         `json:"name"`
	FunctionCode    int            `json:"functionCode"`
	Address         int            `json:"address"`
	AddressNotation string         `json:"addressNotation"`
	DataType        string         `json:"dataType"`
	RegisterCount   int            `json:"registerCount,omitempty"`
	ByteOrder       string         `json:"byteOrder,omitempty"`
	WordOrder       string         `json:"wordOrder,omitempty"`
	Bit             *int           `json:"bit,omitempty"`
	Scale           float64        `json:"scale,omitempty"`
	Offset          float64        `json:"offset,omitempty"`
	Unit            string         `json:"unit,omitempty"`
	Access          string         `json:"access,omitempty"`
	PollIntervalSec int            `json:"pollIntervalSec,omitempty"`
	Deadband        float64        `json:"deadband,omitempty"`
	AlarmMapping    map[string]any `json:"alarmMapping,omitempty"`
	Description     string         `json:"description,omitempty"`
}

type ModbusReadBlock struct {
	ID              string `json:"id"`
	FunctionCode    int    `json:"functionCode"`
	StartAddress    int    `json:"startAddress"`
	Quantity        int    `json:"quantity"`
	PollIntervalSec int    `json:"pollIntervalSec"`
}

type PointTableRelease struct {
	TenantID     string        `json:"tenantId"`
	ProtocolID   string        `json:"protocolId"`
	Version      string        `json:"version"`
	SourceName   string        `json:"sourceName,omitempty"`
	SourceSHA256 string        `json:"sourceSha256,omitempty"`
	Points       []ModbusPoint `json:"points"`
	CreatedAt    int64         `json:"createdAt"`
}

type ProtocolRelease struct {
	TenantID          string         `json:"tenantId"`
	ProtocolID        string         `json:"protocolId"`
	Version           string         `json:"version"`
	Transport         string         `json:"transport"`
	PayloadFormat     string         `json:"payloadFormat"`
	ParserType        string         `json:"parserType"`
	Status            string         `json:"status"`
	PointTableVersion string         `json:"pointTableVersion,omitempty"`
	Capabilities      []string       `json:"capabilities,omitempty"`
	Config            map[string]any `json:"config,omitempty"`
	Artifact          map[string]any `json:"artifact,omitempty"`
	CreatedAt         int64          `json:"createdAt"`
	PublishedAt       int64          `json:"publishedAt,omitempty"`
}

type ProductProtocolBinding struct {
	TenantID           string `json:"tenantId"`
	ProductID          string `json:"productId"`
	ProtocolID         string `json:"protocolId"`
	Version            string `json:"version"`
	PreviousVersion    string `json:"previousVersion,omitempty"`
	PreviousProtocolID string `json:"previousProtocolId,omitempty"`
	UpdatedAt          int64  `json:"updatedAt"`
}

type DeviceAccessProfile struct {
	ConnectionMode string                `json:"connectionMode,omitempty"` // listen (default) or dial
	WireFormat     string                `json:"wireFormat,omitempty"`     // modbus_tcp (default) or rtu_over_tcp
	Queries        []ProtocolQuery       `json:"queries,omitempty"`
	ChildProducts  []ChildProductBinding `json:"childProducts,omitempty"`

	CredentialRef string `json:"credentialRef,omitempty"`
	EndpointPath  string `json:"endpointPath,omitempty"`
	SerialPort    string `json:"serialPort,omitempty"`
	BaudRate      int    `json:"baudRate,omitempty"`
	Parity        string `json:"parity,omitempty"`
	StopBits      int    `json:"stopBits,omitempty"`
	// EdgeNodeID is retained only to reject legacy remote assignments.
	EdgeNodeID        string `json:"edgeNodeId,omitempty"`
	Mode              string `json:"mode,omitempty"`
	Network           string `json:"network,omitempty"`
	AutoRegister      bool   `json:"autoRegister,omitempty"`
	ID                string `json:"id"`
	TenantID          string `json:"tenantId"`
	DeviceID          string `json:"deviceId"`
	ProductID         string `json:"productId"`
	ProtocolID        string `json:"protocolId"`
	ProtocolVersion   string `json:"protocolVersion"`
	PointTableVersion string `json:"pointTableVersion"`
	CollectorID       string `json:"collectorId,omitempty"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	UnitID            int    `json:"unitId"`
	TimeoutMs         int    `json:"timeoutMs"`
	Retries           int    `json:"retries"`
	Enabled           bool   `json:"enabled"`
	RuntimeStatus     string `json:"runtimeStatus"`
	LastSuccessAt     int64  `json:"lastSuccessAt,omitempty"`
	LastErrorAt       int64  `json:"lastErrorAt,omitempty"`
	LastError         string `json:"lastError,omitempty"`
	CreatedAt         int64  `json:"createdAt"`
	UpdatedAt         int64  `json:"updatedAt"`
}

// Configuration clears only runtime observations; UpdatedAt remains the configuration revision.
func (p DeviceAccessProfile) Configuration() string {
	p.RuntimeStatus, p.LastError = "", ""
	p.LastSuccessAt, p.LastErrorAt = 0, 0
	b, err := json.Marshal(p)
	if err != nil {
		return "invalid"
	}
	return string(b)
}

// ProtocolAssistantField is the editable address/mapping contract between the
// protocol assistant and the form-based protocol designer. Expression remains
// optional for backwards-compatible drafts, but the assistant no longer
// generates or executes JavaScript.
type ProtocolAssistantField struct {
	Name          string `json:"name"`
	Label         string `json:"label,omitempty"`
	Type          string `json:"type,omitempty"`
	Expression    string `json:"expression,omitempty"`
	Address       string `json:"address,omitempty"`
	CoilAddress   int    `json:"coilAddress"`
	ModbusAddress int    `json:"modbusAddress"`
	DataType      string `json:"dataType,omitempty"`
	NormalValue   string `json:"normalValue,omitempty"`
	ReportValue   string `json:"reportValue,omitempty"`
	Description   string `json:"description,omitempty"`
}

type ProtocolAssistantDraft struct {
	Name           string                   `json:"name"`
	Description    string                   `json:"description,omitempty"`
	Protocol       string                   `json:"protocol"`
	Transport      string                   `json:"transport"`
	PayloadFormat  string                   `json:"payloadFormat"`
	ParserType     string                   `json:"parserType"`
	MessageType    MessageType              `json:"messageType"`
	Setup          string                   `json:"setup,omitempty"`
	Source         string                   `json:"source,omitempty"`
	Config         map[string]any           `json:"config,omitempty"`
	Fields         []ProtocolAssistantField `json:"fields"`
	TagExpressions map[string]string        `json:"tagExpressions,omitempty"`
	SamplePayload  any                      `json:"samplePayload,omitempty"`
	Warnings       []string                 `json:"warnings,omitempty"`
	Preview        *StandardMessage         `json:"preview,omitempty"`
}

type DeviceHealthItem struct {
	DeviceID         string   `json:"deviceId"`
	DeviceName       string   `json:"deviceName,omitempty"`
	ProductID        string   `json:"productId"`
	BusinessStatus   string   `json:"businessStatus"`
	DataStatus       string   `json:"dataStatus"`
	LastSeenAt       int64    `json:"lastSeenAt,omitempty"`
	ActiveAlarmCount int      `json:"activeAlarmCount"`
	Severity         string   `json:"severity"`
	Findings         []string `json:"findings"`
}

type DeviceHealthReport struct {
	TenantID    string             `json:"tenantId,omitempty"`
	GeneratedAt int64              `json:"generatedAt"`
	Summary     string             `json:"summary"`
	AIAdvice    string             `json:"aiAdvice,omitempty"`
	Counts      map[string]int     `json:"counts"`
	Items       []DeviceHealthItem `json:"items"`
	Warnings    []string           `json:"warnings,omitempty"`
}

// ManagedDevice is the inventory/control-plane record. Runtime connectivity is
// kept separately in DeviceState and joined by the API.
type ManagedDevice struct {
	ID                 string            `json:"id"`
	TenantID           string            `json:"tenantId"`
	ProductID          string            `json:"productId"`
	Name               string            `json:"name"`
	Status             string            `json:"status"`
	DeviceRole         string            `json:"deviceRole"`
	GatewayID          string            `json:"gatewayId,omitempty"`
	RegistrationSource string            `json:"registrationSource,omitempty"`
	AutoRegistered     bool              `json:"autoRegistered,omitempty"`
	AccessKey          string            `json:"accessKey"`
	SecretHash         string            `json:"-"`
	SecretHint         string            `json:"secretHint,omitempty"`
	Description        string            `json:"description,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
	CreatedAt          int64             `json:"createdAt"`
	UpdatedAt          int64             `json:"updatedAt"`
}

type DeviceCredential struct {
	AccessKey string `json:"accessKey"`
	Secret    string `json:"secret"`
}

type RuleCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

type RuleAction struct {
	Type     string `json:"type"`
	CameraID string `json:"cameraId,omitempty"`
	Page     string `json:"page,omitempty"`
}

type AlarmRule struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenantId"`
	ProductID       string          `json:"productId,omitempty"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	AlarmType       string          `json:"alarmType"`
	Level           string          `json:"level"`
	Conditions      []RuleCondition `json:"conditions"`
	Match           string          `json:"match"`
	DurationSeconds int64           `json:"durationSeconds"`
	Recovery        []RuleCondition `json:"recovery,omitempty"`
	Actions         []RuleAction    `json:"actions,omitempty"`
	Expression      string          `json:"expression,omitempty"`
	Enabled         bool            `json:"enabled"`
	Version         int             `json:"version"`
	CreatedAt       int64           `json:"createdAt"`
	UpdatedAt       int64           `json:"updatedAt"`
}

// RulePending records the first observation of a duration-based rule match.
// It is persisted so a process restart does not silently reset the timer.
type RulePending struct {
	TenantID  string `json:"tenantId"`
	RuleID    string `json:"ruleId"`
	DeviceID  string `json:"deviceId"`
	Since     int64  `json:"since"`
	UpdatedAt int64  `json:"updatedAt"`
}

type UIActionEvent struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenantId"`
	RuleID      string     `json:"ruleId"`
	AlarmID     string     `json:"alarmId"`
	DeviceID    string     `json:"deviceId"`
	Action      RuleAction `json:"action"`
	TriggeredAt int64      `json:"triggeredAt"`
}

type Alarm struct {
	ComponentID       string          `json:"componentId,omitempty"`
	ComponentName     string          `json:"componentName,omitempty"`
	ComponentLocation string          `json:"componentLocation,omitempty"`
	ID                string          `json:"alarmId"`
	TenantID          string          `json:"tenantId"`
	RuleID            string          `json:"ruleId"`
	TriggerID         string          `json:"triggerId,omitempty"`
	DeviceID          string          `json:"deviceId"`
	DeviceName        string          `json:"deviceName,omitempty"`
	AlarmType         string          `json:"alarmType"`
	AlarmLevel        string          `json:"alarmLevel"`
	Status            string          `json:"status"`
	Source            string          `json:"source"`
	CityCode          string          `json:"cityCode"`
	DistrictCode      string          `json:"districtCode"`
	BuildingID        string          `json:"buildingId"`
	DeviceType        string          `json:"deviceType"`
	AreaID            string          `json:"areaId,omitempty"`
	FirstTriggeredAt  int64           `json:"firstTriggeredAt"`
	LastTriggeredAt   int64           `json:"lastTriggeredAt"`
	TriggerCount      int             `json:"triggerCount"`
	RecoveredAt       int64           `json:"recoveredAt,omitempty"`
	AckedAt           int64           `json:"ackedAt,omitempty"`
	ClosedAt          int64           `json:"closedAt,omitempty"`
	Confidence        float64         `json:"confidence,omitempty"`
	MultiSource       bool            `json:"multiSource"`
	Cameras           []CameraSummary `json:"cameras,omitempty"`
	Details           map[string]any  `json:"details,omitempty"`
}

func (a Alarm) MQTTTopic(eventType string) string {
	clean := func(v string) string {
		v = strings.Trim(strings.ReplaceAll(v, "/", "_"), " ")
		if v == "" {
			return "unknown"
		}
		return v
	}
	return fmt.Sprintf("/iot/alarm/%s/%s/%s/%s/%s/%s", clean(eventType), clean(a.CityCode), clean(a.DistrictCode), clean(a.BuildingID), clean(a.DeviceType), clean(a.DeviceID))
}

type VideoAlarmEvent struct {
	EventID      string         `json:"eventId"`
	Source       string         `json:"source"`
	TenantID     string         `json:"tenantId"`
	ProjectID    string         `json:"projectId"`
	CameraID     string         `json:"cameraId"`
	CameraName   string         `json:"cameraName"`
	AreaID       string         `json:"areaId"`
	AlarmType    string         `json:"alarmType"`
	AlarmName    string         `json:"alarmName"`
	AlarmLevel   string         `json:"alarmLevel"`
	Confidence   float64        `json:"confidence"`
	EventTime    int64          `json:"eventTime"`
	ReceivedAt   int64          `json:"receivedAt"`
	SnapshotURL  string         `json:"snapshotUrl,omitempty"`
	VideoClipURL string         `json:"videoClipUrl,omitempty"`
	CityCode     string         `json:"cityCode"`
	DistrictCode string         `json:"districtCode"`
	BuildingID   string         `json:"buildingId"`
	Location     map[string]any `json:"location,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
}

type AIAnalysis struct {
	TenantID        string   `json:"tenantId,omitempty"`
	AlarmID         string   `json:"alarmId"`
	Summary         string   `json:"summary"`
	PossibleReasons []string `json:"possibleReasons"`
	Suggestions     []string `json:"suggestions"`
	RiskLevel       string   `json:"riskLevel"`
	Confidence      float64  `json:"confidence"`
	Model           string   `json:"model"`
	PromptVersion   string   `json:"promptVersion"`
	CreatedAt       int64    `json:"createdAt"`
	Error           string   `json:"error,omitempty"`
}

type KnowledgeDoc struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenantId"`
	WorkflowID   string         `json:"workflowId"`
	ProductID    string         `json:"productId,omitempty"`
	Category     string         `json:"category,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	ObjectBucket string         `json:"objectBucket"`
	ObjectKey    string         `json:"objectKey"`
	Filename     string         `json:"filename"`
	Status       string         `json:"status"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    int64          `json:"createdAt"`
}

// KnowledgeChunk is the extracted text slice that is sent to the vector
// index. Character offsets are Unicode-code-point offsets in the normalized
// extracted text, with StartChar inclusive and EndChar exclusive.
type KnowledgeChunk struct {
	DocumentID     string `json:"documentId"`
	ChunkID        string `json:"chunkId"`
	Index          int    `json:"index"`
	StartChar      int    `json:"startChar"`
	EndChar        int    `json:"endChar"`
	CharacterCount int    `json:"characterCount"`
	OverlapChars   int    `json:"overlapChars"`
	Content        string `json:"content"`
	Vectorized     bool   `json:"vectorized"`
}

// WorkflowKnowledgeBinding constrains knowledge retrieval for one tenant and
// one Harness workflow. The plugin manifest defines what the workflow can do;
// this record defines which tenant knowledge it may use.
type WorkflowKnowledgeBinding struct {
	TenantID      string   `json:"tenantId"`
	WorkflowID    string   `json:"workflowId"`
	ProductIDs    []string `json:"productIds,omitempty"`
	Categories    []string `json:"categories,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	RetrievalMode string   `json:"retrievalMode"`
	TopK          int      `json:"topK"`
	MinScore      float64  `json:"minScore"`
	NoMatchPolicy string   `json:"noMatchPolicy"`
	UpdatedAt     int64    `json:"updatedAt"`
}

type ReplayRequest struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenantId"`
	ProductID     string         `json:"productId,omitempty"`
	DeviceID      string         `json:"deviceId,omitempty"`
	Start         int64          `json:"start"`
	End           int64          `json:"end"`
	ParserVersion string         `json:"parserVersion,omitempty"`
	Mode          string         `json:"mode"`
	RatePerSecond int            `json:"ratePerSecond"`
	Status        string         `json:"status"`
	Processed     int            `json:"processed"`
	Failed        int            `json:"failed"`
	CreatedBy     string         `json:"createdBy"`
	CreatedAt     int64          `json:"createdAt"`
	CompletedAt   int64          `json:"completedAt,omitempty"`
	DiffSummary   map[string]int `json:"diffSummary,omitempty"`
	Diffs         []ReplayDiff   `json:"diffs,omitempty"`
}

type ReplayDiff struct {
	RawMessageID string           `json:"rawMessageId"`
	Status       string           `json:"status"`
	Previous     *StandardMessage `json:"previous,omitempty"`
	Current      *StandardMessage `json:"current,omitempty"`
	Error        string           `json:"error,omitempty"`
}

type VideoCameraMapping struct {
	TenantID         string   `json:"tenantId"`
	CameraID         string   `json:"cameraId"`
	CameraName       string   `json:"cameraName"`
	Brand            string   `json:"brand,omitempty"`
	CameraPoint      string   `json:"cameraPoint,omitempty"`
	DeviceID         string   `json:"deviceId,omitempty"`
	IngestMode       string   `json:"ingestMode,omitempty"`
	ProjectID        string   `json:"projectId,omitempty"`
	CityCode         string   `json:"cityCode,omitempty"`
	DistrictCode     string   `json:"districtCode,omitempty"`
	Building         string   `json:"building,omitempty"`
	Floor            string   `json:"floor,omitempty"`
	Room             string   `json:"room,omitempty"`
	AreaID           string   `json:"areaId"`
	RelatedDeviceIDs []string `json:"relatedDeviceIds,omitempty"`
	RelatedFloorIDs  []string `json:"relatedFloorIds,omitempty"`
	RelatedRoomIDs   []string `json:"relatedRoomIds,omitempty"`
	VideoPlatformID  string   `json:"videoPlatformId,omitempty"`
	StreamURL        string   `json:"streamUrl,omitempty"`
	StreamType       string   `json:"streamType,omitempty"`
	SDKEndpoint      string   `json:"sdkEndpoint,omitempty"`
	SDKCameraID      string   `json:"sdkCameraId,omitempty"`
	SDKCredentialRef string   `json:"sdkCredentialRef,omitempty"`
	StreamConfigured bool     `json:"streamConfigured,omitempty"`
	PreviewEligible  bool     `json:"previewEligible,omitempty"`
	Enabled          bool     `json:"enabled"`
	UpdatedAt        int64    `json:"updatedAt"`
}

// CameraSummary is the safe camera metadata attached to an alarm. It
// deliberately has no stream URL or vendor credential fields: live playback
// remains an external camera-platform concern.
type CameraSummary struct {
	CameraID    string `json:"cameraId"`
	Brand       string `json:"brand,omitempty"`
	CameraName  string `json:"cameraName"`
	CameraPoint string `json:"cameraPoint,omitempty"`
	DeviceID    string `json:"deviceId,omitempty"`
	Building    string `json:"building,omitempty"`
	Floor       string `json:"floor,omitempty"`
	Room        string `json:"room,omitempty"`
	Enabled     bool   `json:"enabled"`
}

// VideoCameraRelation is the normalized camera-to-device edge. A device may
// have many cameras, while a camera has at most one device association.
type VideoCameraRelation struct {
	TenantID     string `json:"tenantId"`
	CameraID     string `json:"cameraId"`
	RelationType string `json:"relationType"`
	TargetID     string `json:"targetId"`
}

// VideoStream is a short-lived source or browser playback address. SDK
// providers may return an expiring URL; it is never persisted in a camera
// mapping and should not be exposed to viewers before the preview request.
type VideoStream struct {
	URL        string `json:"url"`
	StreamType string `json:"streamType"`
	Provider   string `json:"provider,omitempty"`
	ExpiresAt  int64  `json:"expiresAt,omitempty"`
}

type VideoPreview struct {
	CameraID    string `json:"cameraId"`
	CameraName  string `json:"cameraName"`
	PlaybackURL string `json:"playbackUrl"`
	StreamType  string `json:"streamType"`
	Provider    string `json:"provider,omitempty"`
	ExpiresAt   int64  `json:"expiresAt,omitempty"`
}

type AIToolCallLog struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenantId"`
	Actor     string         `json:"actor"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input,omitempty"`
	Output    any            `json:"output,omitempty"`
	Success   bool           `json:"success"`
	Error     string         `json:"error,omitempty"`
	CreatedAt int64          `json:"createdAt"`
}

type AuditLog struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenantId"`
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	Details    map[string]any `json:"details,omitempty"`
	CreatedAt  int64          `json:"createdAt"`
}
