package ports

import (
	"context"

	"iot-platform/internal/model"
)

type MessageTopicStore interface {
	ListMessageTopicTenants(context.Context) ([]string, error)
	// ListMessageTopicDevices returns devices and their states from one read,
	// ordered by device ID. Nil deviceIDs means all; an empty non-nil list means
	// none. Limit must be 1..10001, including the query overflow sentinel row.
	ListMessageTopicDevices(context.Context, string, []string, int) ([]model.MessageTopicDeviceRecord, error)
	LoadMessageTopicConfig(context.Context, string) (model.MessageTopicConfig, error)
	// SaveMessageTopicConfig replaces a tenant's configuration only when Revision
	// matches the stored revision. A new configuration starts at revision zero.
	SaveMessageTopicConfig(context.Context, string, model.MessageTopicConfig) (bool, error)
}
