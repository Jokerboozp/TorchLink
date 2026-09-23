// Package gb26875 implements the Dahua fire-terminal v1.03 supplement to
// GB/T 26875.3-2011 without depending on the IoT platform source tree.
package gb26875 /* 声明 gb26875 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// DeviceLocation makes the protocol's zone-less device clock deterministic
// across Windows hosts and Linux workers. Chinese terminals use UTC+08:00.
var DeviceLocation = time.FixedZone("Asia/Shanghai", 8*60*60) /* 声明 DeviceLocation。 */

type MessageType string /* 定义 MessageType 类型。 */

const ( /* 执行当前语句并推进处理流程。 */
	PropertyReport MessageType = "PROPERTY_REPORT" /* 执行当前语句并推进处理流程。 */
	EventReport    MessageType = "EVENT_REPORT"    /* 执行当前语句并推进处理流程。 */
	StateChange    MessageType = "STATE_CHANGE"    /* 执行当前语句并推进处理流程。 */
	AlarmReport    MessageType = "ALARM_REPORT"    /* 执行当前语句并推进处理流程。 */
	CommandReply   MessageType = "COMMAND_REPLY"   /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// RawMessage is the JSON Lines v1 input. Additional platform fields are ignored.
// Identity is supplied by the authenticated ingest envelope, never derived here.
type RawMessage struct { /* 定义 RawMessage 类型。 */
	MessageID     string          `json:"messageId"`     /* 执行当前语句并推进处理流程。 */
	TenantID      string          `json:"tenantId"`      /* 执行当前语句并推进处理流程。 */
	ProductID     string          `json:"productId"`     /* 执行当前语句并推进处理流程。 */
	DeviceID      string          `json:"deviceId"`      /* 执行当前语句并推进处理流程。 */
	Protocol      string          `json:"protocol"`      /* 执行当前语句并推进处理流程。 */
	Transport     string          `json:"transport"`     /* 执行当前语句并推进处理流程。 */
	ReceivedAt    int64           `json:"receivedAt"`    /* 执行当前语句并推进处理流程。 */
	PayloadFormat string          `json:"payloadFormat"` /* 执行当前语句并推进处理流程。 */
	Payload       json.RawMessage `json:"payload"`       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// StandardMessage preserves the existing GB26875 property/event vocabulary.
// The platform records the actual uploaded parser and release version.
type StandardMessage struct { /* 定义 StandardMessage 类型。 */
	MessageID    string            `json:"messageId,omitempty"`    /* 执行当前语句并推进处理流程。 */
	RawMessageID string            `json:"rawMessageId,omitempty"` /* 执行当前语句并推进处理流程。 */
	TenantID     string            `json:"tenantId,omitempty"`     /* 执行当前语句并推进处理流程。 */
	ProductID    string            `json:"productId,omitempty"`    /* 执行当前语句并推进处理流程。 */
	DeviceID     string            `json:"deviceId,omitempty"`     /* 执行当前语句并推进处理流程。 */
	MessageType  MessageType       `json:"messageType"`            /* 执行当前语句并推进处理流程。 */
	Timestamp    int64             `json:"timestamp"`              /* 执行当前语句并推进处理流程。 */
	Properties   map[string]any    `json:"properties,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Event        map[string]any    `json:"event,omitempty"`        /* 执行当前语句并推进处理流程。 */
	Tags         map[string]string `json:"tags,omitempty"`         /* 执行当前语句并推进处理流程。 */
	Raw          map[string]any    `json:"raw,omitempty"`          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
