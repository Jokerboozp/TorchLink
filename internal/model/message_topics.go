package model

// MessageTopicConfig stores tenant-owned overrides of the platform topic catalog.
// Revision is the optimistic concurrency token for the complete configuration.
type MessageTopicConfig struct {
	Revision    int64                           `json:"revision"`
	Overrides   map[string]MessageTopicOverride `json:"overrides"`
	Topics      []MessageTopicRoute             `json:"topics"`
	Deleted     []string                        `json:"deleted"`
	Accounts    []MessageTopicAccount           `json:"accounts"`
	Credentials []MessageTopicCredential        `json:"credentials"`
}

type MessageTopicOverride struct {
	Enabled     bool   `json:"enabled"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
}

// MessageTopicRoute creates an external publication channel from one supported
// source. Topic is the channel's readable slug; broker destinations are isolated
// by tenant and credential rather than exposing the shared source topic.
type MessageTopicRoute struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SourceID    string `json:"sourceId"`
	Topic       string `json:"topic"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type MessageTopicAccount struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Username    string   `json:"username"`
	Enabled     bool     `json:"enabled"`
	TopicIDs    []string `json:"topicIds"`
	DeviceScope string   `json:"deviceScope"`
	DeviceIDs   []string `json:"deviceIds"`
	SecretHash  string   `json:"secretHash"`
	CreatedAt   int64    `json:"createdAt"`
	ExpiresAt   int64    `json:"expiresAt"`
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
	AccessVersion string   `json:"accessVersion"`
	Status        string   `json:"status"`
	CreatedAt     int64    `json:"createdAt"`
	ExpiresAt     int64    `json:"expiresAt"`
}
