package model /* 声明 model 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	TopicRaw         = "iot.raw.message"  /* 更新 TopicRaw 的值。 */
	TopicParseFailed = "iot.parse.failed" /* 更新 TopicParseFailed 的值。 */
	// TopicParsed is the Kafka topic for parsed message types that do not have
	// a more specific downstream topic yet (for example command replies and
	// device logs). Raw messages stay on TopicRaw for the internal parser
	// pipeline; external consumers must use the parsed topics below.
	TopicParsed          = "iot.parsed.message"    /* 更新 TopicParsed 的值。 */
	TopicPropertyReport  = "iot.property.report"   /* 更新 TopicPropertyReport 的值。 */
	TopicEventReport     = "iot.event.report"      /* 更新 TopicEventReport 的值。 */
	TopicDeviceState     = "iot.device.state"      /* 更新 TopicDeviceState 的值。 */
	TopicVideoAlarm      = "iot.video.alarm"       /* 更新 TopicVideoAlarm 的值。 */
	TopicAlarmRaised     = "iot.alarm.raised"      /* 更新 TopicAlarmRaised 的值。 */
	TopicAlarmRecovered  = "iot.alarm.recovered"   /* 更新 TopicAlarmRecovered 的值。 */
	TopicAlarmConfirmed  = "iot.alarm.confirmed"   /* 更新 TopicAlarmConfirmed 的值。 */
	TopicAlarmAIAnalysis = "iot.alarm.ai-analysis" /* 更新 TopicAlarmAIAnalysis 的值。 */
	TopicUIAction        = "iot.ui-action"         /* 更新 TopicUIAction 的值。 */
	TopicReplayRequest   = "iot.replay.request"    /* 更新 TopicReplayRequest 的值。 */
) /* 结束当前表达式或代码块。 */

// ErrRawConflict indicates reuse of a message identity with different content.
var ErrRawConflict = errors.New("message id already exists with different content") /* 声明 ErrRawConflict。 */

type RawMessage struct { /* 定义 RawMessage 类型。 */
	MessageID         string            `json:"messageId"`                   /* 执行当前语句并推进处理流程。 */
	Source            string            `json:"source"`                      /* 执行当前语句并推进处理流程。 */
	TenantID          string            `json:"tenantId"`                    /* 执行当前语句并推进处理流程。 */
	ProductID         string            `json:"productId"`                   /* 执行当前语句并推进处理流程。 */
	DeviceID          string            `json:"deviceId"`                    /* 执行当前语句并推进处理流程。 */
	DeviceName        string            `json:"deviceName,omitempty"`        /* 执行当前语句并推进处理流程。 */
	GatewayID         string            `json:"gatewayId,omitempty"`         /* 执行当前语句并推进处理流程。 */
	Protocol          string            `json:"protocol"`                    /* 执行当前语句并推进处理流程。 */
	Transport         string            `json:"transport"`                   /* 执行当前语句并推进处理流程。 */
	ReceivedAt        int64             `json:"receivedAt"`                  /* 执行当前语句并推进处理流程。 */
	PayloadFormat     string            `json:"payloadFormat"`               /* 执行当前语句并推进处理流程。 */
	Payload           json.RawMessage   `json:"payload"`                     /* 执行当前语句并推进处理流程。 */
	ClientID          string            `json:"clientId,omitempty"`          /* 执行当前语句并推进处理流程。 */
	RemoteAddress     string            `json:"remoteAddress,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Headers           map[string]string `json:"headers,omitempty"`           /* 执行当前语句并推进处理流程。 */
	ParserVersion     string            `json:"parserVersion,omitempty"`     /* 执行当前语句并推进处理流程。 */
	ProtocolID        string            `json:"protocolId,omitempty"`        /* 执行当前语句并推进处理流程。 */
	ProtocolVersion   string            `json:"protocolVersion,omitempty"`   /* 执行当前语句并推进处理流程。 */
	PointTableVersion string            `json:"pointTableVersion,omitempty"` /* 执行当前语句并推进处理流程。 */
	CollectorID       string            `json:"collectorId,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Metadata          map[string]any    `json:"metadata,omitempty"`          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (m *RawMessage) Normalize(now time.Time) { /* 定义 Normalize 函数。 */
	if m.MessageID == "" { /* 判断条件并选择处理分支。 */
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%s", m.TenantID, m.DeviceID, now.UnixNano(), m.Payload))) /* 更新 h 的值。 */
		m.MessageID = "raw_" + hex.EncodeToString(h[:12])                                                         /* 更新 m.MessageID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if m.Source == "" { /* 判断条件并选择处理分支。 */
		m.Source = "external-ingest" /* 更新 m.Source 的值。 */
	} /* 结束当前表达式或代码块。 */
	if m.ReceivedAt == 0 { /* 判断条件并选择处理分支。 */
		m.ReceivedAt = now.UnixMilli() /* 更新 m.ReceivedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if m.PayloadFormat == "" { /* 判断条件并选择处理分支。 */
		m.PayloadFormat = "json" /* 更新 m.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (m RawMessage) Validate() error { /* 定义 Validate 函数。 */
	missing := make([]string, 0, 4)                                                                                                                  /* 更新 missing 的值。 */
	for name, value := range map[string]string{"messageId": m.MessageID, "tenantId": m.TenantID, "productId": m.ProductID, "deviceId": m.DeviceID} { /* 循环处理当前数据。 */
		if strings.TrimSpace(value) == "" { /* 判断条件并选择处理分支。 */
			missing = append(missing, name) /* 更新 missing 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(missing) > 0 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", ")) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(m.Payload) == 0 { /* 判断条件并选择处理分支。 */
		return errors.New("payload is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (m RawMessage) PayloadHash() string { /* 定义 PayloadHash 函数。 */
	h := sha256.Sum256(m.Payload)   /* 更新 h 的值。 */
	return hex.EncodeToString(h[:]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type RawArchiveIndex struct { /* 定义 RawArchiveIndex 类型。 */
	MessageID        string `json:"messageId"`                  /* 执行当前语句并推进处理流程。 */
	TenantID         string `json:"tenantId"`                   /* 执行当前语句并推进处理流程。 */
	ProductID        string `json:"productId"`                  /* 执行当前语句并推进处理流程。 */
	DeviceID         string `json:"deviceId"`                   /* 执行当前语句并推进处理流程。 */
	Protocol         string `json:"protocol"`                   /* 执行当前语句并推进处理流程。 */
	PayloadFormat    string `json:"payloadFormat"`              /* 执行当前语句并推进处理流程。 */
	ObjectBucket     string `json:"objectBucket"`               /* 执行当前语句并推进处理流程。 */
	ObjectKey        string `json:"objectKey"`                  /* 执行当前语句并推进处理流程。 */
	ObjectOffset     int64  `json:"objectOffset"`               /* 执行当前语句并推进处理流程。 */
	PayloadHash      string `json:"payloadHash"`                /* 执行当前语句并推进处理流程。 */
	PayloadSize      int    `json:"payloadSize"`                /* 执行当前语句并推进处理流程。 */
	ReceivedAt       int64  `json:"receivedAt"`                 /* 执行当前语句并推进处理流程。 */
	ArchivedAt       int64  `json:"archivedAt"`                 /* 执行当前语句并推进处理流程。 */
	PublishedAt      int64  `json:"publishedAt,omitempty"`      /* 执行当前语句并推进处理流程。 */
	PublishAttempts  int    `json:"publishAttempts,omitempty"`  /* 执行当前语句并推进处理流程。 */
	LastPublishError string `json:"lastPublishError,omitempty"` /* 执行当前语句并推进处理流程。 */
	ParseAttemptedAt int64  `json:"parseAttemptedAt,omitempty"` /* 执行当前语句并推进处理流程。 */
	ParseError       string `json:"parseError,omitempty"`       /* 执行当前语句并推进处理流程。 */
	// Parsed and parsedMessageType are response metadata populated by the API;
	// they are not persisted in the archive index table.
	Parsed            bool   `json:"parsed,omitempty"`            /* 执行当前语句并推进处理流程。 */
	ParsedMessageType string `json:"parsedMessageType,omitempty"` /* 执行当前语句并推进处理流程。 */
	Parser            string `json:"parser,omitempty"`            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type MessageType string /* 定义 MessageType 类型。 */

const ( /* 执行当前语句并推进处理流程。 */
	PropertyReport MessageType = "PROPERTY_REPORT" /* 执行当前语句并推进处理流程。 */
	EventReport    MessageType = "EVENT_REPORT"    /* 执行当前语句并推进处理流程。 */
	StateChange    MessageType = "STATE_CHANGE"    /* 执行当前语句并推进处理流程。 */
	AlarmReport    MessageType = "ALARM_REPORT"    /* 执行当前语句并推进处理流程。 */
	CommandReply   MessageType = "COMMAND_REPLY"   /* 执行当前语句并推进处理流程。 */
	LogReport      MessageType = "LOG_REPORT"      /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type StandardMessage struct { /* 定义 StandardMessage 类型。 */
	MessageID     string            `json:"messageId"`            /* 执行当前语句并推进处理流程。 */
	RawMessageID  string            `json:"rawMessageId"`         /* 执行当前语句并推进处理流程。 */
	TenantID      string            `json:"tenantId"`             /* 执行当前语句并推进处理流程。 */
	ProductID     string            `json:"productId"`            /* 执行当前语句并推进处理流程。 */
	DeviceID      string            `json:"deviceId"`             /* 执行当前语句并推进处理流程。 */
	MessageType   MessageType       `json:"messageType"`          /* 执行当前语句并推进处理流程。 */
	Timestamp     int64             `json:"timestamp"`            /* 执行当前语句并推进处理流程。 */
	Properties    map[string]any    `json:"properties,omitempty"` /* 执行当前语句并推进处理流程。 */
	Event         map[string]any    `json:"event,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Tags          map[string]string `json:"tags,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Raw           map[string]any    `json:"raw,omitempty"`        /* 执行当前语句并推进处理流程。 */
	Parser        string            `json:"parser"`               /* 执行当前语句并推进处理流程。 */
	ParserVersion string            `json:"parserVersion"`        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// MQTTTopic returns the tenant-scoped external topic for every successfully
// parsed standard message. Keep tenant/product/device before messageType so a
// client can subscribe to /iot/parsed/{tenant}/# without crossing tenants.
func (m StandardMessage) MQTTTopic() string { /* 定义 MQTTTopic 函数。 */
	clean := func(value string) string { /* 更新 clean 的值。 */
		value = strings.Trim(strings.ReplaceAll(value, "/", "_"), " ") /* 更新 value 的值。 */
		if value == "" {                                               /* 判断条件并选择处理分支。 */
			return "unknown" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return value /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fmt.Sprintf("/iot/parsed/%s/%s/%s/%s", clean(m.TenantID), clean(m.ProductID), clean(m.DeviceID), clean(string(m.MessageType))) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type DeviceState struct { /* 定义 DeviceState 类型。 */
	TenantID            string `json:"tenantId"`                    /* 执行当前语句并推进处理流程。 */
	ProductID           string `json:"productId"`                   /* 执行当前语句并推进处理流程。 */
	DeviceID            string `json:"deviceId"`                    /* 执行当前语句并推进处理流程。 */
	ConnectionStatus    string `json:"connectionStatus"`            /* 执行当前语句并推进处理流程。 */
	DataStatus          string `json:"dataStatus"`                  /* 执行当前语句并推进处理流程。 */
	BusinessStatus      string `json:"businessStatus"`              /* 执行当前语句并推进处理流程。 */
	LastConnectAt       int64  `json:"lastConnectAt,omitempty"`     /* 执行当前语句并推进处理流程。 */
	LastDisconnectAt    int64  `json:"lastDisconnectAt,omitempty"`  /* 执行当前语句并推进处理流程。 */
	LastSeenAt          int64  `json:"lastSeenAt,omitempty"`        /* 执行当前语句并推进处理流程。 */
	LastMessageID       string `json:"lastMessageId,omitempty"`     /* 执行当前语句并推进处理流程。 */
	ReportIntervalSec   int64  `json:"reportIntervalSec"`           /* 执行当前语句并推进处理流程。 */
	OfflineToleranceSec int64  `json:"offlineToleranceSec"`         /* 执行当前语句并推进处理流程。 */
	OfflineAt           int64  `json:"offlineAt,omitempty"`         /* 执行当前语句并推进处理流程。 */
	OfflineDetectedAt   int64  `json:"offlineDetectedAt,omitempty"` /* 执行当前语句并推进处理流程。 */
	StatusSource        string `json:"statusSource"`                /* 执行当前语句并推进处理流程。 */
	Reason              string `json:"reason,omitempty"`            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// Product describes a managed device product and the protocol package used to
// turn its raw payloads into standard platform messages.
type Product struct { /* 定义 Product 类型。 */
	ThingModel        *ThingModel    `json:"thingModel,omitempty"`  /* 执行当前语句并推进处理流程。 */
	ID                string         `json:"id"`                    /* 执行当前语句并推进处理流程。 */
	TenantID          string         `json:"tenantId"`              /* 执行当前语句并推进处理流程。 */
	Name              string         `json:"name"`                  /* 执行当前语句并推进处理流程。 */
	Category          string         `json:"category"`              /* 执行当前语句并推进处理流程。 */
	ProtocolPackageID string         `json:"protocolPackageId"`     /* 执行当前语句并推进处理流程。 */
	Transport         string         `json:"transport"`             /* 执行当前语句并推进处理流程。 */
	PayloadFormat     string         `json:"payloadFormat"`         /* 执行当前语句并推进处理流程。 */
	Status            string         `json:"status"`                /* 执行当前语句并推进处理流程。 */
	Description       string         `json:"description,omitempty"` /* 执行当前语句并推进处理流程。 */
	Metadata          map[string]any `json:"metadata,omitempty"`    /* 执行当前语句并推进处理流程。 */
	CreatedAt         int64          `json:"createdAt"`             /* 执行当前语句并推进处理流程。 */
	UpdatedAt         int64          `json:"updatedAt"`             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ProtocolPackage is a declarative protocol-package release. ParserType points
// at a reviewed parser registered in the Go process. The sandbox JavaScript
// parser stores a pure transformation source in Config; go_protocol_parser
// stores a checksum-verified, compiled worker artifact in Config and invokes it
// through the external JSON-lines contract.
type ProtocolPackage struct { /* 定义 ProtocolPackage 类型。 */
	ID            string         `json:"id"`                    /* 执行当前语句并推进处理流程。 */
	TenantID      string         `json:"tenantId"`              /* 执行当前语句并推进处理流程。 */
	Name          string         `json:"name"`                  /* 执行当前语句并推进处理流程。 */
	Version       string         `json:"version"`               /* 执行当前语句并推进处理流程。 */
	Protocol      string         `json:"protocol"`              /* 执行当前语句并推进处理流程。 */
	Transport     string         `json:"transport"`             /* 执行当前语句并推进处理流程。 */
	PayloadFormat string         `json:"payloadFormat"`         /* 执行当前语句并推进处理流程。 */
	ParserType    string         `json:"parserType"`            /* 执行当前语句并推进处理流程。 */
	Status        string         `json:"status"`                /* 执行当前语句并推进处理流程。 */
	Description   string         `json:"description,omitempty"` /* 执行当前语句并推进处理流程。 */
	Config        map[string]any `json:"config,omitempty"`      /* 执行当前语句并推进处理流程。 */
	CreatedAt     int64          `json:"createdAt"`             /* 执行当前语句并推进处理流程。 */
	UpdatedAt     int64          `json:"updatedAt"`             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ProtocolDefinition is the stable identity of a protocol family. Executable
// and mapping content lives in immutable ProtocolRelease records.
type ProtocolDefinition struct { /* 定义 ProtocolDefinition 类型。 */
	ID          string `json:"id"`                    /* 执行当前语句并推进处理流程。 */
	TenantID    string `json:"tenantId"`              /* 执行当前语句并推进处理流程。 */
	Name        string `json:"name"`                  /* 执行当前语句并推进处理流程。 */
	Vendor      string `json:"vendor,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Description string `json:"description,omitempty"` /* 执行当前语句并推进处理流程。 */
	CreatedAt   int64  `json:"createdAt"`             /* 执行当前语句并推进处理流程。 */
	UpdatedAt   int64  `json:"updatedAt"`             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ModbusPoint is a normalized point-table row. Address is always the zero
// based PDU address; AddressNotation records how the uploaded value was
// interpreted so that conversions remain auditable.
type ModbusPoint struct { /* 定义 ModbusPoint 类型。 */
	Identifier      string         `json:"identifier"`                /* 执行当前语句并推进处理流程。 */
	Name            string         `json:"name"`                      /* 执行当前语句并推进处理流程。 */
	FunctionCode    int            `json:"functionCode"`              /* 执行当前语句并推进处理流程。 */
	Address         int            `json:"address"`                   /* 执行当前语句并推进处理流程。 */
	AddressNotation string         `json:"addressNotation"`           /* 执行当前语句并推进处理流程。 */
	DataType        string         `json:"dataType"`                  /* 执行当前语句并推进处理流程。 */
	RegisterCount   int            `json:"registerCount,omitempty"`   /* 执行当前语句并推进处理流程。 */
	ByteOrder       string         `json:"byteOrder,omitempty"`       /* 执行当前语句并推进处理流程。 */
	WordOrder       string         `json:"wordOrder,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Bit             *int           `json:"bit,omitempty"`             /* 执行当前语句并推进处理流程。 */
	Scale           float64        `json:"scale,omitempty"`           /* 执行当前语句并推进处理流程。 */
	Offset          float64        `json:"offset,omitempty"`          /* 执行当前语句并推进处理流程。 */
	Unit            string         `json:"unit,omitempty"`            /* 执行当前语句并推进处理流程。 */
	Access          string         `json:"access,omitempty"`          /* 执行当前语句并推进处理流程。 */
	PollIntervalSec int            `json:"pollIntervalSec,omitempty"` /* 执行当前语句并推进处理流程。 */
	Deadband        float64        `json:"deadband,omitempty"`        /* 执行当前语句并推进处理流程。 */
	AlarmMapping    map[string]any `json:"alarmMapping,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Description     string         `json:"description,omitempty"`     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ModbusReadBlock struct { /* 定义 ModbusReadBlock 类型。 */
	ID              string `json:"id"`              /* 执行当前语句并推进处理流程。 */
	FunctionCode    int    `json:"functionCode"`    /* 执行当前语句并推进处理流程。 */
	StartAddress    int    `json:"startAddress"`    /* 执行当前语句并推进处理流程。 */
	Quantity        int    `json:"quantity"`        /* 执行当前语句并推进处理流程。 */
	PollIntervalSec int    `json:"pollIntervalSec"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type PointTableRelease struct { /* 定义 PointTableRelease 类型。 */
	TenantID     string        `json:"tenantId"`               /* 执行当前语句并推进处理流程。 */
	ProtocolID   string        `json:"protocolId"`             /* 执行当前语句并推进处理流程。 */
	Version      string        `json:"version"`                /* 执行当前语句并推进处理流程。 */
	SourceName   string        `json:"sourceName,omitempty"`   /* 执行当前语句并推进处理流程。 */
	SourceSHA256 string        `json:"sourceSha256,omitempty"` /* 执行当前语句并推进处理流程。 */
	Points       []ModbusPoint `json:"points"`                 /* 执行当前语句并推进处理流程。 */
	CreatedAt    int64         `json:"createdAt"`              /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ProtocolRelease struct { /* 定义 ProtocolRelease 类型。 */
	TenantID          string         `json:"tenantId"`                    /* 执行当前语句并推进处理流程。 */
	ProtocolID        string         `json:"protocolId"`                  /* 执行当前语句并推进处理流程。 */
	Version           string         `json:"version"`                     /* 执行当前语句并推进处理流程。 */
	Transport         string         `json:"transport"`                   /* 执行当前语句并推进处理流程。 */
	PayloadFormat     string         `json:"payloadFormat"`               /* 执行当前语句并推进处理流程。 */
	ParserType        string         `json:"parserType"`                  /* 执行当前语句并推进处理流程。 */
	Status            string         `json:"status"`                      /* 执行当前语句并推进处理流程。 */
	PointTableVersion string         `json:"pointTableVersion,omitempty"` /* 执行当前语句并推进处理流程。 */
	Capabilities      []string       `json:"capabilities,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Config            map[string]any `json:"config,omitempty"`            /* 执行当前语句并推进处理流程。 */
	Artifact          map[string]any `json:"artifact,omitempty"`          /* 执行当前语句并推进处理流程。 */
	CreatedAt         int64          `json:"createdAt"`                   /* 执行当前语句并推进处理流程。 */
	PublishedAt       int64          `json:"publishedAt,omitempty"`       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ProductProtocolBinding struct { /* 定义 ProductProtocolBinding 类型。 */
	TenantID           string `json:"tenantId"`                     /* 执行当前语句并推进处理流程。 */
	ProductID          string `json:"productId"`                    /* 执行当前语句并推进处理流程。 */
	ProtocolID         string `json:"protocolId"`                   /* 执行当前语句并推进处理流程。 */
	Version            string `json:"version"`                      /* 执行当前语句并推进处理流程。 */
	PreviousVersion    string `json:"previousVersion,omitempty"`    /* 执行当前语句并推进处理流程。 */
	PreviousProtocolID string `json:"previousProtocolId,omitempty"` /* 执行当前语句并推进处理流程。 */
	UpdatedAt          int64  `json:"updatedAt"`                    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type DeviceAccessProfile struct { /* 定义 DeviceAccessProfile 类型。 */
	ConnectionMode string                `json:"connectionMode,omitempty"` /* 执行当前语句并推进处理流程。 */ // listen (default) or dial
	WireFormat     string                `json:"wireFormat,omitempty"`     /* 执行当前语句并推进处理流程。 */ // modbus_tcp (default) or rtu_over_tcp
	Queries        []ProtocolQuery       `json:"queries,omitempty"`        /* 执行当前语句并推进处理流程。 */
	ChildProducts  []ChildProductBinding `json:"childProducts,omitempty"`  /* 执行当前语句并推进处理流程。 */

	CredentialRef string `json:"credentialRef,omitempty"` /* 执行当前语句并推进处理流程。 */
	EndpointPath  string `json:"endpointPath,omitempty"`  /* 执行当前语句并推进处理流程。 */
	SerialPort    string `json:"serialPort,omitempty"`    /* 执行当前语句并推进处理流程。 */
	BaudRate      int    `json:"baudRate,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Parity        string `json:"parity,omitempty"`        /* 执行当前语句并推进处理流程。 */
	StopBits      int    `json:"stopBits,omitempty"`      /* 执行当前语句并推进处理流程。 */
	// EdgeNodeID is retained only to reject legacy remote assignments.
	EdgeNodeID        string `json:"edgeNodeId,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Mode              string `json:"mode,omitempty"`          /* 执行当前语句并推进处理流程。 */
	Network           string `json:"network,omitempty"`       /* 执行当前语句并推进处理流程。 */
	AutoRegister      bool   `json:"autoRegister,omitempty"`  /* 执行当前语句并推进处理流程。 */
	ID                string `json:"id"`                      /* 执行当前语句并推进处理流程。 */
	TenantID          string `json:"tenantId"`                /* 执行当前语句并推进处理流程。 */
	DeviceID          string `json:"deviceId"`                /* 执行当前语句并推进处理流程。 */
	ProductID         string `json:"productId"`               /* 执行当前语句并推进处理流程。 */
	ProtocolID        string `json:"protocolId"`              /* 执行当前语句并推进处理流程。 */
	ProtocolVersion   string `json:"protocolVersion"`         /* 执行当前语句并推进处理流程。 */
	PointTableVersion string `json:"pointTableVersion"`       /* 执行当前语句并推进处理流程。 */
	CollectorID       string `json:"collectorId,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Host              string `json:"host"`                    /* 执行当前语句并推进处理流程。 */
	PublicHost        string `json:"publicHost,omitempty"`    // Address configured on field devices for listener profiles.
	Port              int    `json:"port"`                    /* 执行当前语句并推进处理流程。 */
	UnitID            int    `json:"unitId"`                  /* 执行当前语句并推进处理流程。 */
	TimeoutMs         int    `json:"timeoutMs"`               /* 执行当前语句并推进处理流程。 */
	Retries           int    `json:"retries"`                 /* 执行当前语句并推进处理流程。 */
	Enabled           bool   `json:"enabled"`                 /* 执行当前语句并推进处理流程。 */
	RuntimeStatus     string `json:"runtimeStatus"`           /* 执行当前语句并推进处理流程。 */
	LastSuccessAt     int64  `json:"lastSuccessAt,omitempty"` /* 执行当前语句并推进处理流程。 */
	LastErrorAt       int64  `json:"lastErrorAt,omitempty"`   /* 执行当前语句并推进处理流程。 */
	LastError         string `json:"lastError,omitempty"`     /* 执行当前语句并推进处理流程。 */
	CreatedAt         int64  `json:"createdAt"`               /* 执行当前语句并推进处理流程。 */
	UpdatedAt         int64  `json:"updatedAt"`               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// Configuration clears only runtime observations; UpdatedAt remains the configuration revision.
func (p DeviceAccessProfile) Configuration() string { /* 定义 Configuration 函数。 */
	p.RuntimeStatus, p.LastError = "", "" /* 更新 p.LastError 的值。 */
	p.LastSuccessAt, p.LastErrorAt = 0, 0 /* 更新 p.LastErrorAt 的值。 */
	b, err := json.Marshal(p)             /* 更新 err 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		return "invalid" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return string(b) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// ProtocolAssistantField is the editable address/mapping contract between the
// protocol assistant and the form-based protocol designer. Expression remains
// optional for backwards-compatible drafts, but the assistant no longer
// generates or executes JavaScript.
type ProtocolAssistantField struct { /* 定义 ProtocolAssistantField 类型。 */
	Name          string `json:"name"`                  /* 执行当前语句并推进处理流程。 */
	Label         string `json:"label,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Type          string `json:"type,omitempty"`        /* 执行当前语句并推进处理流程。 */
	Expression    string `json:"expression,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Address       string `json:"address,omitempty"`     /* 执行当前语句并推进处理流程。 */
	CoilAddress   int    `json:"coilAddress"`           /* 执行当前语句并推进处理流程。 */
	ModbusAddress int    `json:"modbusAddress"`         /* 执行当前语句并推进处理流程。 */
	DataType      string `json:"dataType,omitempty"`    /* 执行当前语句并推进处理流程。 */
	NormalValue   string `json:"normalValue,omitempty"` /* 执行当前语句并推进处理流程。 */
	ReportValue   string `json:"reportValue,omitempty"` /* 执行当前语句并推进处理流程。 */
	Description   string `json:"description,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ProtocolAssistantDraft struct { /* 定义 ProtocolAssistantDraft 类型。 */
	Name           string                   `json:"name"`                     /* 执行当前语句并推进处理流程。 */
	Description    string                   `json:"description,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Protocol       string                   `json:"protocol"`                 /* 执行当前语句并推进处理流程。 */
	Transport      string                   `json:"transport"`                /* 执行当前语句并推进处理流程。 */
	PayloadFormat  string                   `json:"payloadFormat"`            /* 执行当前语句并推进处理流程。 */
	ParserType     string                   `json:"parserType"`               /* 执行当前语句并推进处理流程。 */
	MessageType    MessageType              `json:"messageType"`              /* 执行当前语句并推进处理流程。 */
	Setup          string                   `json:"setup,omitempty"`          /* 执行当前语句并推进处理流程。 */
	Source         string                   `json:"source,omitempty"`         /* 执行当前语句并推进处理流程。 */
	Config         map[string]any           `json:"config,omitempty"`         /* 执行当前语句并推进处理流程。 */
	Fields         []ProtocolAssistantField `json:"fields"`                   /* 执行当前语句并推进处理流程。 */
	TagExpressions map[string]string        `json:"tagExpressions,omitempty"` /* 执行当前语句并推进处理流程。 */
	SamplePayload  any                      `json:"samplePayload,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Warnings       []string                 `json:"warnings,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Preview        *StandardMessage         `json:"preview,omitempty"`        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type DeviceHealthItem struct { /* 定义 DeviceHealthItem 类型。 */
	DeviceID         string   `json:"deviceId"`             /* 执行当前语句并推进处理流程。 */
	DeviceName       string   `json:"deviceName,omitempty"` /* 执行当前语句并推进处理流程。 */
	ProductID        string   `json:"productId"`            /* 执行当前语句并推进处理流程。 */
	BusinessStatus   string   `json:"businessStatus"`       /* 执行当前语句并推进处理流程。 */
	DataStatus       string   `json:"dataStatus"`           /* 执行当前语句并推进处理流程。 */
	LastSeenAt       int64    `json:"lastSeenAt,omitempty"` /* 执行当前语句并推进处理流程。 */
	ActiveAlarmCount int      `json:"activeAlarmCount"`     /* 执行当前语句并推进处理流程。 */
	Severity         string   `json:"severity"`             /* 执行当前语句并推进处理流程。 */
	Findings         []string `json:"findings"`             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type DeviceHealthReport struct { /* 定义 DeviceHealthReport 类型。 */
	TenantID    string             `json:"tenantId,omitempty"` /* 执行当前语句并推进处理流程。 */
	GeneratedAt int64              `json:"generatedAt"`        /* 执行当前语句并推进处理流程。 */
	Summary     string             `json:"summary"`            /* 执行当前语句并推进处理流程。 */
	AIAdvice    string             `json:"aiAdvice,omitempty"` /* 执行当前语句并推进处理流程。 */
	Counts      map[string]int     `json:"counts"`             /* 执行当前语句并推进处理流程。 */
	Items       []DeviceHealthItem `json:"items"`              /* 执行当前语句并推进处理流程。 */
	Warnings    []string           `json:"warnings,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// ManagedDevice is the inventory/control-plane record. Runtime connectivity is
// kept separately in DeviceState and joined by the API.
type ManagedDevice struct { /* 定义 ManagedDevice 类型。 */
	ID                 string            `json:"id"`                           /* 执行当前语句并推进处理流程。 */
	TenantID           string            `json:"tenantId"`                     /* 执行当前语句并推进处理流程。 */
	ProductID          string            `json:"productId"`                    /* 执行当前语句并推进处理流程。 */
	Name               string            `json:"name"`                         /* 执行当前语句并推进处理流程。 */
	Status             string            `json:"status"`                       /* 执行当前语句并推进处理流程。 */
	DeviceRole         string            `json:"deviceRole"`                   /* 执行当前语句并推进处理流程。 */
	GatewayID          string            `json:"gatewayId,omitempty"`          /* 执行当前语句并推进处理流程。 */
	RegistrationSource string            `json:"registrationSource,omitempty"` /* 执行当前语句并推进处理流程。 */
	AutoRegistered     bool              `json:"autoRegistered,omitempty"`     /* 执行当前语句并推进处理流程。 */
	AccessKey          string            `json:"accessKey,omitempty"`          /* 执行当前语句并推进处理流程。 */
	SecretHash         string            `json:"-"`                            /* 执行当前语句并推进处理流程。 */
	SecretHint         string            `json:"secretHint,omitempty"`         /* 执行当前语句并推进处理流程。 */
	Description        string            `json:"description,omitempty"`        /* 执行当前语句并推进处理流程。 */
	Tags               map[string]string `json:"tags,omitempty"`               /* 执行当前语句并推进处理流程。 */
	CreatedAt          int64             `json:"createdAt"`                    /* 执行当前语句并推进处理流程。 */
	UpdatedAt          int64             `json:"updatedAt"`                    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type DeviceCredential struct { /* 定义 DeviceCredential 类型。 */
	AccessKey string `json:"accessKey"` /* 执行当前语句并推进处理流程。 */
	Secret    string `json:"secret"`    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type RuleCondition struct { /* 定义 RuleCondition 类型。 */
	Field    string `json:"field"`    /* 执行当前语句并推进处理流程。 */
	Operator string `json:"operator"` /* 执行当前语句并推进处理流程。 */
	Value    any    `json:"value"`    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type RuleAction struct { /* 定义 RuleAction 类型。 */
	Type     string `json:"type"`               /* 执行当前语句并推进处理流程。 */
	CameraID string `json:"cameraId,omitempty"` /* 执行当前语句并推进处理流程。 */
	Page     string `json:"page,omitempty"`     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AlarmRule struct { /* 定义 AlarmRule 类型。 */
	ID              string          `json:"id"`                    /* 执行当前语句并推进处理流程。 */
	TenantID        string          `json:"tenantId"`              /* 执行当前语句并推进处理流程。 */
	ProductID       string          `json:"productId,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Name            string          `json:"name"`                  /* 执行当前语句并推进处理流程。 */
	Description     string          `json:"description,omitempty"` /* 执行当前语句并推进处理流程。 */
	AlarmType       string          `json:"alarmType"`             /* 执行当前语句并推进处理流程。 */
	Level           string          `json:"level"`                 /* 执行当前语句并推进处理流程。 */
	Conditions      []RuleCondition `json:"conditions"`            /* 执行当前语句并推进处理流程。 */
	Match           string          `json:"match"`                 /* 执行当前语句并推进处理流程。 */
	DurationSeconds int64           `json:"durationSeconds"`       /* 执行当前语句并推进处理流程。 */
	Recovery        []RuleCondition `json:"recovery,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Actions         []RuleAction    `json:"actions,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Expression      string          `json:"expression,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Enabled         bool            `json:"enabled"`               /* 执行当前语句并推进处理流程。 */
	Version         int             `json:"version"`               /* 执行当前语句并推进处理流程。 */
	CreatedAt       int64           `json:"createdAt"`             /* 执行当前语句并推进处理流程。 */
	UpdatedAt       int64           `json:"updatedAt"`             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// RulePending records the first observation of a duration-based rule match.
// It is persisted so a process restart does not silently reset the timer.
type RulePending struct { /* 定义 RulePending 类型。 */
	TenantID  string `json:"tenantId"`  /* 执行当前语句并推进处理流程。 */
	RuleID    string `json:"ruleId"`    /* 执行当前语句并推进处理流程。 */
	DeviceID  string `json:"deviceId"`  /* 执行当前语句并推进处理流程。 */
	Since     int64  `json:"since"`     /* 执行当前语句并推进处理流程。 */
	UpdatedAt int64  `json:"updatedAt"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type UIActionEvent struct { /* 定义 UIActionEvent 类型。 */
	ID          string     `json:"id"`          /* 执行当前语句并推进处理流程。 */
	TenantID    string     `json:"tenantId"`    /* 执行当前语句并推进处理流程。 */
	RuleID      string     `json:"ruleId"`      /* 执行当前语句并推进处理流程。 */
	AlarmID     string     `json:"alarmId"`     /* 执行当前语句并推进处理流程。 */
	DeviceID    string     `json:"deviceId"`    /* 执行当前语句并推进处理流程。 */
	Action      RuleAction `json:"action"`      /* 执行当前语句并推进处理流程。 */
	TriggeredAt int64      `json:"triggeredAt"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Alarm struct { /* 定义 Alarm 类型。 */
	ComponentID       string          `json:"componentId,omitempty"`       /* 执行当前语句并推进处理流程。 */
	ComponentName     string          `json:"componentName,omitempty"`     /* 执行当前语句并推进处理流程。 */
	ComponentLocation string          `json:"componentLocation,omitempty"` /* 执行当前语句并推进处理流程。 */
	ID                string          `json:"alarmId"`                     /* 执行当前语句并推进处理流程。 */
	TenantID          string          `json:"tenantId"`                    /* 执行当前语句并推进处理流程。 */
	RuleID            string          `json:"ruleId"`                      /* 执行当前语句并推进处理流程。 */
	TriggerID         string          `json:"triggerId,omitempty"`         /* 执行当前语句并推进处理流程。 */
	DeviceID          string          `json:"deviceId"`                    /* 执行当前语句并推进处理流程。 */
	DeviceName        string          `json:"deviceName,omitempty"`        /* 执行当前语句并推进处理流程。 */
	AlarmType         string          `json:"alarmType"`                   /* 执行当前语句并推进处理流程。 */
	AlarmLevel        string          `json:"alarmLevel"`                  /* 执行当前语句并推进处理流程。 */
	Status            string          `json:"status"`                      /* 执行当前语句并推进处理流程。 */
	Source            string          `json:"source"`                      /* 执行当前语句并推进处理流程。 */
	CityCode          string          `json:"cityCode"`                    /* 执行当前语句并推进处理流程。 */
	DistrictCode      string          `json:"districtCode"`                /* 执行当前语句并推进处理流程。 */
	BuildingID        string          `json:"buildingId"`                  /* 执行当前语句并推进处理流程。 */
	DeviceType        string          `json:"deviceType"`                  /* 执行当前语句并推进处理流程。 */
	AreaID            string          `json:"areaId,omitempty"`            /* 执行当前语句并推进处理流程。 */
	FirstTriggeredAt  int64           `json:"firstTriggeredAt"`            /* 执行当前语句并推进处理流程。 */
	LastTriggeredAt   int64           `json:"lastTriggeredAt"`             /* 执行当前语句并推进处理流程。 */
	TriggerCount      int             `json:"triggerCount"`                /* 执行当前语句并推进处理流程。 */
	RecoveredAt       int64           `json:"recoveredAt,omitempty"`       /* 执行当前语句并推进处理流程。 */
	AckedAt           int64           `json:"ackedAt,omitempty"`           /* 执行当前语句并推进处理流程。 */
	ClosedAt          int64           `json:"closedAt,omitempty"`          /* 执行当前语句并推进处理流程。 */
	Confidence        float64         `json:"confidence,omitempty"`        /* 执行当前语句并推进处理流程。 */
	MultiSource       bool            `json:"multiSource"`                 /* 执行当前语句并推进处理流程。 */
	Cameras           []CameraSummary `json:"cameras,omitempty"`           /* 执行当前语句并推进处理流程。 */
	Details           map[string]any  `json:"details,omitempty"`           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (a Alarm) MQTTTopic(eventType string) string { /* 定义 MQTTTopic 函数。 */
	clean := func(v string) string { /* 更新 clean 的值。 */
		v = strings.Trim(strings.ReplaceAll(v, "/", "_"), " ") /* 更新 v 的值。 */
		if v == "" {                                           /* 判断条件并选择处理分支。 */
			return "unknown" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return v /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fmt.Sprintf("/iot/alarm/%s/%s/%s/%s/%s/%s", clean(eventType), clean(a.CityCode), clean(a.DistrictCode), clean(a.BuildingID), clean(a.DeviceType), clean(a.DeviceID)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type VideoAlarmEvent struct { /* 定义 VideoAlarmEvent 类型。 */
	EventID      string         `json:"eventId"`                /* 执行当前语句并推进处理流程。 */
	Source       string         `json:"source"`                 /* 执行当前语句并推进处理流程。 */
	TenantID     string         `json:"tenantId"`               /* 执行当前语句并推进处理流程。 */
	ProjectID    string         `json:"projectId"`              /* 执行当前语句并推进处理流程。 */
	CameraID     string         `json:"cameraId"`               /* 执行当前语句并推进处理流程。 */
	CameraName   string         `json:"cameraName"`             /* 执行当前语句并推进处理流程。 */
	AreaID       string         `json:"areaId"`                 /* 执行当前语句并推进处理流程。 */
	AlarmType    string         `json:"alarmType"`              /* 执行当前语句并推进处理流程。 */
	AlarmName    string         `json:"alarmName"`              /* 执行当前语句并推进处理流程。 */
	AlarmLevel   string         `json:"alarmLevel"`             /* 执行当前语句并推进处理流程。 */
	Confidence   float64        `json:"confidence"`             /* 执行当前语句并推进处理流程。 */
	EventTime    int64          `json:"eventTime"`              /* 执行当前语句并推进处理流程。 */
	ReceivedAt   int64          `json:"receivedAt"`             /* 执行当前语句并推进处理流程。 */
	SnapshotURL  string         `json:"snapshotUrl,omitempty"`  /* 执行当前语句并推进处理流程。 */
	VideoClipURL string         `json:"videoClipUrl,omitempty"` /* 执行当前语句并推进处理流程。 */
	CityCode     string         `json:"cityCode"`               /* 执行当前语句并推进处理流程。 */
	DistrictCode string         `json:"districtCode"`           /* 执行当前语句并推进处理流程。 */
	BuildingID   string         `json:"buildingId"`             /* 执行当前语句并推进处理流程。 */
	Location     map[string]any `json:"location,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Raw          map[string]any `json:"raw,omitempty"`          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIAnalysis struct { /* 定义 AIAnalysis 类型。 */
	TenantID        string   `json:"tenantId,omitempty"` /* 执行当前语句并推进处理流程。 */
	AlarmID         string   `json:"alarmId"`            /* 执行当前语句并推进处理流程。 */
	Summary         string   `json:"summary"`            /* 执行当前语句并推进处理流程。 */
	PossibleReasons []string `json:"possibleReasons"`    /* 执行当前语句并推进处理流程。 */
	Suggestions     []string `json:"suggestions"`        /* 执行当前语句并推进处理流程。 */
	RiskLevel       string   `json:"riskLevel"`          /* 执行当前语句并推进处理流程。 */
	Confidence      float64  `json:"confidence"`         /* 执行当前语句并推进处理流程。 */
	Model           string   `json:"model"`              /* 执行当前语句并推进处理流程。 */
	PromptVersion   string   `json:"promptVersion"`      /* 执行当前语句并推进处理流程。 */
	CreatedAt       int64    `json:"createdAt"`          /* 执行当前语句并推进处理流程。 */
	Error           string   `json:"error,omitempty"`    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type KnowledgeDoc struct { /* 定义 KnowledgeDoc 类型。 */
	ID           string         `json:"id"`                  /* 执行当前语句并推进处理流程。 */
	TenantID     string         `json:"tenantId"`            /* 执行当前语句并推进处理流程。 */
	WorkflowID   string         `json:"workflowId"`          /* 执行当前语句并推进处理流程。 */
	ProductID    string         `json:"productId,omitempty"` /* 执行当前语句并推进处理流程。 */
	Category     string         `json:"category,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Tags         []string       `json:"tags,omitempty"`      /* 执行当前语句并推进处理流程。 */
	ObjectBucket string         `json:"objectBucket"`        /* 执行当前语句并推进处理流程。 */
	ObjectKey    string         `json:"objectKey"`           /* 执行当前语句并推进处理流程。 */
	Filename     string         `json:"filename"`            /* 执行当前语句并推进处理流程。 */
	Status       string         `json:"status"`              /* 执行当前语句并推进处理流程。 */
	Metadata     map[string]any `json:"metadata,omitempty"`  /* 执行当前语句并推进处理流程。 */
	CreatedAt    int64          `json:"createdAt"`           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// KnowledgeChunk is the extracted text slice that is sent to the vector
// index. Character offsets are Unicode-code-point offsets in the normalized
// extracted text, with StartChar inclusive and EndChar exclusive.
type KnowledgeChunk struct { /* 定义 KnowledgeChunk 类型。 */
	DocumentID     string `json:"documentId"`     /* 执行当前语句并推进处理流程。 */
	ChunkID        string `json:"chunkId"`        /* 执行当前语句并推进处理流程。 */
	Index          int    `json:"index"`          /* 执行当前语句并推进处理流程。 */
	StartChar      int    `json:"startChar"`      /* 执行当前语句并推进处理流程。 */
	EndChar        int    `json:"endChar"`        /* 执行当前语句并推进处理流程。 */
	CharacterCount int    `json:"characterCount"` /* 执行当前语句并推进处理流程。 */
	OverlapChars   int    `json:"overlapChars"`   /* 执行当前语句并推进处理流程。 */
	Content        string `json:"content"`        /* 执行当前语句并推进处理流程。 */
	Vectorized     bool   `json:"vectorized"`     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// WorkflowKnowledgeBinding constrains knowledge retrieval for one tenant and
// one Harness workflow. The plugin manifest defines what the workflow can do;
// this record defines which tenant knowledge it may use.
type WorkflowKnowledgeBinding struct { /* 定义 WorkflowKnowledgeBinding 类型。 */
	TenantID      string   `json:"tenantId"`             /* 执行当前语句并推进处理流程。 */
	WorkflowID    string   `json:"workflowId"`           /* 执行当前语句并推进处理流程。 */
	ProductIDs    []string `json:"productIds,omitempty"` /* 执行当前语句并推进处理流程。 */
	Categories    []string `json:"categories,omitempty"` /* 执行当前语句并推进处理流程。 */
	Tags          []string `json:"tags,omitempty"`       /* 执行当前语句并推进处理流程。 */
	RetrievalMode string   `json:"retrievalMode"`        /* 执行当前语句并推进处理流程。 */
	TopK          int      `json:"topK"`                 /* 执行当前语句并推进处理流程。 */
	MinScore      float64  `json:"minScore"`             /* 执行当前语句并推进处理流程。 */
	NoMatchPolicy string   `json:"noMatchPolicy"`        /* 执行当前语句并推进处理流程。 */
	UpdatedAt     int64    `json:"updatedAt"`            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ReplayRequest struct { /* 定义 ReplayRequest 类型。 */
	ID            string         `json:"id"`                      /* 执行当前语句并推进处理流程。 */
	TenantID      string         `json:"tenantId"`                /* 执行当前语句并推进处理流程。 */
	ProductID     string         `json:"productId,omitempty"`     /* 执行当前语句并推进处理流程。 */
	DeviceID      string         `json:"deviceId,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Start         int64          `json:"start"`                   /* 执行当前语句并推进处理流程。 */
	End           int64          `json:"end"`                     /* 执行当前语句并推进处理流程。 */
	ParserVersion string         `json:"parserVersion,omitempty"` /* 执行当前语句并推进处理流程。 */
	Mode          string         `json:"mode"`                    /* 执行当前语句并推进处理流程。 */
	RatePerSecond int            `json:"ratePerSecond"`           /* 执行当前语句并推进处理流程。 */
	Status        string         `json:"status"`                  /* 执行当前语句并推进处理流程。 */
	Processed     int            `json:"processed"`               /* 执行当前语句并推进处理流程。 */
	Failed        int            `json:"failed"`                  /* 执行当前语句并推进处理流程。 */
	CreatedBy     string         `json:"createdBy"`               /* 执行当前语句并推进处理流程。 */
	CreatedAt     int64          `json:"createdAt"`               /* 执行当前语句并推进处理流程。 */
	CompletedAt   int64          `json:"completedAt,omitempty"`   /* 执行当前语句并推进处理流程。 */
	DiffSummary   map[string]int `json:"diffSummary,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Diffs         []ReplayDiff   `json:"diffs,omitempty"`         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ReplayDiff struct { /* 定义 ReplayDiff 类型。 */
	RawMessageID string           `json:"rawMessageId"`       /* 执行当前语句并推进处理流程。 */
	Status       string           `json:"status"`             /* 执行当前语句并推进处理流程。 */
	Previous     *StandardMessage `json:"previous,omitempty"` /* 执行当前语句并推进处理流程。 */
	Current      *StandardMessage `json:"current,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Error        string           `json:"error,omitempty"`    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type VideoCameraMapping struct { /* 定义 VideoCameraMapping 类型。 */
	TenantID         string   `json:"tenantId"`                   /* 执行当前语句并推进处理流程。 */
	CameraID         string   `json:"cameraId"`                   /* 执行当前语句并推进处理流程。 */
	CameraName       string   `json:"cameraName"`                 /* 执行当前语句并推进处理流程。 */
	Brand            string   `json:"brand,omitempty"`            /* 执行当前语句并推进处理流程。 */
	CameraPoint      string   `json:"cameraPoint,omitempty"`      /* 执行当前语句并推进处理流程。 */
	DeviceID         string   `json:"deviceId,omitempty"`         /* 执行当前语句并推进处理流程。 */
	IngestMode       string   `json:"ingestMode,omitempty"`       /* 执行当前语句并推进处理流程。 */
	ProjectID        string   `json:"projectId,omitempty"`        /* 执行当前语句并推进处理流程。 */
	CityCode         string   `json:"cityCode,omitempty"`         /* 执行当前语句并推进处理流程。 */
	DistrictCode     string   `json:"districtCode,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Building         string   `json:"building,omitempty"`         /* 执行当前语句并推进处理流程。 */
	Floor            string   `json:"floor,omitempty"`            /* 执行当前语句并推进处理流程。 */
	Room             string   `json:"room,omitempty"`             /* 执行当前语句并推进处理流程。 */
	AreaID           string   `json:"areaId"`                     /* 执行当前语句并推进处理流程。 */
	RelatedDeviceIDs []string `json:"relatedDeviceIds,omitempty"` /* 执行当前语句并推进处理流程。 */
	RelatedFloorIDs  []string `json:"relatedFloorIds,omitempty"`  /* 执行当前语句并推进处理流程。 */
	RelatedRoomIDs   []string `json:"relatedRoomIds,omitempty"`   /* 执行当前语句并推进处理流程。 */
	VideoPlatformID  string   `json:"videoPlatformId,omitempty"`  /* 执行当前语句并推进处理流程。 */
	StreamURL        string   `json:"streamUrl,omitempty"`        /* 执行当前语句并推进处理流程。 */
	StreamType       string   `json:"streamType,omitempty"`       /* 执行当前语句并推进处理流程。 */
	SDKEndpoint      string   `json:"sdkEndpoint,omitempty"`      /* 执行当前语句并推进处理流程。 */
	SDKCameraID      string   `json:"sdkCameraId,omitempty"`      /* 执行当前语句并推进处理流程。 */
	SDKCredentialRef string   `json:"sdkCredentialRef,omitempty"` /* 执行当前语句并推进处理流程。 */
	StreamConfigured bool     `json:"streamConfigured,omitempty"` /* 执行当前语句并推进处理流程。 */
	PreviewEligible  bool     `json:"previewEligible,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Enabled          bool     `json:"enabled"`                    /* 执行当前语句并推进处理流程。 */
	UpdatedAt        int64    `json:"updatedAt"`                  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// CameraSummary is the safe camera metadata attached to an alarm. It
// deliberately has no stream URL or vendor credential fields: live playback
// remains an external camera-platform concern.
type CameraSummary struct { /* 定义 CameraSummary 类型。 */
	CameraID    string `json:"cameraId"`              /* 执行当前语句并推进处理流程。 */
	Brand       string `json:"brand,omitempty"`       /* 执行当前语句并推进处理流程。 */
	CameraName  string `json:"cameraName"`            /* 执行当前语句并推进处理流程。 */
	CameraPoint string `json:"cameraPoint,omitempty"` /* 执行当前语句并推进处理流程。 */
	DeviceID    string `json:"deviceId,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Building    string `json:"building,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Floor       string `json:"floor,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Room        string `json:"room,omitempty"`        /* 执行当前语句并推进处理流程。 */
	Enabled     bool   `json:"enabled"`               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// VideoCameraRelation is the normalized camera-to-device edge. A device may
// have many cameras, while a camera has at most one device association.
type VideoCameraRelation struct { /* 定义 VideoCameraRelation 类型。 */
	TenantID     string `json:"tenantId"`     /* 执行当前语句并推进处理流程。 */
	CameraID     string `json:"cameraId"`     /* 执行当前语句并推进处理流程。 */
	RelationType string `json:"relationType"` /* 执行当前语句并推进处理流程。 */
	TargetID     string `json:"targetId"`     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AIToolCallLog struct { /* 定义 AIToolCallLog 类型。 */
	ID        string         `json:"id"`               /* 执行当前语句并推进处理流程。 */
	TenantID  string         `json:"tenantId"`         /* 执行当前语句并推进处理流程。 */
	Actor     string         `json:"actor"`            /* 执行当前语句并推进处理流程。 */
	Tool      string         `json:"tool"`             /* 执行当前语句并推进处理流程。 */
	Input     map[string]any `json:"input,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Output    any            `json:"output,omitempty"` /* 执行当前语句并推进处理流程。 */
	Success   bool           `json:"success"`          /* 执行当前语句并推进处理流程。 */
	Error     string         `json:"error,omitempty"`  /* 执行当前语句并推进处理流程。 */
	CreatedAt int64          `json:"createdAt"`        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type AuditLog struct { /* 定义 AuditLog 类型。 */
	ID         string         `json:"id"`                /* 执行当前语句并推进处理流程。 */
	TenantID   string         `json:"tenantId"`          /* 执行当前语句并推进处理流程。 */
	Actor      string         `json:"actor"`             /* 执行当前语句并推进处理流程。 */
	Action     string         `json:"action"`            /* 执行当前语句并推进处理流程。 */
	TargetType string         `json:"targetType"`        /* 执行当前语句并推进处理流程。 */
	TargetID   string         `json:"targetId"`          /* 执行当前语句并推进处理流程。 */
	Details    map[string]any `json:"details,omitempty"` /* 执行当前语句并推进处理流程。 */
	CreatedAt  int64          `json:"createdAt"`         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
