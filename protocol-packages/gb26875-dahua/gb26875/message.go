// Package gb26875 implements the Dahua fire-terminal v1.03 supplement to
// GB/T 26875.3-2011 without depending on the IoT platform source tree.
package gb26875

import (
	"encoding/json"
	"time"
)

// DeviceLocation makes the protocol's zone-less device clock deterministic
// across Windows hosts and Linux workers. Chinese terminals use UTC+08:00.
var DeviceLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type MessageType string

const (
	PropertyReport MessageType = "PROPERTY_REPORT"
	EventReport    MessageType = "EVENT_REPORT"
	StateChange    MessageType = "STATE_CHANGE"
	AlarmReport    MessageType = "ALARM_REPORT"
	CommandReply   MessageType = "COMMAND_REPLY"
)

// RawMessage is the JSON Lines v1 input. Additional platform fields are ignored.
// Identity is supplied by the authenticated ingest envelope, never derived here.
type RawMessage struct {
	MessageID     string          `json:"messageId"`
	TenantID      string          `json:"tenantId"`
	ProductID     string          `json:"productId"`
	DeviceID      string          `json:"deviceId"`
	Protocol      string          `json:"protocol"`
	Transport     string          `json:"transport"`
	ReceivedAt    int64           `json:"receivedAt"`
	PayloadFormat string          `json:"payloadFormat"`
	Payload       json.RawMessage `json:"payload"`
}

// StandardMessage preserves the existing GB26875 property/event vocabulary.
// The platform records the actual uploaded parser and release version.
type StandardMessage struct {
	MessageID    string            `json:"messageId,omitempty"`
	RawMessageID string            `json:"rawMessageId,omitempty"`
	TenantID     string            `json:"tenantId,omitempty"`
	ProductID    string            `json:"productId,omitempty"`
	DeviceID     string            `json:"deviceId,omitempty"`
	MessageType  MessageType       `json:"messageType"`
	Timestamp    int64             `json:"timestamp"`
	Properties   map[string]any    `json:"properties,omitempty"`
	Event        map[string]any    `json:"event,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	Raw          map[string]any    `json:"raw,omitempty"`
}
