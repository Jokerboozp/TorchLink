package model

// MessageTopicConfig stores tenant-owned overrides of the platform topic catalog.
// Revision is the optimistic concurrency token for the complete configuration.
type MessageTopicConfig struct {
	Revision       int64                           `json:"revision"`
	Overrides      map[string]MessageTopicOverride `json:"overrides"`
	Topics         []MessageTopicRoute             `json:"topics"`
	Deleted        []string                        `json:"deleted"`
	Accounts       []MessageTopicAccount           `json:"accounts"`
	Credentials    []MessageTopicCredential        `json:"credentials"`
	Rules          []MessageTopicRule              `json:"rules"`
	RetiredTopics  []string                        `json:"retiredTopics"`
	RoutingHistory []MessageTopicReservedRoute     `json:"routingHistory"`
}

// Reserved routes retain ordinary publication addresses after an override is
// renamed, reset or deleted. Shared topics must not inherit their data history
// or receive publications already in flight to those addresses.
type MessageTopicReservedRoute struct {
	SourceID string `json:"sourceId"`
	Topic    string `json:"topic"`
}

type MessageTopicOverride struct {
	Enabled     bool   `json:"enabled"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
}

// MessageTopicRoute is either a shared tenant topic (Protocol is set), or a
// legacy source route (SourceID is set) with per-credential broker destinations.
type MessageTopicRoute struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	SourceID    string                 `json:"sourceId"`
	Protocol    string                 `json:"protocol"`
	Topic       string                 `json:"topic"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description"`
	Exposure    []MessageTopicExposure `json:"exposure"`
}

// Exposure records every platform data scope ever published to a shared topic.
// Removing or narrowing a rule does not remove older messages from the broker.
type MessageTopicExposure struct {
	SourceID    string   `json:"sourceId"`
	DeviceScope string   `json:"deviceScope"`
	DeviceIDs   []string `json:"deviceIds"`
}

type MessageTopicRule struct {
	ID          string            `json:"id"`
	TopicID     string            `json:"topicId"`
	Name        string            `json:"name"`
	SourceID    string            `json:"sourceId"`
	Enabled     bool              `json:"enabled"`
	DeviceScope string            `json:"deviceScope"`
	DeviceIDs   []string          `json:"deviceIds"`
	Format      string            `json:"format"`
	Fields      map[string]string `json:"fields"`
	Template    string            `json:"template"`
}

type MessageTopicAccount struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Username        string   `json:"username"`
	Enabled         bool     `json:"enabled"`
	TopicIDs        []string `json:"topicIds"`
	PublishTopicIDs []string `json:"publishTopicIds"`
	DeviceScope     string   `json:"deviceScope"`
	DeviceIDs       []string `json:"deviceIds"`
	SecretHash      string   `json:"secretHash"`
	CreatedAt       int64    `json:"createdAt"`
	ExpiresAt       int64    `json:"expiresAt"`
}

// Credentials contain only broker identity and the exact grant snapshot. The
// account secret and broker passwords are never persisted as plaintext here.
type MessageTopicCredential struct {
	ID            string   `json:"id"`
	AccountID     string   `json:"accountId"`
	Protocol      string   `json:"protocol"`
	Username      string   `json:"username"`
	GroupID       string   `json:"groupId"`
	Topics        []string `json:"topics"`
	PublishTopics []string `json:"publishTopics"`
	AccessVersion string   `json:"accessVersion"`
	Status        string   `json:"status"`
	Provisioning  bool     `json:"provisioning"`
	CreatedAt     int64    `json:"createdAt"`
	ExpiresAt     int64    `json:"expiresAt"`
}
