package ports

import (
	"context"

	"iot-platform/internal/model"
)

type MessageTopicStore interface {
	LoadMessageTopicConfig(context.Context, string) (model.MessageTopicConfig, error)
	// SaveMessageTopicConfig replaces a tenant's configuration only when Revision
	// matches the stored revision. A new configuration starts at revision zero.
	SaveMessageTopicConfig(context.Context, string, model.MessageTopicConfig) (bool, error)
}
