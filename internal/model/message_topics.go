package model

// MessageTopicConfig stores tenant-owned overrides of the platform topic catalog.
// Revision is the optimistic concurrency token for the complete configuration.
type MessageTopicConfig struct {
	Revision  int64                           `json:"revision"`
	Overrides map[string]MessageTopicOverride `json:"overrides"`
}

type MessageTopicOverride struct {
	Enabled     bool   `json:"enabled"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
}
