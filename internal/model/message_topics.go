package model

import (
	"bytes"
	"encoding/json"
)

// MessageTopicConfig stores a tenant's external data subscription topics.
// Revision is the optimistic concurrency token for the complete configuration.
type MessageTopicConfig struct {
	Revision      int64                    `json:"revision"`
	Topics        []MessageTopicRoute      `json:"topics"`
	Credentials   []MessageTopicCredential `json:"credentials"`
	RetiredTopics []string                 `json:"retiredTopics"`
}

// MessageTopicRoute publishes one business data query to one tenant-owned
// MQTT or Kafka topic. KeyIDs are the open API keys allowed to subscribe.
type MessageTopicRoute struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Protocol    string                 `json:"protocol"`
	Topic       string                 `json:"topic"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description"`
	Exposure    []MessageTopicExposure `json:"exposure"`
	Query       *MessageTopicQuery     `json:"query"`
	KeyIDs      []string               `json:"keyIds"`
}

// MessageTopicQuery selects one business dataset and produces one stable JSON
// record per matching event or scheduled row. It never contains database SQL.
type MessageTopicQuery struct {
	Dataset         string              `json:"dataset"`
	Fields          map[string]string   `json:"fields"`
	Filter          *MessageTopicFilter `json:"filter,omitempty"`
	DeviceScope     string              `json:"deviceScope"`
	DeviceIDs       []string            `json:"deviceIds"`
	Mode            string              `json:"mode"`
	IntervalSeconds int                 `json:"intervalSeconds"`
}

type MessageTopicFilter struct {
	Logic    string               `json:"logic,omitempty"`
	Children []MessageTopicFilter `json:"children,omitempty"`
	Field    string               `json:"field,omitempty"`
	Operator string               `json:"operator,omitempty"`
	Value    any                  `json:"value,omitempty"`
}

func (f *MessageTopicFilter) UnmarshalJSON(data []byte) error {
	type plain MessageTopicFilter
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode((*plain)(f))
}

// Exposure records every platform data scope ever published to a topic.
// Narrowing a query does not remove older messages from the broker.
type MessageTopicExposure struct {
	SourceID    string   `json:"sourceId"`
	DeviceScope string   `json:"deviceScope"`
	DeviceIDs   []string `json:"deviceIds"`
}

// Credentials contain only broker identity and the exact grant snapshot. The
// API key secret and broker passwords are never persisted as plaintext here.
type MessageTopicCredential struct {
	ID            string   `json:"id"`
	KeyID         string   `json:"keyId"`
	Protocol      string   `json:"protocol"`
	Username      string   `json:"username"`
	GroupID       string   `json:"groupId"`
	Topics        []string `json:"topics"`
	AccessVersion string   `json:"accessVersion"`
	Status        string   `json:"status"`
	Provisioning  bool     `json:"provisioning"`
	CreatedAt     int64    `json:"createdAt"`
	ExpiresAt     int64    `json:"expiresAt"`
}
